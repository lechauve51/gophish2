package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Size limits enforced on generated content.
const (
	MaxSubjectBytes     = 998
	MaxEmailTextBytes   = 256 * 1024
	MaxEmailHTMLBytes   = 512 * 1024
	MaxLandingHTMLBytes = 1024 * 1024
)

var (
	templateVarRegex = regexp.MustCompile(`\{\{-?\s*\.([A-Za-z_][A-Za-z0-9_]*)`)
	urlVarRegex      = regexp.MustCompile(`\{\{-?\s*\.URL\s*\}\}`)
	scriptRegex      = regexp.MustCompile(`(?i)<script`)
	formRegex        = regexp.MustCompile(`(?i)<form[\s>]`)

	allowedVars = func() map[string]bool {
		m := make(map[string]bool, len(AllowedVariables))
		for _, v := range AllowedVariables {
			m[v] = true
		}
		return m
	}()
)

// ParseGenerated extracts the JSON object from raw model output, tolerating
// markdown fences or surrounding text.
func ParseGenerated(raw string) (*GeneratedContent, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, errors.New("no JSON object found in AI response")
	}
	g := &GeneratedContent{}
	if err := json.Unmarshal([]byte(raw[start:end+1]), g); err != nil {
		return nil, fmt.Errorf("invalid JSON in AI response: %v", err)
	}
	return g, nil
}

// ValidateContent enforces the content policy on generated material: size
// limits, whitelist of template variables, presence of the tracking link in
// the email, presence of a form in the landing page, and no scripts in email
// HTML.
func ValidateContent(g *GeneratedContent) error {
	if strings.TrimSpace(g.EmailSubject) == "" {
		return errors.New("generated email subject is empty")
	}
	if len(g.EmailSubject) > MaxSubjectBytes {
		return errors.New("generated email subject exceeds the maximum length")
	}
	if g.EmailText == "" && g.EmailHTML == "" {
		return errors.New("generated email has neither text nor HTML content")
	}
	if len(g.EmailText) > MaxEmailTextBytes {
		return errors.New("generated email text exceeds the maximum size")
	}
	if len(g.EmailHTML) > MaxEmailHTMLBytes {
		return errors.New("generated email HTML exceeds the maximum size")
	}
	if strings.TrimSpace(g.LandingHTML) == "" {
		return errors.New("generated landing page is empty")
	}
	if len(g.LandingHTML) > MaxLandingHTMLBytes {
		return errors.New("generated landing page exceeds the maximum size")
	}
	fields := map[string]string{
		"email_subject":        g.EmailSubject,
		"email_text":           g.EmailText,
		"email_html":           g.EmailHTML,
		"landing_html":         g.LandingHTML,
		"landing_redirect_url": g.LandingRedirectURL,
	}
	for name, content := range fields {
		for _, m := range templateVarRegex.FindAllStringSubmatch(content, -1) {
			if !allowedVars[m[1]] {
				return fmt.Errorf("field %s uses unsupported template variable {{.%s}}", name, m[1])
			}
		}
	}
	if !urlVarRegex.MatchString(g.EmailHTML) && !urlVarRegex.MatchString(g.EmailText) {
		return errors.New("generated email must contain the {{.URL}} tracking link")
	}
	if !formRegex.MatchString(g.LandingHTML) {
		return errors.New("generated landing page must contain a <form>")
	}
	if g.EmailHTML != "" && scriptRegex.MatchString(g.EmailHTML) {
		return errors.New("generated email HTML must not contain <script> tags")
	}
	return nil
}
