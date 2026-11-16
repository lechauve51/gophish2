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
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessage     `json:"messages"`
	MaxTokens      int               `json:"max_tokens,omitempty"`
	Temperature    float64           `json:"temperature"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
	Tools          []Tool            `json:"tools,omitempty"`
	ToolChoice     interface{}       `json:"tool_choice,omitempty"`
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

// do performs the HTTP call and decodes the response.
func (p *ChatProvider) do(ctx context.Context, reqBody chatRequest) (*chatResponse, error) {
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AI provider request failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AI provider returned status %d: %s", resp.StatusCode, string(body))
	}
	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("invalid AI provider response: %v", err)
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return nil, fmt.Errorf("AI provider error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return nil, errors.New("AI provider returned no choices")
	}
	return &cr, nil
}

// Generate sends the prompts to the chat completions endpoint and returns the
// raw content of the first choice. JSON mode is requested so the output can be
// parsed as structured data.
func (p *ChatProvider) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	cr, err := p.do(ctx, chatRequest{
		Model: p.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens:      p.maxTokens,
		Temperature:    0.7,
		ResponseFormat: map[string]string{"type": "json_object"},
	})
	if err != nil {
		return "", err
	}
	if cr.Choices[0].Message.Content == "" {
		return "", errors.New("AI provider returned no content")
	}
	return cr.Choices[0].Message.Content, nil
}

// Chat runs a multi-turn conversation with tool calling enabled and returns
// the model's text plus any requested tool calls.
func (p *ChatProvider) Chat(ctx context.Context, messages []Message, tools []Tool) (*Reply, error) {
	cmsgs := make([]chatMessage, 0, len(messages))
	for _, m := range messages {
		cm := chatMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		for _, tc := range m.ToolCalls {
			ctc := chatToolCall{ID: tc.ID, Type: tc.Type}
			ctc.Function.Name = tc.Function.Name
			ctc.Function.Arguments = tc.Function.Arguments
			cm.ToolCalls = append(cm.ToolCalls, ctc)
		}
		cmsgs = append(cmsgs, cm)
	}
	cr, err := p.do(ctx, chatRequest{
		Model:       p.model,
		Messages:    cmsgs,
		MaxTokens:   p.maxTokens,
		Temperature: 0.4,
		Tools:       tools,
		ToolChoice:  "auto",
	})
	if err != nil {
		return nil, err
	}
	msg := cr.Choices[0].Message
	reply := &Reply{Content: msg.Content}
	for _, ctc := range msg.ToolCalls {
		reply.ToolCalls = append(reply.ToolCalls, ToolCall{
			ID:   ctc.ID,
			Type: ctc.Type,
			Function: ToolCallFunc{
				Name:      ctc.Function.Name,
				Arguments: ctc.Function.Arguments,
			},
		})
	}
	return reply, nil
}
