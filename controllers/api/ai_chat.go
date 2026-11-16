package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gophish/gophish/ai"
	"github.com/gophish/gophish/config"
	ctx "github.com/gophish/gophish/context"
	log "github.com/gophish/gophish/logger"
	"github.com/gophish/gophish/models"
	"github.com/sirupsen/logrus"
)

// maxChatIterations bounds the tool-calling loop of a single chat turn.
const maxChatIterations = 8

// AIChatRequest is the payload of POST /api/ai/chat.
type AIChatRequest struct {
	ConversationID string `json:"conversation_id"`
	Message        string `json:"message"`
	Authorization  string `json:"authorization"`
}

// AIChatResponse is the data returned by POST /api/ai/chat.
type AIChatResponse struct {
	ConversationID string       `json:"conversation_id"`
	Messages       []ai.Message `json:"messages"`
}

// AIChat handles POST /api/ai/chat: a multi-turn conversational agent with
// tool calling. The model can inspect the platform and configure it
// (templates, pages, groups, sending profiles, campaigns, test emails).
// Writes that create phishing content or campaigns require an authorization
// reference, either in the request or stored in the conversation.
func (as *Server) AIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		convID := r.URL.Query().Get("conversation_id")
		if convID == "" {
			JSONResponse(w, models.Response{Success: false, Message: "conversation_id is required"}, http.StatusBadRequest)
			return
		}
		conv := as.aiConversations.Get(convID)
		if conv == nil {
			JSONResponse(w, models.Response{Success: false, Message: "Unknown or expired conversation_id"}, http.StatusNotFound)
			return
		}
		JSONResponse(w, models.Response{
			Success: true,
			Data:    AIChatResponse{ConversationID: conv.ID, Messages: conv.Messages},
		}, http.StatusOK)
		return
	}
	if r.Method != "POST" {
		JSONResponse(w, models.Response{Success: false, Message: "Method not allowed"}, http.StatusBadRequest)
		return
	}
	if as.aiProvider == nil {
		JSONResponse(w, models.Response{Success: false, Message: "AI provider is not configured or enabled"}, http.StatusNotImplemented)
		return
	}
	req := &AIChatRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Invalid JSON structure"}, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		JSONResponse(w, models.Response{Success: false, Message: "Message is required"}, http.StatusBadRequest)
		return
	}
	conv := as.aiConversations.Get(req.ConversationID)
	if conv == nil {
		JSONResponse(w, models.Response{Success: false, Message: "Unknown or expired conversation_id"}, http.StatusNotFound)
		return
	}
	if req.Authorization != "" {
		conv.SetAuthorization(req.Authorization)
	}
	conv.Append(ai.Message{Role: "user", Content: req.Message})

	userID, _ := ctx.Get(r, "user_id").(int64)
	toolsCalled := []string{}
	finalReply := ""

	for i := 0; i < maxChatIterations; i++ {
		messages := append([]ai.Message{{Role: "system", Content: ai.ChatSystemPrompt()}}, conv.Messages...)
		reply, err := as.aiProvider.Chat(r.Context(), messages, ai.Tools())
		if err != nil {
			log.WithFields(logrus.Fields{"error": err.Error()}).Error("AI chat failed")
			JSONResponse(w, models.Response{Success: false, Message: fmt.Sprintf("AI provider error: %v", err)}, http.StatusBadGateway)
			return
		}
		finalReply = reply.Content
		if len(reply.ToolCalls) == 0 {
			conv.Append(ai.Message{Role: "assistant", Content: reply.Content})
			break
		}
		conv.Append(ai.Message{Role: "assistant", Content: reply.Content, ToolCalls: reply.ToolCalls})
		for _, tc := range reply.ToolCalls {
			toolsCalled = append(toolsCalled, tc.Function.Name)
			result := as.executeTool(r, conv, tc)
			conv.Append(result)
		}
	}
	if finalReply == "" {
		finalReply = "..."
		conv.Append(ai.Message{Role: "assistant", Content: finalReply})
	}

	log.WithFields(logrus.Fields{
		"endpoint":        "ai/chat",
		"user_id":         userID,
		"model":           as.aiProvider.Model(),
		"conversation_id": conv.ID,
		"tools":           strings.Join(toolsCalled, ","),
		"authorization":   conv.Authorization,
	}).Info("AI chat turn")

	JSONResponse(w, models.Response{
		Success: true,
		Message: finalReply,
		Data:    AIChatResponse{ConversationID: conv.ID, Messages: conv.Messages},
	}, http.StatusOK)
}

