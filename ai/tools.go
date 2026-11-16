package ai

func objectSchema(properties map[string]interface{}, required []string) map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

func stringProp(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc}
}

func boolProp(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "boolean", "description": desc}
}

// Tools returns the function definitions the chat agent may call.
func Tools() []Tool {
	fn := func(name, desc string, params map[string]interface{}) Tool {
		return Tool{Type: "function", Function: ToolFunction{Name: name, Description: desc, Parameters: params}}
	}
	return []Tool{
		fn("get_server_info", "Get gophish2 server info: version, AI status, current user, and counts of templates, landing pages, groups, sending profiles and campaigns.", objectSchema(map[string]interface{}{}, nil)),
		fn("list_templates", "List the user's email templates (id, name, subject).", objectSchema(map[string]interface{}{}, nil)),
		fn("list_pages", "List the user's landing pages (id, name, capture settings, redirect URL).", objectSchema(map[string]interface{}{}, nil)),
		fn("list_groups", "List the user's target groups (id, name, number of targets).", objectSchema(map[string]interface{}{}, nil)),
		fn("list_sending_profiles", "List the user's SMTP sending profiles (id, name, host, from address).", objectSchema(map[string]interface{}{}, nil)),
		fn("list_campaigns", "List the user's campaigns (id, name, status, launch date).", objectSchema(map[string]interface{}{}, nil)),
		fn("get_campaign_summary", "Get the summary and statistics of a campaign by id.", objectSchema(map[string]interface{}{
			"campaign_id": map[string]interface{}{"type": "integer", "description": "campaign id"},
		}, []string{"campaign_id"})),
		fn("list_webhooks", "List configured webhooks (requires the ModifySystem permission).", objectSchema(map[string]interface{}{}, nil)),
		fn("generate_content", "Generate (dry run, nothing is created) an email template and landing page for a scenario, validated server-side. Returns the content and suggested names.", objectSchema(map[string]interface{}{
			"scenario":          stringProp("e.g. invoice_fraud, password_reset, package_delivery, hr_policy, it_alert, voicemail, or free text"),
			"industry":          stringProp("target industry, e.g. finance, healthcare"),
			"language":          stringProp("language code, e.g. fr, en"),
			"tone":              stringProp("e.g. urgent, neutral, friendly"),
			"target_audience":   stringProp("e.g. all employees, finance department"),
			"sender_name":       stringProp("display name of the sender, e.g. IT Support"),
			"company_name":      stringProp("organization to impersonate"),
			"capture_passwords": boolProp("capture submitted passwords on the landing page"),
			"redirect_url":      stringProp("URL users are redirected to after submitting the form (e.g. a training page)"),
		}, []string{"scenario"})),
		fn("create_template", "Create an email template. Requires an authorization reference (argument or set_authorization).", objectSchema(map[string]interface{}{
			"name":          stringProp("template name"),
			"subject":       stringProp("email subject, may use {{.FirstName}} etc."),
			"text":          stringProp("plaintext body, must contain {{.URL}}"),
			"html":          stringProp("HTML body, must contain <a href=\"{{.URL}}\">"),
			"authorization": stringProp("authorization reference (ticket/change id)"),
		}, []string{"name", "subject", "authorization"})),
		fn("create_page", "Create a landing page. Requires an authorization reference.", objectSchema(map[string]interface{}{
			"name":                stringProp("page name"),
			"html":                stringProp("full HTML page containing a <form> with input name=\"username\""),
			"capture_credentials": boolProp("capture submitted credentials"),
			"capture_passwords":   boolProp("capture submitted passwords"),
			"redirect_url":        stringProp("URL to redirect to after submission"),
			"authorization":       stringProp("authorization reference"),
		}, []string{"name", "html", "authorization"})),
		fn("create_group", "Create a target group with recipients.", objectSchema(map[string]interface{}{
			"name": stringProp("group name"),
			"targets": map[string]interface{}{
				"type": "array",
				"items": objectSchema(map[string]interface{}{
					"email":      stringProp("recipient email"),
					"first_name": stringProp("recipient first name"),
					"last_name":  stringProp("recipient last name"),
					"position":   stringProp("recipient job title"),
				}, []string{"email"}),
			},
		}, []string{"name", "targets"})),
		fn("create_sending_profile", "Create an SMTP sending profile. host must be in host:port format (port defaults to 25).", objectSchema(map[string]interface{}{
			"name":               stringProp("profile name"),
			"host":               stringProp("SMTP host, e.g. smtp.example.com:587"),
			"username":           stringProp("SMTP username (optional)"),
			"password":           stringProp("SMTP password (optional)"),
			"from_address":       stringProp("From address, e.g. IT Support <it@example.com>"),
			"ignore_cert_errors": boolProp("ignore TLS certificate errors"),
		}, []string{"name", "host", "from_address"})),
		fn("create_campaign", "Create (and launch or schedule) a campaign from an existing template, page, sending profile and groups. Requires an authorization reference and explicit user confirmation.", objectSchema(map[string]interface{}{
			"name":          stringProp("campaign name"),
			"template_name": stringProp("existing email template name"),
			"page_name":     stringProp("existing landing page name"),
			"smtp_name":     stringProp("existing sending profile name"),
			"groups":        map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "existing group names"},
			"url":           stringProp("phishing URL / domain, e.g. https://phish.example.com"),
			"launch_date":   stringProp("optional RFC3339 launch date; omit to launch immediately"),
			"send_by_date":  stringProp("optional RFC3339 send-by date"),
			"authorization": stringProp("authorization reference (ticket/change id)"),
		}, []string{"name", "template_name", "page_name", "smtp_name", "groups", "url", "authorization"})),
		fn("send_test_email", "Send a test email with an existing template through an existing sending profile, to a single address.", objectSchema(map[string]interface{}{
			"template_name": stringProp("existing template name"),
			"smtp_name":     stringProp("existing sending profile name"),
			"to":            stringProp("recipient address for the test"),
			"url":           stringProp("optional phishing URL embedded in the test"),
		}, []string{"template_name", "smtp_name", "to"})),
		fn("set_authorization", "Store the authorization reference (ticket/change id) for this conversation. Required before create_template, create_page or create_campaign.", objectSchema(map[string]interface{}{
			"reference": stringProp("authorization reference, e.g. SEC-1234"),
		}, []string{"reference"})),
	}
}

