package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gophish/gophish/ai"
	ctx "github.com/gophish/gophish/context"
	log "github.com/gophish/gophish/logger"
	"github.com/gophish/gophish/models"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
)

// AIRequest is the payload for the /api/ai/generate and /api/ai/campaign
// endpoints.
type AIRequest struct {
	Scenario         string `json:"scenario"`
	Industry         string `json:"industry"`
	Language         string `json:"language"`
	Tone             string `json:"tone"`
	TargetAudience   string `json:"target_audience"`
	SenderName       string `json:"sender_name"`
	CompanyName      string `json:"company_name"`
	CapturePasswords bool   `json:"capture_passwords"`
	RedirectURL      string `json:"redirect_url"`
	// Content allows applying a previously reviewed generation (as returned
	// by /api/ai/generate) instead of calling the model again.
	Content       *ai.GeneratedContent `json:"content"`
	Apply         bool                 `json:"apply"`
	Authorization string               `json:"authorization"`
	TemplateName  string               `json:"template_name"`
	PageName      string               `json:"page_name"`
	// Campaign fields, only used by /api/ai/campaign when apply=true and
	// campaign_name is set.
	CampaignName string    `json:"campaign_name"`
	URL          string    `json:"url"`
	SMTPName     string    `json:"smtp_name"`
	Groups       []string  `json:"groups"`
	LaunchDate   time.Time `json:"launch_date"`
	SendByDate   time.Time `json:"send_by_date"`
}

// AIResult is the data returned by the AI endpoints.
type AIResult struct {
	Content      ai.GeneratedContent `json:"content"`
	TemplateName string              `json:"template_name"`
	PageName     string              `json:"page_name"`
	Applied      bool                `json:"applied"`
	TemplateID   int64               `json:"template_id,omitempty"`
	PageID       int64               `json:"page_id,omitempty"`
	CampaignID   int64               `json:"campaign_id,omitempty"`
}

// AIConfig returns the public AI configuration. The API key is never exposed.
func (as *Server) AIConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		JSONResponse(w, models.Response{Success: false, Message: "Method not allowed"}, http.StatusBadRequest)
		return
	}
	resp := struct {
		Enabled bool   `json:"enabled"`
		Model   string `json:"model,omitempty"`
	}{Enabled: as.aiProvider != nil}
	if as.aiProvider != nil {
		resp.Model = as.aiProvider.Model()
	}
	JSONResponse(w, resp, http.StatusOK)
}

func (as *Server) requireAIProvider(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "POST" {
		JSONResponse(w, models.Response{Success: false, Message: "Method not allowed"}, http.StatusBadRequest)
		return false
	}
	if as.aiProvider == nil {
		JSONResponse(w, models.Response{Success: false, Message: "AI provider is not configured or enabled"}, http.StatusNotImplemented)
		return false
	}
	return true
}

func (as *Server) decodeAIRequest(w http.ResponseWriter, r *http.Request) (*AIRequest, bool) {
	req := &AIRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		JSONResponse(w, models.Response{Success: false, Message: "Invalid JSON structure"}, http.StatusBadRequest)
		return nil, false
	}
	return req, true
}

// generateContent produces and validates content, either by calling the
// provider or by re-validating content supplied by the caller.
func (as *Server) generateContent(r *http.Request, req *AIRequest) (*ai.GeneratedContent, error) {
	var content *ai.GeneratedContent
	if req.Content != nil {
		content = req.Content
	} else {
		brief := &ai.Brief{
			Scenario:         req.Scenario,
			Industry:         req.Industry,
			Language:         req.Language,
			Tone:             req.Tone,
			TargetAudience:   req.TargetAudience,
			SenderName:       req.SenderName,
			CompanyName:      req.CompanyName,
			CapturePasswords: req.CapturePasswords,
			RedirectURL:      req.RedirectURL,
		}
		userPrompt, err := ai.UserPrompt(brief)
		if err != nil {
			return nil, err
		}
		raw, err := as.aiProvider.Generate(r.Context(), ai.SystemPrompt(), userPrompt)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ai.ErrProvider, err)
		}
		content, err = ai.ParseGenerated(raw)
		if err != nil {
			return nil, err
		}
	}
	if err := ai.ValidateContent(content); err != nil {
		return nil, err
	}
	// Defense in depth: run the generated content through Gophish's own
	// template engine validator as well.
	for _, field := range []string{content.EmailSubject, content.EmailText, content.EmailHTML, content.LandingHTML, content.LandingRedirectURL} {
		if err := models.ValidateTemplate(field); err != nil {
			return nil, err
		}
	}
	return content, nil
}

