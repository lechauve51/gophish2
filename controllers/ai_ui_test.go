package controllers

import (
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/gophish/gophish/models"
)

func TestAIUIRendered(t *testing.T) {
	ctx := setupTest(t)
	defer tearDown(t, ctx)
	u, err := models.GetUser(1)
	if err != nil {
		t.Fatal(err)
	}
	u.PasswordChangeRequired = false
	if err := models.PutUser(&u); err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp := attemptLogin(t, ctx, client, "admin", "gophish", "")
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		t.Fatalf("login failed with %d", resp.StatusCode)
	}

	pages := map[string]bool{
		"/templates":        true,
		"/landing_pages":    true,
		"/campaigns":        false,
		"/sending_profiles": false,
	}
	for path, wantGenerateBtn := range pages {
		resp, err := client.Get(ctx.adminServer.URL + path)
		if err != nil {
			t.Fatalf("error requesting %s: %v", path, err)
		}
		body, _ := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		text := string(body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", path, resp.StatusCode)
		}
		if !strings.Contains(text, "aiChatOpen") {
			t.Fatalf("%s: AI Assistant nav item missing", path)
		}
		if !strings.Contains(text, "/js/ai-generate.js") {
			t.Fatalf("%s: ai-generate.js not included", path)
		}
		if strings.Contains(text, "aiGenerateBtn") != wantGenerateBtn {
			t.Fatalf("%s: Generate with AI button presence = %v, want %v", path, strings.Contains(text, "aiGenerateBtn"), wantGenerateBtn)
		}
	}
}
