// Package ai provides AI-assisted generation of phishing simulation content
// (email templates and landing pages) for authorized security awareness
// campaigns. It talks to any OpenAI-compatible /chat/completions endpoint,
// so any model provider can be plugged in through configuration (OpenAI,
// Mistral, Anthropic-compatible gateways, Ollama, vLLM, LM Studio, etc.).
package ai

import (
	"context"
	"errors"

	"github.com/gophish/gophish/config"
)

// ErrProvider marks errors coming from the upstream AI provider itself, as
// opposed to validation errors on the generated content.
var ErrProvider = errors.New("AI provider error")

// AllowedVariables is the whitelist of Gophish template variables that
// generated content is allowed to use.
var AllowedVariables = []string{
	"From", "URL", "Tracker", "TrackingURL", "RId", "BaseURL",
	"Email", "FirstName", "LastName", "Position",
}

// Brief describes the simulation content to generate.
type Brief struct {
	Scenario         string `json:"scenario"`
	Industry         string `json:"industry"`
	Language         string `json:"language"`
	Tone             string `json:"tone"`
	TargetAudience   string `json:"target_audience"`
	SenderName       string `json:"sender_name"`
	CompanyName      string `json:"company_name"`
	CapturePasswords bool   `json:"capture_passwords"`
	RedirectURL      string `json:"redirect_url"`
}

// GeneratedContent is the structured JSON output expected from the model.
type GeneratedContent struct {
	EmailSubject       string `json:"email_subject"`
	EmailText          string `json:"email_text"`
	EmailHTML          string `json:"email_html"`
	LandingHTML        string `json:"landing_html"`
	LandingRedirectURL string `json:"landing_redirect_url"`
	CapturePasswords   bool   `json:"capture_passwords"`
	Rationale          string `json:"rationale"`
}

// Provider generates raw text completions from prompts and multi-turn chat
// replies with tool calls.
type Provider interface {
	Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	Chat(ctx context.Context, messages []Message, tools []Tool) (*Reply, error)
	Model() string
}

const (
	// DefaultMaxTokens is used when the config does not specify one.
	DefaultMaxTokens = 4096
	// DefaultTimeoutSeconds is used when the config does not specify one.
	DefaultTimeoutSeconds = 60
)

// NewProviderFromConfig builds a Provider from the AI configuration section.
// It returns nil when the feature is disabled or incompletely configured.
func NewProviderFromConfig(conf config.AIConfig) Provider {
	if !conf.Enabled || conf.BaseURL == "" || conf.Model == "" {
		return nil
	}
	return NewChatProvider(conf)
}