func defaultNames(req *AIRequest) (string, string) {
	scenario := req.Scenario
	if scenario == "" {
		scenario = "campaign"
	}
	suffix := time.Now().UTC().Format("20060102-150405")
	templateName := req.TemplateName
	if templateName == "" {
		templateName = "AI - " + scenario + " - " + suffix
	}
	pageName := req.PageName
	if pageName == "" {
		pageName = "AI - " + scenario + " page - " + suffix
	}
	return templateName, pageName
}

func auditAI(r *http.Request, endpoint, model string, req *AIRequest, applied bool) {
	userID, _ := ctx.Get(r, "user_id").(int64)
	sum := sha256.Sum256([]byte(strings.Join([]string{
		req.Scenario, req.Industry, req.Language, req.Tone, req.TargetAudience,
		req.SenderName, req.CompanyName, req.RedirectURL,
	}, "|")))
	log.WithFields(logrus.Fields{
		"endpoint":      endpoint,
		"user_id":       userID,
		"model":         model,
		"scenario":      req.Scenario,
		"brief_sha256":  hex.EncodeToString(sum[:]),
		"applied":       applied,
		"authorization": req.Authorization,
	}).Info("AI content generation")
}

// AIGenerate handles POST /api/ai/generate. It always runs as a dry run: it
// returns the generated content without creating anything.
func (as *Server) AIGenerate(w http.ResponseWriter, r *http.Request) {
	if !as.requireAIProvider(w, r) {
		return
	}
	req, ok := as.decodeAIRequest(w, r)
	if !ok {
		return
	}
	content, err := as.generateContent(r, req)
	if err != nil {
		log.WithFields(logrus.Fields{"error": err.Error()}).Error("AI generation failed")
		status := http.StatusUnprocessableEntity
		if errors.Is(err, ai.ErrProvider) {
			status = http.StatusBadGateway
		}
		JSONResponse(w, models.Response{Success: false, Message: err.Error()}, status)
		return
	}
	auditAI(r, "ai/generate", as.aiProvider.Model(), req, false)
	templateName, pageName := defaultNames(req)
	JSONResponse(w, models.Response{
		Success: true,
		Message: "Dry run: nothing was created. Review the content, then call /api/ai/campaign with apply=true and an authorization reference to create it.",
		Data:    AIResult{Content: *content, TemplateName: templateName, PageName: pageName, Applied: false},
	}, http.StatusOK)
}

