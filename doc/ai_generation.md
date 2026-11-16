# AI-Assisted Campaign Generation

Gophish2 can generate email templates and landing pages through any
OpenAI-compatible `/chat/completions` endpoint (OpenAI, Mistral, Anthropic
compatible gateways, Ollama, vLLM, LM Studio, ...). The feature is designed for
**authorized phishing simulation / security awareness campaigns only**.

## Configuration

Add an `ai` section to `config.json`:

```json
"ai": {
    "enabled": true,
    "base_url": "https://api.openai.com/v1",
    "model": "gpt-4o-mini",
    "max_tokens": 4096,
    "timeout_seconds": 60
}
```

The provider API key is **never** stored in `config.json`: set the
`GOPHISH_AI_API_KEY` environment variable instead. The key is excluded from
JSON serialization and is never logged.

For a local model, point `base_url` at e.g. `http://127.0.0.1:11434/v1`
(Ollama) and leave `GOPHISH_AI_API_KEY` unset.

## API endpoints

All endpoints live under `/api/` and use the usual API key authentication
(`?api_key=` or `Authorization: Bearer <gophish api key>`). They are
rate-limited like other POST endpoints.

### `GET /api/ai/config`

Returns `{"enabled": bool, "model": string}`. Never exposes the API key.

### `POST /api/ai/generate`

Always a **dry run**: returns the generated content without creating anything.

```json
{
  "scenario": "invoice_fraud",
  "industry": "finance",
  "language": "fr",
  "tone": "urgent",
  "target_audience": "all employees",
  "sender_name": "IT Support",
  "company_name": "Acme Corp",
  "capture_passwords": true,
  "redirect_url": "https://intranet.example.com/training"
}
```

Response `data` contains `content` (the generated `email_subject`,
`email_text`, `email_html`, `landing_html`, `landing_redirect_url`,
`capture_passwords`, `rationale`), plus suggested `template_name`/`page_name`.

### `POST /api/ai/campaign`

Same payload, plus:

- `apply` (bool): create the template and landing page. **Default is a dry
  run** (`apply: false`).
- `authorization` (string): **required when `apply: true`**. An empty value is
  rejected with HTTP 400. The reference is written to the audit log together
  with the user id, model, scenario and a hash of the brief.
- `content` (object): optional. When set, this previously reviewed generation
  (as returned by `/api/ai/generate`) is re-validated and applied as-is,
  without calling the model again. This enables a human review loop.
- `campaign_name`, `url`, `smtp_name`, `groups[]`, `launch_date`,
  `send_by_date`: when `campaign_name` is set (with `apply: true`), a full
  campaign is also created and launched/scheduled like
  `POST /api/campaigns/`.

## Guardrails

- Dry run by default; creation requires an explicit `apply: true` **and** a
  non-empty `authorization` reference.
- Generated content is validated server-side: whitelist of Gophish template
  variables (`{{.From}}`, `{{.URL}}`, `{{.Tracker}}`, `{{.TrackingURL}}`,
  `{{.RId}}`, `{{.BaseURL}}`, `{{.FirstName}}`, `{{.LastName}}`, `{{.Email}}`,
  `{{.Position}}`), the email must contain the `{{.URL}}` tracking link, the
  landing page must contain a `<form>`, no `<script>` in email HTML, size
  limits, and the content is additionally run through Gophish's own template
  validator before anything is saved.
- Every generation (dry run or applied) is audit-logged with user id, model,
  scenario, brief hash and authorization reference.
- View-only users cannot use the endpoints (`EnforceViewOnly` applies).

## Web UI

The **Email Templates** and **Landing Pages** pages have a "Generate with AI"
button that opens a brief form, shows a sandboxed preview of the generated
email and landing page, and creates both after an authorization reference is
provided.

## External agent example

`examples/ai_agent.py` (stdlib only) shows the full loop: check config,
generate a preview, apply with an authorization reference, create the campaign
and poll its results:

```bash
python3 examples/ai_agent.py --api-key "$GOPHISH_API_KEY" \
    --scenario invoice_fraud --language fr \
    --apply --authorization "SEC-1234" \
    --campaign-name "Q3 awareness" --url https://phish.example.com \
    --smtp "Corporate SMTP" --groups "All employees" --watch 300
```

Campaign events (opened/clicked/submitted) are also pushed to any configured
webhook, so an agent can close the feedback loop without polling.
