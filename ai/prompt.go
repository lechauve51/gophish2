package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SystemPrompt returns the system prompt sent to the model. It constrains the
// output to a strict JSON schema and to the Gophish template variables that
// are actually supported.
func SystemPrompt() string {
	allowed := make([]string, len(AllowedVariables))
	for i, v := range AllowedVariables {
		allowed[i] = "{{." + v + "}}"
	}
	return fmt.Sprintf(`You are an assistant embedded in Gophish, a phishing simulation platform used by authorized security teams for employee security awareness training. You generate email and landing page content exclusively for AUTHORIZED simulation campaigns.

Rules:
- Respond with STRICT JSON only, no markdown fences, no commentary, matching exactly this schema:
  {"email_subject": string, "email_text": string, "email_html": string, "landing_html": string, "landing_redirect_url": string, "capture_passwords": bool, "rationale": string}
- "email_html" must be a complete, self-contained HTML email (inline CSS only, no external resources, NO <script> tags) and MUST contain the tracking link {{.URL}} inside an <a href="{{.URL}}"> element.
- "email_text" is the plaintext fallback and must also contain {{.URL}}.
- "landing_html" must be a complete HTML page containing at least one <form> with an <input name="username"> and, when "capture_passwords" is true, an <input type="password" name="password">, styled to imitate the impersonated organization.
- You may ONLY use these template variables: %s. Never invent other variables.
- Personalize with {{.FirstName}} / {{.LastName}} / {{.Position}} where natural.
- Write everything in the requested language, with the requested tone, for the requested audience, industry and scenario.
- Keep "email_html" under 100 KB and "landing_html" under 500 KB.
- "rationale" is a one or two sentence explanation of the chosen scenario, kept for audit purposes.`, strings.Join(allowed, ", "))
}

// UserPrompt builds the user prompt from a brief. An empty language defaults
// to English.
func UserPrompt(b *Brief) (string, error) {
	if b.Language == "" {
		b.Language = "en"
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Generate a phishing simulation email template and landing page for this brief (JSON): %s\nReturn JSON only.", string(raw)), nil
}
