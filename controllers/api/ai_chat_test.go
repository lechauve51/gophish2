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

type scriptedChatProvider struct {
	replies      []*ai.Reply
	err          error
	calls        int
	lastMessages []ai.Message
	lastTools    []ai.Tool
}

func (m *scriptedChatProvider) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return "", errors.New("Generate not used in chat tests")
}

func (m *scriptedChatProvider) Chat(ctx context.Context, messages []ai.Message, tools []ai.Tool) (*ai.Reply, error) {
	m.calls++
	m.lastMessages = messages
	m.lastTools = tools
	if m.err != nil {
		return nil, m.err
	}
	if len(m.replies) == 0 {
		return &ai.Reply{Content: "done"}, nil
	}
	r := m.replies[0]
	m.replies = m.replies[1:]
	return r, nil
}

func (m *scriptedChatProvider) Model() string { return "scripted-model" }

func toolCall(id, name, args string) ai.ToolCall {
	return ai.ToolCall{ID: id, Type: "function", Function: ai.ToolCallFunc{Name: name, Arguments: args}}
}

func setupChatTest(t *testing.T, p ai.Provider) *testContext {
	ctx := setupTest(t)
	ctx.apiServer = NewServer(
		WithAIProvider(p),
		WithLimiter(ratelimit.NewPostLimiter(ratelimit.WithRequestsPerMinute(100000))),
	)
	return ctx
}

func doChat(t *testing.T, ctx *testContext, method, body string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(method, "/api/ai/chat?api_key="+ctx.apiKey, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx.apiServer.ServeHTTP(rr, req)
	resp := map[string]interface{}{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("error decoding response (status %d): %v body: %s", rr.Code, err, rr.Body.String())
	}
	return rr.Code, resp
}

func chatData(t *testing.T, resp map[string]interface{}) AIChatResponse {
	t.Helper()
	raw, ok := resp["data"]
	if !ok {
		t.Fatalf("no data in response: %v", resp)
	}
	rawJSON, _ := json.Marshal(raw)
	var out AIChatResponse
	if err := json.Unmarshal(rawJSON, &out); err != nil {
		t.Fatalf("error decoding chat data: %v", err)
	}
	return out
}

func TestAIChatSimpleReply(t *testing.T) {
	p := &scriptedChatProvider{replies: []*ai.Reply{{Content: "Bonjour ! Comment puis-je aider ?"}}}
	ctx := setupChatTest(t, p)
	code, resp := doChat(t, ctx, http.MethodPost, `{"message": "salut"}`)
	if code != http.StatusOK || resp["success"] != true {
		t.Fatalf("expected 200 success, got %d %v", code, resp)
	}
	data := chatData(t, resp)
	if data.ConversationID == "" {
		t.Fatal("conversation_id must be returned")
	}
	if len(data.Messages) != 2 || data.Messages[0].Role != "user" || data.Messages[1].Role != "assistant" {
		t.Fatalf("unexpected messages: %+v", data.Messages)
	}
	if p.calls != 1 || len(p.lastTools) == 0 {
		t.Fatalf("provider should be called once with tools, calls=%d tools=%d", p.calls, len(p.lastTools))
	}
	if p.lastMessages[0].Role != "system" {
		t.Fatal("system prompt should be injected first")
	}
}

func TestAIChatBadRequests(t *testing.T) {
	p := &scriptedChatProvider{}
	ctx := setupChatTest(t, p)
	if code, _ := doChat(t, ctx, http.MethodGet, ""); code != http.StatusBadRequest {
		t.Fatalf("GET without conversation_id should be 400, got %d", code)
	}
	if code, _ := doChat(t, ctx, http.MethodGet, ""); code != http.StatusBadRequest {
		t.Fatalf("GET without conversation_id should be 400, got %d", code)
	}
	if code, _ := doChat(t, ctx, http.MethodPut, `{"message": "hi"}`); code != http.StatusBadRequest {
		t.Fatalf("PUT should be 400, got %d", code)
	}
	// GET with a valid conversation returns its messages
	code, resp := doChat(t, ctx, http.MethodPost, `{"message": "hello"}`)
	if code != http.StatusOK {
		t.Fatalf("POST should be 200, got %d", code)
	}
	convID := chatData(t, resp).ConversationID
	req := httptest.NewRequest(http.MethodGet, "/api/ai/chat?api_key="+ctx.apiKey+"&conversation_id="+convID, nil)
	rr := httptest.NewRecorder()
	ctx.apiServer.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET conversation should be 200, got %d", rr.Code)
	}
	getResp := map[string]interface{}{}
	json.NewDecoder(rr.Body).Decode(&getResp)
	data := chatData(t, getResp)
	if len(data.Messages) != 2 {
		t.Fatalf("expected 2 messages on GET, got %d", len(data.Messages))
	}
	req = httptest.NewRequest(http.MethodGet, "/api/ai/chat?api_key="+ctx.apiKey+"&conversation_id=bogus", nil)
	rr = httptest.NewRecorder()
	ctx.apiServer.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("GET bogus conversation should be 404, got %d", rr.Code)
	}
	if code, _ := doChat(t, ctx, http.MethodPost, `not json`); code != http.StatusBadRequest {
		t.Fatalf("bad json should be 400, got %d", code)
	}
	if code, _ := doChat(t, ctx, http.MethodPost, `{"message": "  "}`); code != http.StatusBadRequest {
		t.Fatalf("empty message should be 400, got %d", code)
	}
	if code, _ := doChat(t, ctx, http.MethodPost, `{"conversation_id": "bogus", "message": "hi"}`); code != http.StatusNotFound {
		t.Fatalf("unknown conversation should be 404, got %d", code)
	}

	ctx2 := setupTest(t)
	ctx2.apiServer = NewServer(WithLimiter(ratelimit.NewPostLimiter(ratelimit.WithRequestsPerMinute(100000))))
	if code, _ := doChat(t, ctx2, http.MethodPost, `{"message": "hi"}`); code != http.StatusNotImplemented {
		t.Fatalf("no provider should be 501, got %d", code)
	}
}

