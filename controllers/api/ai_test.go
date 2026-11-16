package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gophish/gophish/ai"
	"github.com/gophish/gophish/middleware/ratelimit"
	"github.com/gophish/gophish/models"
)

type mockAIProvider struct {
	response string
	err      error
	calls    int
}

func (m *mockAIProvider) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	m.calls++
	return m.response, m.err
}

func (m *mockAIProvider) Chat(ctx context.Context, messages []ai.Message, tools []ai.Tool) (*ai.Reply, error) {
	m.calls++
	return nil, m.err
}

func (m *mockAIProvider) Model() string { return "mock-model" }

const mockGeneratedJSON = `{
	"email_subject": "Invoice overdue {{.FirstName}}",
	"email_text": "Hello {{.FirstName}}, please review your invoice here: {{.URL}}",
	"email_html": "<html><body>Hello {{.FirstName}}, <a href=\"{{.URL}}\">review your invoice</a></body></html>",
	"landing_html": "<html><body><form><input name=\"username\"><input type=\"password\" name=\"password\"></form></body></html>",
	"landing_redirect_url": "https://example.com/training",
	"capture_passwords": true,
	"rationale": "Invoice fraud scenario for audit."
}`

func setupAITest(t *testing.T, provider *mockAIProvider) *testContext {
	ctx := setupTest(t)
	ctx.apiServer = NewServer(
		WithAIProvider(provider),
		WithLimiter(ratelimit.NewPostLimiter(ratelimit.WithRequestsPerMinute(100000))),
	)
	return ctx
}

func doAIRequest(t *testing.T, ctx *testContext, method, path, body string) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(method, path+"?api_key="+ctx.apiKey, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx.apiServer.ServeHTTP(rr, req)
	resp := map[string]interface{}{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("error decoding response (status %d): %v body: %s", rr.Code, err, rr.Body.String())
	}
	return rr, resp
}

func aiResultData(t *testing.T, resp map[string]interface{}) AIResult {
	t.Helper()
	raw, ok := resp["data"]
	if !ok {
		t.Fatalf("response has no data field: %v", resp)
	}
	rawJSON, _ := json.Marshal(raw)
	var result AIResult
	if err := json.Unmarshal(rawJSON, &result); err != nil {
		t.Fatalf("error decoding data: %v", err)
	}
	return result
}

