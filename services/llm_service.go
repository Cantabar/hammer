go
package services

import (
	"context"
	"fmt"
	"log"
	"strings"

	openai "github.com/sashabaranov/go-openai"
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
	if planStepsPromptTemplate == "" || evaluateFilesPromptTemplate == "" || generateCodePromptTemplate == "" {
		log.Fatal("LLM prompt templates failed to load. Check embed directives and file paths.")
	}
	return &LLMService{
		client: openai.NewClient(apiKey),
	}
}

func (s *LLMService) GenerateCommitMessage(ctx context.Context, gitDiff string) (string, error) {
	prompt := fmt.Sprintf("Given the following git diff:\n%s\nGenerate a concise commit message that summarizes the changes.", gitDiff)

	resp, err := s.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: openai.GPT4TurboPreview,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are an assistant that generates git commit messages.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			MaxTokens:   60,
			Temperature: 0.5,
		},
	)

	if err != nil {
		return "", fmt.Errorf("openai commit message generation request failed: %w", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("openai returned empty commit message response")
	}

	message := strings.TrimSpace(resp.Choices[0].Message.Content)
	if len(message) > 50 {
		message = message[:47] + "..."
	}

	return message, nil
}

func (s *LLMService) GenerateSemanticCommitPrefix(ctx context.Context, gitDiff string) (string, error) {
	prompt := fmt.Sprintf("Given the following git diff:\n%s\nDetermine the semantic commit prefix that best matches the changes. Options: chore, fix, feat, refactor, test.", gitDiff)

	resp, err := s.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: openai.GPT4TurboPreview,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are an assistant that determines semantic commit prefixes.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			MaxTokens:   20,
			Temperature: 0.5,
		},
	)

	if err != nil {
		return "", fmt.Errorf("openai semantic prefix determination request failed: %w", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("openai returned empty semantic prefix response")
	}

	prefix := strings.TrimSpace(resp.Choices[0].Message.Content)
	validPrefixes := map[string]bool{"chore": true, "fix": true, "feat": true, "refactor": true, "test": true}
	if !validPrefixes[prefix] {
		return "", fmt.Errorf("invalid semantic prefix: %s", prefix)
	}

	return prefix, nil
}

// PlanSteps breaks down the user prompt into actionable steps.
func (s *LLMService) PlanSteps(ctx context.Context, userPrompt string) ([]string, error) {
	prompt := fmt.Sprintf(planStepsPromptTemplate, userPrompt)

	resp, err := s.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: openai.GPT4TurboPreview,
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

	content := "1. " + resp.Choices[0].Message.Content
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

	rawOutput := resp.Choices[0].Message.Content
	log.Printf("LLM Code Generation Raw Output:\n%s", rawOutput)
	changes := make(map[string]string)
	blocks := strings.Split(rawOutput, "