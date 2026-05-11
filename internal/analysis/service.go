package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const claudeURL = "https://api.anthropic.com/v1/messages"
const claudeModel = "claude-haiku-4-5-20251001"

type Service struct {
	apiKey string
	client *http.Client
}

func NewService(apiKey string) *Service {
	return &Service{
		apiKey: apiKey,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

type AnalysisResult struct {
	Summary     string   `json:"summary"`
	RedFlags    []string `json:"red_flags"`
	Suggestions []string `json:"suggestions"`
	ToneScore   int      `json:"tone_score"` // 1-10, 10 = very healthy
}

func (s *Service) AnalyzeText(ctx context.Context, text string) (*AnalysisResult, error) {
	prompt := fmt.Sprintf(`Проанализируй следующий текст переписки или ситуацию с точки зрения психологического здоровья отношений.
Ответь строго в JSON формате:
{
  "summary": "краткое резюме ситуации (1-2 предложения)",
  "red_flags": ["список тревожных признаков если есть"],
  "suggestions": ["конкретные советы для улучшения ситуации"],
  "tone_score": число от 1 до 10 (10 = очень здоровые отношения)
}

Текст для анализа:
%s`, text)

	body, _ := json.Marshal(map[string]any{
		"model":      claudeModel,
		"max_tokens": 1024,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claudeURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("claude api error: %d", resp.StatusCode)
	}

	var apiResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}
	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("empty response from claude")
	}

	var result AnalysisResult
	if err := json.Unmarshal([]byte(apiResp.Content[0].Text), &result); err != nil {
		return nil, fmt.Errorf("failed to parse claude response: %w", err)
	}
	return &result, nil
}