func TestAIChatProviderError(t *testing.T) {
	p := &scriptedChatProvider{err: errors.New("boom")}
	ctx := setupChatTest(t, p)
	code, resp := doChat(t, ctx, http.MethodPost, `{"message": "hi"}`)
	if code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d %v", code, resp)
	}
}

func TestAIChatToolCreateGroup(t *testing.T) {
	p := &scriptedChatProvider{replies: []*ai.Reply{
		{Content: "", ToolCalls: []ai.ToolCall{toolCall("c1", "create_group", `{"name": "Chat Group", "targets": [{"email": "a@example.com", "first_name": "A"}, {"email": "b@example.com"}]}`)}},
		{Content: "Groupe créé avec 2 cibles."},
	}}
	ctx := setupChatTest(t, p)
	code, resp := doChat(t, ctx, http.MethodPost, `{"message": "crée un groupe Chat Group avec a@example.com et b@example.com"}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d %v", code, resp)
	}
	g, err := models.GetGroupByName("Chat Group", 1)
	if err != nil {
		t.Fatalf("group not created: %v", err)
	}
	if len(g.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(g.Targets))
	}
	data := chatData(t, resp)
	foundTool := false
	for _, m := range data.Messages {
		if m.Role == "tool" && m.Name == "create_group" && strings.Contains(m.Content, `"targets":2`) {
			foundTool = true
		}
	}
	if !foundTool {
		t.Fatalf("tool result message missing: %+v", data.Messages)
	}
}

func TestAIChatGatedToolsRequireAuthorization(t *testing.T) {
	p := &scriptedChatProvider{replies: []*ai.Reply{
		{ToolCalls: []ai.ToolCall{toolCall("c1", "create_template", `{"name": "T1", "subject": "s", "text": "x {{.URL}}", "html": "<a href=\"{{.URL}}\">x</a>"}`)}},
		{ToolCalls: []ai.ToolCall{toolCall("c2", "create_campaign", `{"name": "C1", "template_name": "T", "page_name": "P", "smtp_name": "S", "groups": ["G"], "url": "https://phish.example.com"}`)}},
		{Content: "il manque une autorisation"},
	}}
	ctx := setupChatTest(t, p)
	code, resp := doChat(t, ctx, http.MethodPost, `{"message": "crée un template et une campagne"}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d %v", code, resp)
	}
	data := chatData(t, resp)
	var toolMsgs []ai.Message
	for _, m := range data.Messages {
		if m.Role == "tool" {
			toolMsgs = append(toolMsgs, m)
		}
	}
	if len(toolMsgs) != 2 {
		t.Fatalf("expected 2 tool results, got %d", len(toolMsgs))
	}
	if !strings.Contains(toolMsgs[0].Content, "authorization") || !strings.Contains(toolMsgs[1].Content, "authorization") {
		t.Fatalf("both gated tools should fail with authorization error: %+v", toolMsgs)
	}
	if ts, _ := models.GetTemplates(1); len(ts) != 0 {
		t.Fatal("no template should be created without authorization")
	}
}

