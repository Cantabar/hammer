package services

import (
	"context"
	_ "embed" // Import the embed package (blank identifier is convention)
	"fmt"
	"log"
	"strings"

	openai "github.com/sashabaranov/go-openai" // Ensure correct import path
)

//go:embed prompts/plan_steps.txt
var planStepsPromptTemplate string

//go:embed prompts/evaluate_files.txt
var evaluateFilesPromptTemplate string

//go:embed prompts/generate_code.txt
var generateCodePromptTemplate string

type LLMService struct {
	client *openai.Client
}

func NewLLMService(apiKey string) *LLMService {
	// You could potentially add validation here to ensure templates loaded correctly,
	// though embed errors usually happen at compile time.
	if planStepsPromptTemplate == "" || evaluateFilesPromptTemplate == "" || generateCodePromptTemplate == "" {
		log.Fatal("LLM prompt templates failed to load. Check embed directives and file paths.")
	}
	return &LLMService{
		client: openai.NewClient(apiKey),
	}
}

// PlanSteps breaks down the user prompt into actionable steps.
func (s *LLMService) PlanSteps(ctx context.Context, userPrompt string) ([]string, error) {
	// Use the embedded template string
	prompt := fmt.Sprintf(planStepsPromptTemplate, userPrompt)

	resp, err := s.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: openai.GPT4TurboPreview, // Or your preferred model
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a planning assistant that breaks down code generation tasks into simple steps.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			MaxTokens:   500,
			Temperature: 0.2,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("openai planning request failed: %w", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("openai returned empty planning response")
	}

	// Parsing logic remains the same
	content := "1. " + resp.Choices[0].Message.Content // Add back the primed "1. "
	rawSteps := strings.Split(strings.TrimSpace(content), "\n")
	var steps []string
	for _, step := range rawSteps {
		trimmed := strings.TrimSpace(step)
		if len(trimmed) > 0 {
			parts := strings.SplitN(trimmed, ". ", 2)
			if len(parts) == 2 {
				steps = append(steps, strings.TrimSpace(parts[1]))
			} else {
				steps = append(steps, trimmed)
			}
		}
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("could not parse any steps from LLM response: %s", content)
	}
	log.Printf("Planned Steps: %v", steps)
	return steps, nil
}

// EvaluateRelevantFiles determines which files are needed for a given step.
func (s *LLMService) EvaluateRelevantFiles(ctx context.Context, step string, allFiles []string) ([]string, error) {
	fileList := strings.Join(allFiles, "\n")
	// Use the embedded template string
	prompt := fmt.Sprintf(evaluateFilesPromptTemplate, step, fileList)

	resp, err := s.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: openai.GPT3Dot5Turbo,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a file evaluation assistant.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			MaxTokens:   200,
			Temperature: 0.1,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("openai evaluation request failed: %w", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("openai returned empty evaluation response")
	}

	// Parsing logic remains the same
	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	log.Printf("LLM Evaluation Raw Response: %s", content)
	if content == "NONE" || content == "" {
		log.Println("Evaluated Files: None")
		return []string{}, nil
	}
	relevantFiles := strings.Split(content, ",")
	var cleanedFiles []string
	validFiles := make(map[string]struct{})
	for _, f := range allFiles {
		validFiles[f] = struct{}{}
	}
	for _, file := range relevantFiles {
		trimmed := strings.TrimSpace(file)
		if _, ok := validFiles[trimmed]; ok && trimmed != "" {
			cleanedFiles = append(cleanedFiles, trimmed)
		} else if trimmed != "" {
			log.Printf("Warning: LLM suggested non-existent file '%s' for step '%s'", trimmed, step)
		}
	}
	log.Printf("Evaluated Relevant Files: %v", cleanedFiles)
	return cleanedFiles, nil
}

// GenerateCodeChanges generates the code modifications for a step.
func (s *LLMService) GenerateCodeChanges(ctx context.Context, step string, relevantFilesContent map[string]string, userPrompt string) (map[string]string, error) {
	contextStr := ""
	if len(relevantFilesContent) > 0 {
		contextStr += "Relevant File Contents:\n"
		for path, content := range relevantFilesContent {
			contextStr += fmt.Sprintf("--- File: %s ---\n%s\n\n", path, content)
		}
	} else {
		contextStr = "No existing files were deemed relevant. You might be creating a new file."
	}

	// Use the embedded template string
	prompt := fmt.Sprintf(generateCodePromptTemplate, userPrompt, step, contextStr)

	resp, err := s.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: openai.GPT4TurboPreview,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are an expert code generation assistant.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			MaxTokens:   3000,
			Temperature: 0.3,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("openai code generation request failed: %w", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("openai returned empty code generation response")
	}

	// Parsing logic remains the same
	rawOutput := resp.Choices[0].Message.Content
	log.Printf("LLM Code Generation Raw Output:\n%s", rawOutput)
	changes := make(map[string]string)
	blocks := strings.Split(rawOutput, "---<<<EO>>>---")
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		parts := strings.SplitN(block, "CONTENT:", 2)
		if len(parts) != 2 {
			log.Printf("Warning: Could not parse FILENAME/CONTENT structure in block: %s", block)
			continue
		}
		header := strings.TrimSpace(parts[0])
		contentBlock := strings.TrimSpace(parts[1])
		if !strings.HasPrefix(header, "FILENAME:") {
			log.Printf("Warning: Block header does not start with FILENAME:: %s", header)
			continue
		}
		filePath := strings.TrimSpace(strings.TrimPrefix(header, "FILENAME:"))
		content := contentBlock
		if strings.HasPrefix(contentBlock, "```") {
			contentEnd := strings.LastIndex(contentBlock, "```")
			if contentEnd > 0 {
				firstNewline := strings.Index(contentBlock, "\n")
				if firstNewline > 0 && firstNewline < contentEnd {
					content = strings.TrimSpace(contentBlock[firstNewline+1 : contentEnd])
				} else {
					content = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(contentBlock, "```"), "```"))
				}
			} else {
				log.Printf("Warning: Found opening ``` but no closing ``` for file %s. Using raw content.", filePath)
				content = strings.TrimPrefix(contentBlock, "```")
			}
		}
		if filePath != "" {
			changes[filePath] = content
			log.Printf("Parsed change for file: %s", filePath)
		} else {
			log.Printf("Warning: Parsed an empty file path from block: %s", block)
		}
	}
	if len(changes) == 0 && rawOutput != "" {
		log.Printf("Warning: LLM generated output but no files could be parsed. Raw Output: %s", rawOutput)
	} else if len(changes) == 0 {
		log.Println("LLM did not generate any file changes for this step.")
	}
	return changes, nil
}
