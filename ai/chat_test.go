package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gophish/gophish/config"
)

func TestConversationStore(t *testing.T) {
	store := NewConversationStore(time.Minute)
	c := store.Get("")
	if c == nil || c.ID == "" {
		t.Fatal("empty id should create a conversation")
	}
	c.Append(Message{Role: "user", Content: "hello"})
	c.SetAuthorization("SEC-1")

	again := store.Get(c.ID)
	if again == nil {
		t.Fatal("conversation should be retrievable")
	}
	if len(again.Messages) != 1 || again.Messages[0].Content != "hello" {
		t.Fatalf("messages not persisted: %+v", again.Messages)
	}
	if again.Authorization != "SEC-1" {
		t.Fatalf("authorization not persisted: %q", again.Authorization)
	}
	if store.Get("bogus") != nil {
		t.Fatal("unknown id should return nil")
	}

	expired := NewConversationStore(-1)
	ec := expired.Get("")
	ec.LastUsed = time.Now().Add(-48 * time.Hour)
	if expired.Get(ec.ID) != nil {
		t.Fatal("expired conversation should be gone")
	}
}

func TestToolsDefinitions(t *testing.T) {
	tools := Tools()
	seen := map[string]bool{}
	for _, tool := range tools {
		if tool.Type != "function" {
			t.Fatalf("unexpected tool type %q", tool.Type)
		}
		name := tool.Function.Name
		if name == "" || seen[name] {
			t.Fatalf("bad or duplicate tool name %q", name)
		}
		seen[name] = true
		if tool.Function.Description == "" {
			t.Fatalf("tool %s has no description", name)
		}
		if tool.Function.Parameters["type"] != "object" {
			t.Fatalf("tool %s parameters must be an object schema", name)
		}
	}
	for _, want := range []string{"get_server_info", "list_templates", "list_pages", "list_groups", "list_sending_profiles", "list_campaigns", "get_campaign_summary", "generate_content", "create_template", "create_page", "create_group", "create_sending_profile", "create_campaign", "send_test_email", "set_authorization"} {
		if !seen[want] {
			t.Fatalf("missing tool %s", want)
		}
	}
}

func TestChatSystemPrompt(t *testing.T) {
	sp := ChatSystemPrompt()
	for _, want := range []string{"authorization", "generate_content", "campaign", "send_test_email", "{{.URL}}", "AUTHORIZED"} {
		if !strings.Contains(sp, want) {
			t.Fatalf("chat system prompt missing %q", want)
		}
	}
}

func TestChatProviderChatWithTools(t *testing.T) {
	var gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf)
		gotBody = string(buf)
		fmt.Fprint(w, `{"choices": [{"message": {"role": "assistant", "content": "Je vais créer le groupe.", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "create_group", "arguments": "{\"name\": \"G\"}"}}]}}]}`)
	}))
	defer ts.Close()

	p := NewChatProvider(config.AIConfig{BaseURL: ts.URL, Model: "m", APIKey: "k", TimeoutSeconds: 5})
	reply, err := p.Chat(context.Background(),
		[]Message{{Role: "user", Content: "crée un groupe G"}},
		Tools()[:2],
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply.Content == "" || len(reply.ToolCalls) != 1 {
		t.Fatalf("unexpected reply: %+v", reply)
	}
	tc := reply.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "create_group" || !strings.Contains(tc.Function.Arguments, `"G"`) {
		t.Fatalf("bad tool call: %+v", tc)
	}
	if !strings.Contains(gotBody, `"tool_choice":"auto"`) || !strings.Contains(gotBody, `"tools":[`) {
		t.Fatalf("tools not sent in request: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"role":"user"`) {
		t.Fatalf("messages not sent: %s", gotBody)
	}
}

func TestChatProviderChatNoToolCalls(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices": [{"message": {"role": "assistant", "content": "Bonjour !"}}]}`)
	}))
	defer ts.Close()
	p := NewChatProvider(config.AIConfig{BaseURL: ts.URL, Model: "m", TimeoutSeconds: 5})
	reply, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply.Content != "Bonjour !" || len(reply.ToolCalls) != 0 {
		t.Fatalf("unexpected reply: %+v", reply)
	}
}