func TestAIGenerateDryRun(t *testing.T) {
	mock := &mockAIProvider{response: mockGeneratedJSON}
	ctx := setupAITest(t, mock)

	rr, resp := doAIRequest(t, ctx, http.MethodPost, "/api/ai/generate",
		`{"scenario": "invoice_fraud", "industry": "finance", "language": "fr", "tone": "urgent"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", rr.Code, resp)
	}
	if resp["success"] != true {
		t.Fatalf("expected success, got %v", resp)
	}
	result := aiResultData(t, resp)
	if result.Applied {
		t.Fatal("generate endpoint must never apply")
	}
	if result.Content.EmailSubject == "" || result.Content.LandingHTML == "" {
		t.Fatalf("content not populated: %+v", result.Content)
	}
	if result.TemplateName == "" || result.PageName == "" {
		t.Fatal("default names should be proposed")
	}
	if mock.calls != 1 {
		t.Fatalf("expected 1 provider call, got %d", mock.calls)
	}
	// Dry run: nothing in the DB
	ts, err := models.GetTemplates(1)
	if err != nil || len(ts) != 0 {
		t.Fatalf("dry run must not create templates: %v %v", ts, err)
	}
	ps, err := models.GetPages(1)
	if err != nil || len(ps) != 0 {
		t.Fatalf("dry run must not create pages: %v %v", ps, err)
	}
}

func TestAIGenerateNoProvider(t *testing.T) {
	ctx := setupTest(t)
	ctx.apiServer = NewServer(WithLimiter(ratelimit.NewPostLimiter(ratelimit.WithRequestsPerMinute(100000))))
	rr, resp := doAIRequest(t, ctx, http.MethodPost, "/api/ai/generate", `{"scenario": "x"}`)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %v", rr.Code, resp)
	}
}

func TestAIGenerateBadRequests(t *testing.T) {
	mock := &mockAIProvider{response: mockGeneratedJSON}
	ctx := setupAITest(t, mock)

	if rr, _ := doAIRequest(t, ctx, http.MethodGet, "/api/ai/generate", ""); rr.Code != http.StatusBadRequest {
		t.Fatalf("GET should be rejected, got %d", rr.Code)
	}
	if rr, _ := doAIRequest(t, ctx, http.MethodPost, "/api/ai/generate", `not json`); rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON should be 400, got %d", rr.Code)
	}
	if rr, _ := doAIRequest(t, ctx, http.MethodPost, "/api/ai/campaign", `not json`); rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON should be 400, got %d", rr.Code)
	}
}

func TestAIGenerateValidationError(t *testing.T) {
	bad := strings.Replace(mockGeneratedJSON, `{{.URL}}`, `{{.Nope}}`, -1)
	mock := &mockAIProvider{response: bad}
	ctx := setupAITest(t, mock)
	rr, resp := doAIRequest(t, ctx, http.MethodPost, "/api/ai/generate", `{"scenario": "x"}`)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %v", rr.Code, resp)
	}
}

func TestAIGenerateProviderError(t *testing.T) {
	mock := &mockAIProvider{err: errors.New("connection refused")}
	ctx := setupAITest(t, mock)
	rr, resp := doAIRequest(t, ctx, http.MethodPost, "/api/ai/generate", `{"scenario": "x"}`)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %v", rr.Code, resp)
	}
}

func TestAIGenerateMalformedModelOutput(t *testing.T) {
	mock := &mockAIProvider{response: "sorry, I cannot do that"}
	ctx := setupAITest(t, mock)
	rr, _ := doAIRequest(t, ctx, http.MethodPost, "/api/ai/generate", `{"scenario": "x"}`)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rr.Code)
	}
}

func TestAICampaignDryRunByDefault(t *testing.T) {
	mock := &mockAIProvider{response: mockGeneratedJSON}
	ctx := setupAITest(t, mock)
	rr, resp := doAIRequest(t, ctx, http.MethodPost, "/api/ai/campaign", `{"scenario": "invoice_fraud"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", rr.Code, resp)
	}
	result := aiResultData(t, resp)
	if result.Applied || result.TemplateID != 0 || result.PageID != 0 || result.CampaignID != 0 {
		t.Fatalf("dry run must not create anything: %+v", result)
	}
	ts, _ := models.GetTemplates(1)
	if len(ts) != 0 {
		t.Fatal("no template should exist after dry run")
	}
}

func TestAICampaignApplyRequiresAuthorization(t *testing.T) {
	mock := &mockAIProvider{response: mockGeneratedJSON}
	ctx := setupAITest(t, mock)
	rr, resp := doAIRequest(t, ctx, http.MethodPost, "/api/ai/campaign",
		`{"scenario": "invoice_fraud", "apply": true}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %v", rr.Code, resp)
	}
	if !strings.Contains(fmt.Sprint(resp["message"]), "authorization") {
		t.Fatalf("error message should mention authorization: %v", resp)
	}
}

func TestAICampaignApplyCreatesTemplateAndPage(t *testing.T) {
	mock := &mockAIProvider{response: mockGeneratedJSON}
	ctx := setupAITest(t, mock)

	body := `{
		"scenario": "invoice_fraud",
		"apply": true,
		"authorization": "ticket SEC-1234",
		"template_name": "AI Test Template",
		"page_name": "AI Test Page"
	}`
	rr, resp := doAIRequest(t, ctx, http.MethodPost, "/api/ai/campaign", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", rr.Code, resp)
	}
	result := aiResultData(t, resp)
	if !result.Applied || result.TemplateID == 0 || result.PageID == 0 {
		t.Fatalf("expected applied result with IDs: %+v", result)
	}

	tmpl, err := models.GetTemplateByName("AI Test Template", 1)
	if err != nil {
		t.Fatalf("template not created: %v", err)
	}
	if tmpl.Subject != "Invoice overdue {{.FirstName}}" {
		t.Fatalf("unexpected subject: %s", tmpl.Subject)
	}
	if tmpl.HTML == "" || tmpl.Text == "" {
		t.Fatal("template body not saved")
	}

	page, err := models.GetPageByName("AI Test Page", 1)
	if err != nil {
		t.Fatalf("page not created: %v", err)
	}
	if !page.CaptureCredentials || !page.CapturePasswords {
		t.Fatalf("expected credential+password capture, got %+v", page)
	}
	if page.RedirectURL != "https://example.com/training" {
		t.Fatalf("unexpected redirect URL: %s", page.RedirectURL)
	}
	// parseHTML must have rewritten the form action to the local capture endpoint
	if !strings.Contains(page.HTML, `action=""`) {
		t.Fatalf("form action should be rewritten, got: %s", page.HTML)
	}
}

func TestAICampaignApplyWithReviewedContent(t *testing.T) {
	// The provider must not be called when the caller supplies reviewed content.
	mock := &mockAIProvider{err: errors.New("should not be called")}
	ctx := setupAITest(t, mock)

	content, _ := json.Marshal(map[string]string{})
	_ = content
	body := fmt.Sprintf(`{
		"apply": true,
		"authorization": "ticket SEC-99",
		"template_name": "AI Reviewed Template",
		"page_name": "AI Reviewed Page",
		"content": %s
	}`, mockGeneratedJSON)
	rr, resp := doAIRequest(t, ctx, http.MethodPost, "/api/ai/campaign", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", rr.Code, resp)
	}
	if mock.calls != 0 {
		t.Fatalf("provider should not be called when content is supplied, calls=%d", mock.calls)
	}
	if _, err := models.GetTemplateByName("AI Reviewed Template", 1); err != nil {
		t.Fatalf("template not created: %v", err)
	}
}

func TestAICampaignApplyInvalidContentRejected(t *testing.T) {
	mock := &mockAIProvider{}
	ctx := setupAITest(t, mock)
	bad := strings.Replace(mockGeneratedJSON, `{{.URL}}`, `{{.URL2}}`, -1)
	body := fmt.Sprintf(`{"apply": true, "authorization": "ref", "content": %s}`, bad)
	rr, _ := doAIRequest(t, ctx, http.MethodPost, "/api/ai/campaign", body)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rr.Code)
	}
}

func TestAICampaignApplyConflict(t *testing.T) {
	mock := &mockAIProvider{response: mockGeneratedJSON}
	ctx := setupAITest(t, mock)
	body := `{"apply": true, "authorization": "ref", "template_name": "Dup", "page_name": "DupPage"}`
	if rr, _ := doAIRequest(t, ctx, http.MethodPost, "/api/ai/campaign", body); rr.Code != http.StatusCreated {
		t.Fatalf("first apply should succeed, got %d", rr.Code)
	}
	rr, resp := doAIRequest(t, ctx, http.MethodPost, "/api/ai/campaign", body)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 on duplicate name, got %d: %v", rr.Code, resp)
	}
}

func TestAICampaignApplyFullCampaign(t *testing.T) {
	mock := &mockAIProvider{response: mockGeneratedJSON}
	ctx := setupAITest(t, mock)

	group := models.Group{Name: "AI Targets"}
	group.Targets = []models.Target{
		{BaseRecipient: models.BaseRecipient{Email: "a@example.com", FirstName: "A"}},
	}
	group.UserId = 1
	if err := models.PostGroup(&group); err != nil {
		t.Fatalf("error creating group: %v", err)
	}
	smtp := models.SMTP{Name: "AI SMTP", UserId: 1, Host: "example.com", FromAddress: "it@example.com"}
	if err := models.PostSMTP(&smtp); err != nil {
		t.Fatalf("error creating smtp: %v", err)
	}

	launch := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	body := fmt.Sprintf(`{
		"apply": true,
		"authorization": "ticket SEC-42",
		"template_name": "AI Camp Template",
		"page_name": "AI Camp Page",
		"campaign_name": "AI Test Campaign",
		"url": "https://phish.example.com",
		"smtp_name": "AI SMTP",
		"groups": ["AI Targets"],
		"launch_date": %q
	}`, launch)
	rr, resp := doAIRequest(t, ctx, http.MethodPost, "/api/ai/campaign", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", rr.Code, resp)
	}
	result := aiResultData(t, resp)
	if result.CampaignID == 0 {
		t.Fatalf("expected a campaign ID: %+v", result)
	}
	c, err := models.GetCampaign(result.CampaignID, 1)
	if err != nil {
		t.Fatalf("campaign not created: %v", err)
	}
	if c.Name != "AI Test Campaign" || c.URL != "https://phish.example.com" {
		t.Fatalf("unexpected campaign: %+v", c)
	}
	// Scheduled in the future: must stay queued and not launch the worker
	if c.Status != models.CampaignQueued {
		t.Fatalf("expected queued status, got %s", c.Status)
	}
	if c.Template.Name != "AI Camp Template" || c.Page.Name != "AI Camp Page" {
		t.Fatalf("campaign not linked to generated template/page: %+v", c)
	}
}

func TestAICampaignMissingFields(t *testing.T) {
	mock := &mockAIProvider{response: mockGeneratedJSON}
	ctx := setupAITest(t, mock)
	body := `{"apply": true, "authorization": "ref", "campaign_name": "Incomplete"}`
	rr, resp := doAIRequest(t, ctx, http.MethodPost, "/api/ai/campaign", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %v", rr.Code, resp)
	}
}

func TestAIConfigEndpoint(t *testing.T) {
	mock := &mockAIProvider{response: mockGeneratedJSON}
	ctx := setupAITest(t, mock)
	rr, resp := doAIRequest(t, ctx, http.MethodGet, "/api/ai/config", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if resp["enabled"] != true || resp["model"] != "mock-model" {
		t.Fatalf("unexpected config response: %v", resp)
	}
	if strings.Contains(rr.Body.String(), "api_key") || strings.Contains(rr.Body.String(), "secret") {
		t.Fatalf("config endpoint must not leak keys: %s", rr.Body.String())
	}

	// Without a provider
	ctx2 := setupTest(t)
	ctx2.apiServer = NewServer()
	rr2, resp2 := doAIRequest(t, ctx2, http.MethodGet, "/api/ai/config", "")
	if rr2.Code != http.StatusOK || resp2["enabled"] != false {
		t.Fatalf("expected disabled config, got %d %v", rr2.Code, resp2)
	}
}

func TestAIRequiresAPIKey(t *testing.T) {
	mock := &mockAIProvider{response: mockGeneratedJSON}
	ctx := setupAITest(t, mock)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/generate", bytes.NewBufferString(`{"scenario":"x"}`))
	rr := httptest.NewRecorder()
	ctx.apiServer.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without API key, got %d", rr.Code)
	}
}
