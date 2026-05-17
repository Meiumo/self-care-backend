package liveresponse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const openRouterURL = "https://openrouter.ai/api/v1/chat/completions"

// AIResponse is the parsed DeepSeek JSON payload.
type AIResponse struct {
	Message          string   `json:"message"`
	AdviceTags       []string `json:"advice_tags"`
	FollowupHours    *int     `json:"followup_hours"`
	FollowupQuestion string   `json:"followup_question"`
}

// Message is one chat turn for the OpenRouter API.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenRouterClient calls the OpenRouter API (OpenAI-compatible).
type OpenRouterClient struct {
	apiKey  string
	model   string
	httpCli *http.Client
}

func NewOpenRouterClient(apiKey, model string) *OpenRouterClient {
	if model == "" {
		model = "deepseek/deepseek-v4-pro"
	}
	return &OpenRouterClient{
		apiKey:  apiKey,
		model:   model,
		httpCli: &http.Client{Timeout: 30 * time.Second},
	}
}

// CompleteText sends the messages and returns the raw text response without JSON parsing.
// Use for prompts that don't require structured output (e.g. chat turns, recheck advice).
func (c *OpenRouterClient) CompleteText(ctx context.Context, messages []Message, temperature float64) (string, error) {
	return c.call(ctx, messages, temperature, 700)
}

// Complete sends the messages to DeepSeek and returns the parsed AIResponse.
// It retries once if the first response is not valid JSON in the expected schema.
func (c *OpenRouterClient) Complete(ctx context.Context, messages []Message, temperature float64) (*AIResponse, error) {
	for attempt := range 2 {
		msgs := messages
		if attempt == 1 {
			// Append a corrective turn so the model knows what went wrong
			msgs = append(msgs, Message{
				Role:    "user",
				Content: "Предыдущий ответ не прошёл валидацию JSON. Ответь строго в JSON без markdown-блоков.",
			})
		}

		raw, err := c.call(ctx, msgs, temperature, 450)
		if err != nil {
			return nil, err
		}

		resp, err := parseAIResponse(raw)
		if err == nil {
			return resp, nil
		}
		if attempt == 1 {
			return nil, fmt.Errorf("deepseek: invalid JSON after retry: %w", err)
		}
	}
	// Unreachable
	return nil, fmt.Errorf("deepseek: unexpected exit")
}

// ── Internal ──────────────────────────────────────────────────────────────────

type orRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
}

type orResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *OpenRouterClient) call(ctx context.Context, messages []Message, temperature float64, maxTokens int) (string, error) {
	body, err := json.Marshal(orRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var or orResponse
	if err := json.Unmarshal(data, &or); err != nil {
		return "", fmt.Errorf("deepseek: unmarshal response: %w", err)
	}
	if or.Error != nil {
		return "", fmt.Errorf("deepseek: api error: %s", or.Error.Message)
	}
	if len(or.Choices) == 0 {
		return "", fmt.Errorf("deepseek: empty choices")
	}

	return or.Choices[0].Message.Content, nil
}

// parseAIResponse strips possible markdown fences and parses the JSON payload.
func parseAIResponse(raw string) (*AIResponse, error) {
	raw = strings.TrimSpace(raw)
	// Strip ```json ... ``` or ``` ... ``` if the model adds them anyway
	if strings.HasPrefix(raw, "```") {
		start := strings.Index(raw, "\n")
		end := strings.LastIndex(raw, "```")
		if start >= 0 && end > start {
			raw = strings.TrimSpace(raw[start+1 : end])
		}
	}

	var resp AIResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, err
	}
	if resp.Message == "" {
		return nil, fmt.Errorf("deepseek: message field is empty")
	}
	if resp.AdviceTags == nil {
		resp.AdviceTags = []string{}
	}
	return &resp, nil
}