// AICampaign handles POST /api/ai/campaign. Without apply=true it behaves
// like a dry run. With apply=true it creates the email template and landing
// page (and optionally a full campaign), which requires a non-empty
// authorization reference that is written to the audit log.
func (as *Server) AICampaign(w http.ResponseWriter, r *http.Request) {
	if !as.requireAIProvider(w, r) {
		return
	}
	req, ok := as.decodeAIRequest(w, r)
	if !ok {
		return
	}
	content, err := as.generateContent(r, req)
	if err != nil {
		log.WithFields(logrus.Fields{"error": err.Error()}).Error("AI generation failed")
		status := http.StatusUnprocessableEntity
		if errors.Is(err, ai.ErrProvider) {
			status = http.StatusBadGateway
		}
		JSONResponse(w, models.Response{Success: false, Message: err.Error()}, status)
		return
	}
	templateName, pageName := defaultNames(req)
	result := AIResult{Content: *content, TemplateName: templateName, PageName: pageName, Applied: req.Apply}
	if !req.Apply {
		auditAI(r, "ai/campaign", as.aiProvider.Model(), req, false)
		JSONResponse(w, models.Response{
			Success: true,
			Message: "Dry run: nothing was created. Repost with apply=true and an authorization reference to create the template, page and campaign.",
			Data:    result,
		}, http.StatusOK)
		return
	}
	if strings.TrimSpace(req.Authorization) == "" {
		JSONResponse(w, models.Response{Success: false, Message: "An authorization reference is required to create AI-generated content (apply=true)"}, http.StatusBadRequest)
		return
	}
	uid := ctx.Get(r, "user_id").(int64)

	t := models.Template{
		Name:         templateName,
		Subject:      content.EmailSubject,
		Text:         content.EmailText,
		HTML:         content.EmailHTML,
		UserId:       uid,
		ModifiedDate: time.Now().UTC(),
	}
	if _, err := models.GetTemplateByName(t.Name, uid); err == nil {
		JSONResponse(w, models.Response{Success: false, Message: "Template name already in use"}, http.StatusConflict)
		return
	} else if err != gorm.ErrRecordNotFound {
		log.Error(err)
		JSONResponse(w, models.Response{Success: false, Message: "Error checking template name"}, http.StatusInternalServerError)
		return
	}
	if err := models.PostTemplate(&t); err != nil {
		log.Error(err)
		JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
		return
	}
	result.TemplateID = t.Id

	redirectURL := content.LandingRedirectURL
	if redirectURL == "" {
		redirectURL = req.RedirectURL
	}
	p := models.Page{
		Name:               pageName,
		HTML:               content.LandingHTML,
		CaptureCredentials: true,
		CapturePasswords:   content.CapturePasswords || req.CapturePasswords,
		RedirectURL:        redirectURL,
		UserId:             uid,
		ModifiedDate:       time.Now().UTC(),
	}
	if _, err := models.GetPageByName(p.Name, uid); err == nil {
		JSONResponse(w, models.Response{Success: false, Message: "Page name already in use"}, http.StatusConflict)
		return
	} else if err != gorm.ErrRecordNotFound {
		log.Error(err)
		JSONResponse(w, models.Response{Success: false, Message: "Error checking page name"}, http.StatusInternalServerError)
		return
	}
	if err := models.PostPage(&p); err != nil {
		log.Error(err)
		JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
		return
	}
	result.PageID = p.Id

	if req.CampaignName != "" {
		if req.URL == "" || req.SMTPName == "" || len(req.Groups) == 0 {
			JSONResponse(w, models.Response{Success: false, Message: "url, smtp_name and at least one group are required to create a campaign"}, http.StatusBadRequest)
			return
		}
		c := models.Campaign{
			Name:       req.CampaignName,
			Template:   models.Template{Name: t.Name},
			Page:       models.Page{Name: p.Name},
			SMTP:       models.SMTP{Name: req.SMTPName},
			URL:        req.URL,
			LaunchDate: req.LaunchDate,
			SendByDate: req.SendByDate,
		}
		for _, g := range req.Groups {
			c.Groups = append(c.Groups, models.Group{Name: g})
		}
		if err := models.PostCampaign(&c, uid); err != nil {
			log.Error(err)
			JSONResponse(w, models.Response{Success: false, Message: err.Error()}, http.StatusBadRequest)
			return
		}
		if c.Status == models.CampaignInProgress {
			go as.worker.LaunchCampaign(c)
		}
		result.CampaignID = c.Id
	}

	auditAI(r, "ai/campaign", as.aiProvider.Model(), req, true)
	JSONResponse(w, models.Response{Success: true, Message: "AI-generated content created", Data: result}, http.StatusCreated)
}