func TestAIChatFullFlowWithAuthorization(t *testing.T) {
	p := &scriptedChatProvider{}
	ctx := setupChatTest(t, p)
	// Pre-existing resources referenced by the campaign
	group := models.Group{Name: "Chat Targets"}
	group.Targets = []models.Target{{BaseRecipient: models.BaseRecipient{Email: "t@example.com", FirstName: "T"}}}
	group.UserId = 1
	if err := models.PostGroup(&group); err != nil {
		t.Fatal(err)
	}
	smtp := models.SMTP{Name: "Chat SMTP", UserId: 1, Host: "smtp.example.com:587", FromAddress: "it@example.com"}
	if err := models.PostSMTP(&smtp); err != nil {
		t.Fatal(err)
	}
	tmpl := models.Template{Name: "Chat Template", UserId: 1, Subject: "s", Text: "x {{.URL}}", HTML: "<a href=\"{{.URL}}\">x</a>"}
	if err := models.PostTemplate(&tmpl); err != nil {
		t.Fatal(err)
	}
	page := models.Page{Name: "Chat Page", UserId: 1, HTML: "<form><input name=\"username\"></form>"}
	if err := models.PostPage(&page); err != nil {
		t.Fatal(err)
	}

	launch := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	p.replies = []*ai.Reply{
		{ToolCalls: []ai.ToolCall{toolCall("c1", "set_authorization", `{"reference": "CHAT-SEC-7"}`)}},
		{ToolCalls: []ai.ToolCall{toolCall("c2", "create_campaign", fmt.Sprintf(`{"name": "Chat Campaign", "template_name": "Chat Template", "page_name": "Chat Page", "smtp_name": "Chat SMTP", "groups": ["Chat Targets"], "url": "https://phish.example.com", "launch_date": %q, "authorization": "CHAT-SEC-7"}`, launch))}},
		{Content: "Campagne créée et planifiée."},
	}
	code, resp := doChat(t, ctx, http.MethodPost, `{"message": "crée la campagne Chat Campaign sur https://phish.example.com, planifiée, avec l'autorisation CHAT-SEC-7"}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d %v", code, resp)
	}
	cs, err := models.GetCampaigns(1)
	if err != nil || len(cs) != 1 {
		t.Fatalf("campaign not created: %v %v", cs, err)
	}
	if cs[0].Name != "Chat Campaign" || cs[0].Status != models.CampaignQueued {
		t.Fatalf("unexpected campaign: %+v", cs[0])
	}
	data := chatData(t, resp)
	last := data.Messages[len(data.Messages)-1]
	if last.Role != "assistant" || last.Content != "Campagne créée et planifiée." {
		t.Fatalf("unexpected final message: %+v", last)
	}
}

func TestAIChatGenerateContentToolIsDryRun(t *testing.T) {
	gen := &mockAIProvider{response: mockGeneratedJSON}
	p := &scriptedChatProvider{}
	_ = gen
	p.replies = []*ai.Reply{
		{ToolCalls: []ai.ToolCall{toolCall("c1", "generate_content", `{"scenario": "invoice_fraud", "language": "fr"}`)}},
		{Content: "voici le preview"},
	}
	// use a provider that can do both: wrap scripted with generate support
	ctx := setupChatTest(t, &dualProvider{chat: p, gen: mockGeneratedJSON})
	code, resp := doChat(t, ctx, http.MethodPost, `{"message": "génère un mail de fraude à la facture en français"}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d %v", code, resp)
	}
	data := chatData(t, resp)
	found := false
	for _, m := range data.Messages {
		if m.Role == "tool" && m.Name == "generate_content" && strings.Contains(m.Content, "email_subject") && strings.Contains(m.Content, "dry run") {
			found = true
		}
	}
	if !found {
		t.Fatalf("generate_content tool result missing: %+v", data.Messages)
	}
	if ts, _ := models.GetTemplates(1); len(ts) != 0 {
		t.Fatal("generate_content must stay a dry run")
	}
}

