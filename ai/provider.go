package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/gophish/gophish/config"
)

// ChatProvider implements Provider against any OpenAI-compatible
// /chat/completions endpoint.
type ChatProvider struct {
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
	client    *http.Client
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessage     `json:"messages"`
	MaxTokens      int               `json:"max_tokens,omitempty"`
	Temperature    float64           `json:"temperature"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// NewChatProvider builds a ChatProvider from the AI configuration section.
func NewChatProvider(conf config.AIConfig) *ChatProvider {
	timeoutSeconds := conf.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = DefaultTimeoutSeconds
	}
	maxTokens := conf.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	return &ChatProvider{
		baseURL:   strings.TrimSuffix(conf.BaseURL, "/"),
		apiKey:    conf.APIKey,
		model:     conf.Model,
		maxTokens: maxTokens,
		client:    &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
	}
}

// Model returns the configured model name.
func (p *ChatProvider) Model() string {
	return p.model
}

// Generate sends the prompts to the chat completions endpoint and returns the
// raw content of the first choice. JSON mode is requested so the output can be
// parsed as structured data.
func (p *ChatProvider) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	reqBody := chatRequest{
		Model: p.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens:      p.maxTokens,
		Temperature:    0.7,
		ResponseFormat: map[string]string{"type": "json_object"},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("AI provider request failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI provider returned status %d: %s", resp.StatusCode, string(body))
	}
	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", fmt.Errorf("invalid AI provider response: %v", err)
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return "", fmt.Errorf("AI provider error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 || cr.Choices[0].Message.Content == "" {
		return "", errors.New("AI provider returned no content")
	}
	return cr.Choices[0].Message.Content, nil
}
