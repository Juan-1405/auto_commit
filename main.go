package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

const (
	openRouterAPIURL = "https://openrouter.ai/api/v1/chat/completions"
	llmModel         = "tngtech/deepseek-r1t2-chimera:free" // Or any other model that supports structured outputs
)

// CommitMessage defines the structure for the LLM's generated commit message.
type CommitMessage struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// LLMRequest represents the request body for the OpenRouter API.
type LLMRequest struct {
	Model         string        `json:"model"`
	Messages      []LLMMessage  `json:"messages"`
	ResponseFormat LLMResponseFormat `json:"response_format"`
}

// LLMMessage represents a message in the LLM conversation.
type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMResponseFormat defines the structured output format for the LLM.
type LLMResponseFormat struct {
	Type     string            `json:"type"`
	JSONSchema LLMJSONSchema `json:"json_schema"`
}

// LLMJSONSchema defines the JSON schema for the structured output.
type LLMJSONSchema struct {
	Name   string                 `json:"name"`
	Strict bool                   `json:"strict"`
	Schema map[string]interface{} `json:"schema"`
}

// LLMResponse represents the simplified structure of the OpenRouter API response.
type LLMResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func main() {
	log.SetFlags(0) // Disable timestamp and file location in logs

	// 1. Check if the current directory is a Git repository.
	if err := runGitCommand("rev-parse", "--is-inside-work-tree"); err != nil {
		log.Fatalf("Error: Not a Git repository or git not installed. %v", err)
	}
	log.Println("Git repository detected.")

	diffOutput, err := getGitDiff()
	if err != nil {
		log.Fatalf("Error getting git diff: %v", err)
	}
	if diffOutput == "" {
		log.Println("No staged changes found (diff is empty). Exiting.")
		os.Exit(0)
	}
	log.Println("Git diff obtained.")

	// 4. Get OPENROUTER_API_KEY from environment.
	openRouterAPIKey := os.Getenv("OPENROUTER_API_KEY")
	if openRouterAPIKey == "" {
		log.Fatal("Error: OPENROUTER_API_KEY environment variable not set.")
	}

	// 5. Generate commit message using LLM.
	language := getLanguagePreference()
	commitMsg, err := generateCommitMessage(openRouterAPIKey, diffOutput, language)
	if err != nil {
		log.Fatalf("Error generating commit message: %v", err)
	}
	log.Printf("Generated Commit Title: %s\n", commitMsg.Title)
	log.Printf("Generated Commit Description:\n%s\n", commitMsg.Description)

	// 6. Execute git commit.
	if err := runGitCommand("add", "."); err != nil {
		log.Fatalf("Error during git commit: %v", err)
	}

	log.Println("Executing git commit...")
	if err := runGitCommand("commit", "-m", commitMsg.Title, "-m", commitMsg.Description); err != nil {
		log.Fatalf("Error during git commit: %v", err)
	}
	log.Println("Git commit successful.")

	// 7. Execute git push.
	log.Println("Executing git push...")
	if err := runGitCommand("push"); err != nil {
		log.Fatalf("Error during git push: %v", err)
	}
	log.Println("Git push successful. Application finished.")
}

// runGitCommand executes a git command and prints its output.
// It returns an error if the command fails.
func runGitCommand(args ...string) error {
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if err != nil {
		return fmt.Errorf("git command failed: %w", err)
	}
	return nil
}

// getGitDiff returns the output of `git status --short` followed by `git diff --cached`.
func getGitDiff() (string, error) {
	// Get short status (e.g., M file.go, A new_file.go)
	statusCmd := exec.Command("git", "status", "--short")
	var statusOut bytes.Buffer
	statusCmd.Stdout = &statusOut
	statusCmd.Stderr = os.Stderr
	if err := statusCmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get git status --short: %w", err)
	}

	// Get cached diff
	diffCmd := exec.Command("git", "diff")
	var diffOut bytes.Buffer
	diffCmd.Stdout = &diffOut
	diffCmd.Stderr = os.Stderr // Print errors directly
	if err := diffCmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get git diff --cached: %w", err)
	}

	// Combine status and diff for better context
	return "Git Status (staged files):\n" + statusOut.String() + "\nGit Diff (staged changes):\n" + diffOut.String(), nil
}

// getLanguagePreference prompts the user to select a language for the commit message.
func getLanguagePreference() string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter commit language (en/es) [en]: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "es" {
		return "Spanish"
	}
	return "English"
}

// generateCommitMessage calls the OpenRouter API to generate a commit message.
func generateCommitMessage(apiKey, diff, language string) (*CommitMessage, error) {
	var instruction, titleDesc, descriptionDesc string

	if language == "Spanish" {
		instruction = "Analiza el siguiente git diff y genera un título de commit conciso (máx. 70 caracteres) y una descripción detallada del commit. Responde en formato JSON de acuerdo con el esquema:"
		titleDesc = "Título conciso del mensaje de commit"
		descriptionDesc = "Descripción detallada del mensaje de commit"
	} else {
		instruction = "Analyze the following git diff and generate a concise commit title (max 70 chars) and a detailed commit description. Respond in JSON format according to the schema:"
		titleDesc = "Concise commit message title"
		descriptionDesc = "Detailed commit message description"
	}

	promptSchema := fmt.Sprintf(`
		{
		  "type": "object",
		  "properties": {
			"title": {
			  "type": "string",
			  "description": "%s"
			},
			"description": {
			  "type": "string",
			  "description": "%s"
			}
		  },
		  "required": ["title", "description"],
		  "additionalProperties": false
		}
		`, titleDesc, descriptionDesc)

	prompt := fmt.Sprintf("%s\n\n```json\n%s\n```\n\nGit Diff:\n```diff\n%s\n```", instruction, promptSchema, diff)

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title": map[string]interface{}{
				"type":        "string",
				"description": titleDesc,
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": descriptionDesc,
			},
		},
		"required":             []string{"title", "description"},
		"additionalProperties": false,
	}

	requestBody := LLMRequest{
		Model: llmModel,
		Messages: []LLMMessage{
			{Role: "user", Content: prompt},
		},
		ResponseFormat: LLMResponseFormat{
			Type: "json_schema",
			JSONSchema: LLMJSONSchema{
				Name:   "commit_message",
				Strict: true,
				Schema: schema,
			},
		},
	}

	requestBodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal LLM request: %w", err)
	}

	req, err := http.NewRequest("POST", openRouterAPIURL, bytes.NewBuffer(requestBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorBody map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&errorBody); err != nil {
			return nil, fmt.Errorf("API request failed with status %d, could not decode error body: %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("API request failed with status %d: %v", resp.StatusCode, errorBody)
	}

	var llmResponse LLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResponse); err != nil {
		return nil, fmt.Errorf("failed to decode LLM response: %w", err)
	}

	if len(llmResponse.Choices) == 0 || llmResponse.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("LLM response contained no choices or empty content")
	}

	// The content from LLM is a JSON string, so we need to unmarshal it again
	var commitMessage CommitMessage
	llmContent := llmResponse.Choices[0].Message.Content
	if err := json.Unmarshal([]byte(llmContent), &commitMessage); err != nil {
		return nil, fmt.Errorf("failed to unmarshal commit message from LLM content: %w", err)
	}

	return &commitMessage, nil
}