type dualProvider struct {
	chat *scriptedChatProvider
	gen  string
}

func (d *dualProvider) Generate(ctx context.Context, s, u string) (string, error) { return d.gen, nil }
func (d *dualProvider) Chat(ctx context.Context, m []ai.Message, t []ai.Tool) (*ai.Reply, error) {
	return d.chat.Chat(ctx, m, t)
}
func (d *dualProvider) Model() string { return "dual" }

func TestAIChatIterationCap(t *testing.T) {
	p := &scriptedChatProvider{}
	alwaysTool := &ai.Reply{ToolCalls: []ai.ToolCall{toolCall("c", "list_templates", `{}`)}}
	ctx := setupChatTest(t, &loopProvider{base: p, reply: alwaysTool})
	code, resp := doChat(t, ctx, http.MethodPost, `{"message": "liste les templates encore et encore"}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200 after cap, got %d %v", code, resp)
	}
	if p.calls != maxChatIterations {
		t.Fatalf("expected %d provider calls, got %d", maxChatIterations, p.calls)
	}
}

type loopProvider struct {
	base  *scriptedChatProvider
	reply *ai.Reply
}

func (l *loopProvider) Generate(ctx context.Context, s, u string) (string, error) {
	return "", errors.New("unused")
}
func (l *loopProvider) Chat(ctx context.Context, m []ai.Message, t []ai.Tool) (*ai.Reply, error) {
	l.base.calls++
	return l.reply, nil
}
func (l *loopProvider) Model() string { return "loop" }

func TestAIChatConversationPersistsAcrossTurns(t *testing.T) {
	p := &scriptedChatProvider{replies: []*ai.Reply{
		{Content: "réponse 1"},
		{Content: "réponse 2"},
	}}
	ctx := setupChatTest(t, p)
	code, resp := doChat(t, ctx, http.MethodPost, `{"message": "tour 1"}`)
	if code != http.StatusOK {
		t.Fatalf("got %d", code)
	}
	convID := chatData(t, resp).ConversationID
	code, resp = doChat(t, ctx, http.MethodPost, fmt.Sprintf(`{"conversation_id": %q, "message": "tour 2"}`, convID))
	if code != http.StatusOK {
		t.Fatalf("got %d", code)
	}
	data := chatData(t, resp)
	if data.ConversationID != convID {
		t.Fatal("conversation id changed")
	}
	if len(data.Messages) != 4 {
		t.Fatalf("expected 4 messages after 2 turns, got %d: %+v", len(data.Messages), data.Messages)
	}
}
