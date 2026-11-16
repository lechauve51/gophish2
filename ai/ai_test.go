package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gophish/gophish/config"
)

const validGeneratedJSON = `{
	"email_subject": "Invoice overdue {{.FirstName}}",
	"email_text": "Hello {{.FirstName}}, please review your invoice here: {{.URL}}",
	"email_html": "<html><body>Hello {{.FirstName}}, <a href=\"{{.URL}}\">review your invoice</a></body></html>",
	"landing_html": "<html><body><form><input name=\"username\"><input type=\"password\" name=\"password\"></form></body></html>",
	"landing_redirect_url": "https://example.com/training",
	"capture_passwords": true,
	"rationale": "Classic invoice fraud scenario."
}`

func validContent(t *testing.T) *GeneratedContent {
	t.Helper()
	g, err := ParseGenerated(validGeneratedJSON)
	if err != nil {
		t.Fatalf("failed to parse valid content: %v", err)
	}
	return g
}

func TestParseGenerated(t *testing.T) {
	g, err := ParseGenerated(validGeneratedJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.EmailSubject == "" || g.LandingHTML == "" {
		t.Fatalf("fields not populated: %+v", g)
	}

	fenced := "```json\n" + validGeneratedJSON + "\n```"
	if _, err := ParseGenerated(fenced); err != nil {
		t.Fatalf("fenced JSON should parse: %v", err)
	}

	wrapped := "Here is the result: " + validGeneratedJSON + " hope this helps!"
	if _, err := ParseGenerated(wrapped); err != nil {
		t.Fatalf("wrapped JSON should parse: %v", err)
	}

	if _, err := ParseGenerated("no json at all"); err == nil {
		t.Fatal("expected error for non-JSON output")
	}
	if _, err := ParseGenerated("{invalid json}"); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidateContentValid(t *testing.T) {
	if err := ValidateContent(validContent(t)); err != nil {
		t.Fatalf("valid content rejected: %v", err)
	}
}

func TestValidateContentFailures(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(g *GeneratedContent)
	}{
		{"empty subject", func(g *GeneratedContent) { g.EmailSubject = "  " }},
		{"no email body", func(g *GeneratedContent) { g.EmailText = ""; g.EmailHTML = "" }},
		{"missing tracking url", func(g *GeneratedContent) {
			g.EmailText = "no link here"
			g.EmailHTML = "<html><body>no link</body></html>"
		}},
		{"unsupported variable", func(g *GeneratedContent) { g.EmailHTML = "<a href=\"{{.URL}}\">{{.Password}}</a>" }},
		{"landing without form", func(g *GeneratedContent) { g.LandingHTML = "<html><body>nothing</body></html>" }},
		{"empty landing", func(g *GeneratedContent) { g.LandingHTML = " " }},
		{"script in email html", func(g *GeneratedContent) {
			g.EmailHTML = "<html><body><script>alert(1)</script><a href=\"{{.URL}}\">x</a></body></html>"
		}},
		{"subject too long", func(g *GeneratedContent) { g.EmailSubject = strings.Repeat("a", MaxSubjectBytes+1) }},
		{"html too large", func(g *GeneratedContent) {
			g.EmailHTML = "<a href=\"{{.URL}}\">" + strings.Repeat("a", MaxEmailHTMLBytes) + "</a>"
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := validContent(t)
			c.mutate(g)
			if err := ValidateContent(g); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestValidateContentAllowsAllDocumentedVariables(t *testing.T) {
	g := validContent(t)
	g.EmailHTML = `<a href="{{.URL}}">{{.FirstName}} {{.LastName}} {{.Email}} {{.Position}} {{.From}} {{.Tracker}} {{.TrackingURL}} {{.RId}} {{.BaseURL}}</a>`
	if err := ValidateContent(g); err != nil {
		t.Fatalf("all whitelisted variables should be accepted: %v", err)
	}
}

func TestSystemPrompt(t *testing.T) {
	sp := SystemPrompt()
	for _, v := range AllowedVariables {
		if !strings.Contains(sp, "{{."+v+"}}") {
			t.Fatalf("system prompt missing variable {{.%s}}", v)
		}
	}
	for _, key := range []string{"email_subject", "email_text", "email_html", "landing_html", "landing_redirect_url", "capture_passwords", "rationale"} {
		if !strings.Contains(sp, key) {
			t.Fatalf("system prompt missing schema key %s", key)
		}
	}
}

func TestUserPromptDefaultsLanguage(t *testing.T) {
	b := &Brief{Scenario: "invoice_fraud"}
	up, err := UserPrompt(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Language != "en" {
		t.Fatalf("expected default language en, got %s", b.Language)
	}
	if !strings.Contains(up, "invoice_fraud") {
		t.Fatalf("user prompt should contain the brief: %s", up)
	}
	if !strings.Contains(up, `"language":"en"`) {
		t.Fatalf("user prompt should contain the defaulted language: %s", up)
	}
}

func TestNewProviderFromConfig(t *testing.T) {
	if p := NewProviderFromConfig(config.AIConfig{}); p != nil {
		t.Fatal("disabled config should yield nil provider")
	}
	if p := NewProviderFromConfig(config.AIConfig{Enabled: true, BaseURL: "http://localhost:1234/v1"}); p != nil {
		t.Fatal("missing model should yield nil provider")
	}
	p := NewProviderFromConfig(config.AIConfig{Enabled: true, BaseURL: "http://localhost:1234/v1", Model: "m"})
	if p == nil {
		t.Fatal("expected a provider")
	}
	if p.Model() != "m" {
		t.Fatalf("unexpected model %s", p.Model())
	}
}

func TestChatProviderGenerate(t *testing.T) {
	var gotAuth, gotPath, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		b, _ := json.Marshal(req)
		gotBody = string(b)
		fmt.Fprintf(w, `{"choices": [{"message": {"role": "assistant", "content": %s}}]}`,
			strconv.Quote(validGeneratedJSON))
	}))
	defer ts.Close()

	p := NewChatProvider(config.AIConfig{
		BaseURL:        ts.URL + "/v1/",
		Model:          "test-model",
		APIKey:         "secret-key",
		MaxTokens:      123,
		TimeoutSeconds: 5,
	})
	out, err := p.Generate(context.Background(), "system", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != validGeneratedJSON {
		t.Fatalf("unexpected output: %s", out)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("unexpected path %s", gotPath)
	}
	if gotAuth != "Bearer secret-key" {
		t.Fatalf("unexpected auth header %s", gotAuth)
	}
	if !strings.Contains(gotBody, `"model":"test-model"`) || !strings.Contains(gotBody, `"max_tokens":123`) {
		t.Fatalf("unexpected request body %s", gotBody)
	}
	if !strings.Contains(gotBody, `"response_format":{"type":"json_object"}`) {
		t.Fatalf("JSON mode not requested: %s", gotBody)
	}
}

func TestChatProviderErrors(t *testing.T) {
	// HTTP error status
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error": {"message": "boom"}}`)
	}))
	defer ts.Close()
	p := NewChatProvider(config.AIConfig{BaseURL: ts.URL, Model: "m", TimeoutSeconds: 5})
	if _, err := p.Generate(context.Background(), "s", "u"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected status error, got %v", err)
	}

	// API-level error payload with 200
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"error": {"message": "quota exceeded"}}`)
	}))
	defer ts2.Close()
	p2 := NewChatProvider(config.AIConfig{BaseURL: ts2.URL, Model: "m", TimeoutSeconds: 5})
	if _, err := p2.Generate(context.Background(), "s", "u"); err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("expected provider error, got %v", err)
	}

	// Empty choices
	ts3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices": []}`)
	}))
	defer ts3.Close()
	p3 := NewChatProvider(config.AIConfig{BaseURL: ts3.URL, Model: "m", TimeoutSeconds: 5})
	if _, err := p3.Generate(context.Background(), "s", "u"); err == nil {
		t.Fatal("expected error for empty choices")
	}

	// Invalid JSON
	ts4 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	}))
	defer ts4.Close()
	p4 := NewChatProvider(config.AIConfig{BaseURL: ts4.URL, Model: "m", TimeoutSeconds: 5})
	if _, err := p4.Generate(context.Background(), "s", "u"); err == nil {
		t.Fatal("expected error for invalid JSON")
	}

	// Timeout
	ts5 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		fmt.Fprint(w, `{}`)
	}))
	defer ts5.Close()
	p5 := NewChatProvider(config.AIConfig{BaseURL: ts5.URL, Model: "m", TimeoutSeconds: 1})
	if _, err := p5.Generate(context.Background(), "s", "u"); err == nil {
		t.Fatal("expected timeout error")
	}
}