// ChatSystemPrompt is the system prompt of the conversational agent.
func ChatSystemPrompt() string {
	allowed := make([]string, len(AllowedVariables))
	for i, v := range AllowedVariables {
		allowed[i] = "{{." + v + "}}"
	}
	return `You are the gophish2 assistant, helping an authorized security team run phishing simulation campaigns for employee security awareness training (AUTHORIZED simulations only).
You can inspect the platform and create or configure resources (email templates, landing pages, target groups, SMTP sending profiles, campaigns, test emails) using the provided tools.

Rules:
- Reply in the same language as the user's message, concisely.
- When information is missing, ask for it: phishing URL/domain, SMTP host and credentials, sender address, target group and recipients, launch timing, and the authorization reference (ticket/change id).
- To draft phishing content, call generate_content (dry run). Summarize the result, then create the template and page with create_template / create_page only after the user confirms, and only with a valid authorization reference. Ask once and store it with set_authorization.
- Never create or launch a campaign without explicit user confirmation and an authorization reference. Prefer send_test_email to validate the setup first, and prefer a scheduled launch_date.
- Template variables you may use in content: ` + joinVars(allowed) + `. The email must contain the {{.URL}} tracking link and the landing page must contain a <form>.
- After each action, briefly summarize what was created/changed (ids, names).
- Never reveal secrets (API keys, SMTP passwords). Never log or repeat passwords back.`
}

func joinVars(v []string) string {
	out := ""
	for i, s := range v {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