// executeTool runs one tool call and returns the result as a tool message.
func (as *Server) executeTool(r *http.Request, conv *ai.Conversation, tc ai.ToolCall) ai.Message {
	uid, _ := ctx.Get(r, "user_id").(int64)
	args := map[string]interface{}{}
	if tc.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return toolResult(tc, nil, fmt.Errorf("invalid tool arguments: %v", err))
		}
	}
	result, err := as.runTool(r, uid, conv, tc.Function.Name, args)
	return toolResult(tc, result, err)
}

func toolResult(tc ai.ToolCall, result interface{}, err error) ai.Message {
	content := ""
	if err != nil {
		content = "error: " + err.Error()
	} else {
		raw, merr := json.Marshal(result)
		if merr != nil {
			content = "error: could not encode result"
		} else {
			content = string(raw)
		}
	}
	return ai.Message{Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: content}
}

func argString(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func argBool(args map[string]interface{}, key string) bool {
	v, _ := args[key].(bool)
	return v
}

func argStringSlice(args map[string]interface{}, key string) []string {
	out := []string{}
	if v, ok := args[key].([]interface{}); ok {
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

// resolveAuthorization returns the authorization reference from the tool
// arguments or from the conversation.
func resolveAuthorization(conv *ai.Conversation, args map[string]interface{}) string {
	if ref := argString(args, "authorization"); ref != "" {
		conv.SetAuthorization(ref)
		return ref
	}
	return conv.Authorization
}

func auditTool(r *http.Request, conv *ai.Conversation, tool, authorization string, fields logrus.Fields) {
	userID, _ := ctx.Get(r, "user_id").(int64)
	if fields == nil {
		fields = logrus.Fields{}
	}
	fields["endpoint"] = "ai/chat"
	fields["tool"] = tool
	fields["user_id"] = userID
	fields["conversation_id"] = conv.ID
	fields["authorization"] = authorization
	log.WithFields(fields).Info("AI tool execution")
}

// runTool executes a tool by name. Write operations that create phishing
// content or campaigns require a non-empty authorization reference.
func (as *Server) runTool(r *http.Request, uid int64, conv *ai.Conversation, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "get_server_info":
		ts, _ := models.GetTemplates(uid)
		ps, _ := models.GetPages(uid)
		gs, _ := models.GetGroups(uid)
		ss, _ := models.GetSMTPs(uid)
		cs, _ := models.GetCampaigns(uid)
		user := ctx.Get(r, "user")
		username := ""
		if u, ok := user.(models.User); ok {
			username = u.Username
		}
		model := ""
		if as.aiProvider != nil {
			model = as.aiProvider.Model()
		}
		return map[string]interface{}{
			"version": config.Version, "ai_enabled": as.aiProvider != nil,
			"model": model, "username": username,
			"counts": map[string]int{
				"templates": len(ts), "pages": len(ps), "groups": len(gs),
				"sending_profiles": len(ss), "campaigns": len(cs),
			},
		}, nil
	case "list_templates":
		ts, err := models.GetTemplates(uid)
		if err != nil {
			return nil, err
		}
		out := []map[string]interface{}{}
		for _, t := range ts {
			out = append(out, map[string]interface{}{"id": t.Id, "name": t.Name, "subject": t.Subject})
		}
		return out, nil
	case "list_pages":
		ps, err := models.GetPages(uid)
		if err != nil {
			return nil, err
		}
		out := []map[string]interface{}{}
		for _, p := range ps {
			out = append(out, map[string]interface{}{
				"id": p.Id, "name": p.Name, "capture_credentials": p.CaptureCredentials,
				"capture_passwords": p.CapturePasswords, "redirect_url": p.RedirectURL,
			})
		}
		return out, nil
	case "list_groups":
		gs, err := models.GetGroups(uid)
		if err != nil {
			return nil, err
		}
		out := []map[string]interface{}{}
		for _, g := range gs {
			out = append(out, map[string]interface{}{"id": g.Id, "name": g.Name, "targets": len(g.Targets)})
		}
		return out, nil
	case "list_sending_profiles":
		ss, err := models.GetSMTPs(uid)
		if err != nil {
			return nil, err
		}
		out := []map[string]interface{}{}
		for _, s := range ss {
			out = append(out, map[string]interface{}{"id": s.Id, "name": s.Name, "host": s.Host, "from_address": s.FromAddress})
		}
		return out, nil
	case "list_campaigns":
		cs, err := models.GetCampaigns(uid)
		if err != nil {
			return nil, err
		}
		out := []map[string]interface{}{}
		for _, c := range cs {
			out = append(out, map[string]interface{}{
				"id": c.Id, "name": c.Name, "status": c.Status, "launch_date": c.LaunchDate,
			})
		}
		return out, nil
	case "get_campaign_summary":
		id, _ := args["campaign_id"].(float64)
		if id == 0 {
			return nil, fmt.Errorf("campaign_id is required")
		}
		summary, err := models.GetCampaignSummary(int64(id), uid)
		if err != nil {
			return nil, err
		}
		return summary, nil
	case "list_webhooks":
		u := ctx.Get(r, "user").(models.User)
		if ok, _ := u.HasPermission(models.PermissionModifySystem); !ok {
			return nil, fmt.Errorf("permission denied: ModifySystem required")
		}
		whs, err := models.GetWebhooks()
		if err != nil {
			return nil, err
		}
		out := []map[string]interface{}{}
		for _, wh := range whs {
			out = append(out, map[string]interface{}{"id": wh.Id, "url": wh.URL, "is_active": wh.IsActive})
		}
		return out, nil
	case "generate_content":
		genReq := &AIRequest{
			Scenario:         argString(args, "scenario"),
			Industry:         argString(args, "industry"),
			Language:         argString(args, "language"),
			Tone:             argString(args, "tone"),
			TargetAudience:   argString(args, "target_audience"),
			SenderName:       argString(args, "sender_name"),
			CompanyName:      argString(args, "company_name"),
			CapturePasswords: argBool(args, "capture_passwords"),
			RedirectURL:      argString(args, "redirect_url"),
		}
		content, err := as.generateContent(r, genReq)
		if err != nil {
			return nil, err
		}
		templateName, pageName := defaultNames(genReq)
		auditTool(r, conv, name, "", logrus.Fields{"scenario": genReq.Scenario})
		return map[string]interface{}{
			"content": content, "template_name": templateName, "page_name": pageName,
			"note": "dry run: nothing was created; call create_template and create_page with an authorization reference to save",
		}, nil
	case "create_template":
		auth := resolveAuthorization(conv, args)
		if auth == "" {
			return nil, fmt.Errorf("authorization reference required: pass \"authorization\" or call set_authorization first")
		}
		t := models.Template{
			Name:         argString(args, "name"),
			Subject:      argString(args, "subject"),
			Text:         argString(args, "text"),
			HTML:         argString(args, "html"),
			UserId:       uid,
			ModifiedDate: time.Now().UTC(),
		}
		if err := t.Validate(); err != nil {
			return nil, err
		}
		if err := models.PostTemplate(&t); err != nil {
			return nil, err
		}
		auditTool(r, conv, name, auth, logrus.Fields{"template_id": t.Id, "name": t.Name})
		return map[string]interface{}{"id": t.Id, "name": t.Name}, nil
	case "create_page":
		auth := resolveAuthorization(conv, args)
		if auth == "" {
			return nil, fmt.Errorf("authorization reference required: pass \"authorization\" or call set_authorization first")
		}
		p := models.Page{
			Name:               argString(args, "name"),
			HTML:               argString(args, "html"),
			CaptureCredentials: argBool(args, "capture_credentials"),
			CapturePasswords:   argBool(args, "capture_passwords"),
			RedirectURL:        argString(args, "redirect_url"),
			UserId:             uid,
			ModifiedDate:       time.Now().UTC(),
		}
		if err := p.Validate(); err != nil {
			return nil, err
		}
		if err := models.PostPage(&p); err != nil {
			return nil, err
		}
		auditTool(r, conv, name, auth, logrus.Fields{"page_id": p.Id, "name": p.Name})
		return map[string]interface{}{"id": p.Id, "name": p.Name}, nil
	case "create_group":
		g := models.Group{
			Name:         argString(args, "name"),
			UserId:       uid,
			ModifiedDate: time.Now().UTC(),
		}
		if raw, ok := args["targets"].([]interface{}); ok {
			for _, item := range raw {
				m, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				t := models.Target{BaseRecipient: models.BaseRecipient{
					Email:     argString(m, "email"),
					FirstName: argString(m, "first_name"),
					LastName:  argString(m, "last_name"),
					Position:  argString(m, "position"),
				}}
				if t.Email != "" {
					g.Targets = append(g.Targets, t)
				}
			}
		}
		if err := models.PostGroup(&g); err != nil {
			return nil, err
		}
		auditTool(r, conv, name, "", logrus.Fields{"group_id": g.Id, "name": g.Name, "targets": len(g.Targets)})
		return map[string]interface{}{"id": g.Id, "name": g.Name, "targets": len(g.Targets)}, nil
	case "create_sending_profile":
		s := models.SMTP{
			Name:             argString(args, "name"),
			Host:             argString(args, "host"),
			Username:         argString(args, "username"),
			Password:         argString(args, "password"),
			FromAddress:      argString(args, "from_address"),
			IgnoreCertErrors: argBool(args, "ignore_cert_errors"),
			UserId:           uid,
			ModifiedDate:     time.Now().UTC(),
		}
		if err := s.Validate(); err != nil {
			return nil, err
		}
		if err := models.PostSMTP(&s); err != nil {
			return nil, err
		}
		auditTool(r, conv, name, "", logrus.Fields{"smtp_id": s.Id, "name": s.Name, "host": s.Host})
		return map[string]interface{}{"id": s.Id, "name": s.Name, "host": s.Host, "from_address": s.FromAddress}, nil
	case "create_campaign":
		auth := resolveAuthorization(conv, args)
		if auth == "" {
			return nil, fmt.Errorf("authorization reference required: pass \"authorization\" or call set_authorization first")
		}
		c := models.Campaign{
			Name:     argString(args, "name"),
			Template: models.Template{Name: argString(args, "template_name")},
			Page:     models.Page{Name: argString(args, "page_name")},
			SMTP:     models.SMTP{Name: argString(args, "smtp_name")},
			URL:      argString(args, "url"),
		}
		for _, g := range argStringSlice(args, "groups") {
			c.Groups = append(c.Groups, models.Group{Name: g})
		}
		if ld := argString(args, "launch_date"); ld != "" {
			t, err := time.Parse(time.RFC3339, ld)
			if err != nil {
				return nil, fmt.Errorf("invalid launch_date (use RFC3339): %v", err)
			}
			c.LaunchDate = t
		}
		if sd := argString(args, "send_by_date"); sd != "" {
			t, err := time.Parse(time.RFC3339, sd)
			if err != nil {
				return nil, fmt.Errorf("invalid send_by_date (use RFC3339): %v", err)
			}
			c.SendByDate = t
		}
		if err := models.PostCampaign(&c, uid); err != nil {
			return nil, err
		}
		if c.Status == models.CampaignInProgress {
			go as.worker.LaunchCampaign(c)
		}
		auditTool(r, conv, name, auth, logrus.Fields{"campaign_id": c.Id, "name": c.Name, "status": c.Status, "url": c.URL})
		return map[string]interface{}{"id": c.Id, "name": c.Name, "status": c.Status}, nil
	case "send_test_email":
		s := &models.EmailRequest{
			ErrorChan: make(chan error, 1),
			UserId:    uid,
			URL:       argString(args, "url"),
			BaseRecipient: models.BaseRecipient{
				Email: argString(args, "to"),
			},
		}
		var err error
		s.Template, err = models.GetTemplateByName(argString(args, "template_name"), uid)
		if err != nil {
			return nil, fmt.Errorf("template not found: %v", err)
		}
		s.TemplateId = s.Template.Id
		s.SMTP, err = models.GetSMTPByName(argString(args, "smtp_name"), uid)
		if err != nil {
			return nil, fmt.Errorf("sending profile not found: %v", err)
		}
		s.FromAddress = s.SMTP.FromAddress
		if s.Template.EnvelopeSender != "" {
			s.FromAddress = s.Template.EnvelopeSender
		}
		if err := s.Validate(); err != nil {
			return nil, err
		}
		if err := as.worker.SendTestEmail(s); err != nil {
			return nil, err
		}
		auditTool(r, conv, name, "", logrus.Fields{"template": s.Template.Name, "smtp": s.SMTP.Name, "to": s.Email})
		return map[string]interface{}{"sent": true, "to": s.Email}, nil
	case "set_authorization":
		ref := argString(args, "reference")
		if ref == "" {
			return nil, fmt.Errorf("reference is required")
		}
		conv.SetAuthorization(ref)
		return map[string]interface{}{"stored": true}, nil
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}
