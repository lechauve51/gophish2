#!/usr/bin/env python3
"""Example AI agent that drives gophish2's /api/ai endpoints.

Workflow: check the AI config, generate a dry-run preview, optionally apply it
(create the email template + landing page + campaign, which requires an
authorization reference), then optionally poll the campaign summary.

Stdlib only. Usage:

    python3 examples/ai_agent.py --api-key $GOPHISH_API_KEY \
        --scenario invoice_fraud --language fr --industry finance \
        --apply --authorization "SEC-1234" \
        --campaign-name "Q3 awareness" --url https://phish.example.com \
        --smtp "Corporate SMTP" --groups "All employees" --watch 300
"""

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request


def api_call(base_url, api_key, method, path, payload=None):
    url = base_url.rstrip("/") + path
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Authorization", "Bearer " + api_key)
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            return resp.status, json.loads(resp.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        body = e.read().decode()
        try:
            return e.code, json.loads(body)
        except ValueError:
            return e.code, {"success": False, "message": body}


def build_brief(args):
    return {
        "scenario": args.scenario,
        "industry": args.industry,
        "language": args.language,
        "tone": args.tone,
        "target_audience": args.target_audience,
        "sender_name": args.sender_name,
        "company_name": args.company_name,
        "capture_passwords": args.capture_passwords,
        "redirect_url": args.redirect_url,
    }


def chat_turn(base_url, api_key, conversation_id, message):
    payload = {"message": message}
    if conversation_id:
        payload["conversation_id"] = conversation_id
    status, resp = api_call(base_url, api_key, "POST", "/api/ai/chat", payload)
    if status != 200 or not resp.get("success"):
        return None, "chat failed (HTTP %d): %s" % (status, resp.get("message")), None
    data = resp.get("data", {})
    return data.get("conversation_id"), resp.get("message", ""), data.get("messages")


def interactive_chat(args):
    conv_id = ""
    print("AI assistant ready (Ctrl+D to quit). Tell it what to configure:")
    while True:
        try:
            line = input("> ").strip()
        except EOFError:
            print()
            return 0
        if not line:
            continue
        conv_id, reply, _ = chat_turn(args.base_url, args.api_key, conv_id, line)
        if conv_id is None:
            print("error:", reply)
            return 1
        print(reply)


def main(argv=None):
    parser = argparse.ArgumentParser(description="Drive gophish2 AI campaign generation")
    parser.add_argument("--base-url", default=os.environ.get("GOPHISH_URL", "http://127.0.0.1:3333"))
    parser.add_argument("--api-key", default=os.environ.get("GOPHISH_API_KEY", ""))
    parser.add_argument("--scenario", default="invoice_fraud")
    parser.add_argument("--industry", default="")
    parser.add_argument("--language", default="fr")
    parser.add_argument("--tone", default="urgent")
    parser.add_argument("--target-audience", default="all employees")
    parser.add_argument("--sender-name", default="")
    parser.add_argument("--company-name", default="")
    parser.add_argument("--capture-passwords", action="store_true", default=True)
    parser.add_argument("--no-capture-passwords", dest="capture_passwords", action="store_false")
    parser.add_argument("--redirect-url", default="")
    parser.add_argument("--apply", action="store_true", help="create the content (requires --authorization)")
    parser.add_argument("--authorization", default="", help="authorization reference, logged in the audit trail")
    parser.add_argument("--campaign-name", default="")
    parser.add_argument("--url", default="", help="phishing URL for the campaign")
    parser.add_argument("--smtp", default="", help="sending profile name")
    parser.add_argument("--groups", default="", help="comma-separated group names")
    parser.add_argument("--watch", type=int, default=0, help="seconds to poll the campaign summary after creation")
    parser.add_argument("--chat", action="store_true", help="interactive chat with the conversational agent (POST /api/ai/chat)")
    args = parser.parse_args(argv)

    if not args.api_key:
        print("error: --api-key or GOPHISH_API_KEY is required", file=sys.stderr)
        return 2

    if args.chat:
        status, cfg = api_call(args.base_url, args.api_key, "GET", "/api/ai/config")
        if status != 200 or not cfg.get("enabled"):
            print("error: AI generation is not enabled on this server", file=sys.stderr)
            return 1
        return interactive_chat(args)

    status, cfg = api_call(args.base_url, args.api_key, "GET", "/api/ai/config")
    if status != 200:
        print("error: cannot reach /api/ai/config (HTTP %d)" % status, file=sys.stderr)
        return 1
    if not cfg.get("enabled"):
        print("error: AI generation is not enabled on this server (set \"ai\": {\"enabled\": true} in config.json)", file=sys.stderr)
        return 1
    print("AI enabled, model: %s" % cfg.get("model", "?"))

    brief = build_brief(args)
    status, resp = api_call(args.base_url, args.api_key, "POST", "/api/ai/generate", brief)
    if status != 200 or not resp.get("success"):
        print("error: generation failed (HTTP %d): %s" % (status, resp.get("message")), file=sys.stderr)
        return 1
    content = resp["data"]["content"]
    print("preview generated:")
    print("  subject: %s" % content.get("email_subject", ""))
    print("  rationale: %s" % content.get("rationale", ""))
    print("  suggested template: %s" % resp["data"].get("template_name", ""))
    print("  suggested page: %s" % resp["data"].get("page_name", ""))

    if not args.apply:
        print("dry run only; re-run with --apply --authorization REF to create the content")
        return 0

    if not args.authorization:
        print("error: --authorization is required with --apply", file=sys.stderr)
        return 2

    payload = dict(brief)
    payload["apply"] = True
    payload["authorization"] = args.authorization
    payload["content"] = content
    if args.campaign_name:
        payload.update({
            "campaign_name": args.campaign_name,
            "url": args.url,
            "smtp_name": args.smtp,
            "groups": [g.strip() for g in args.groups.split(",") if g.strip()],
        })
    status, resp = api_call(args.base_url, args.api_key, "POST", "/api/ai/campaign", payload)
    if status not in (200, 201) or not resp.get("success"):
        print("error: apply failed (HTTP %d): %s" % (status, resp.get("message")), file=sys.stderr)
        return 1
    data = resp["data"]
    print("created: template_id=%s page_id=%s campaign_id=%s" % (
        data.get("template_id"), data.get("page_id"), data.get("campaign_id")))

    campaign_id = data.get("campaign_id")
    if args.watch > 0 and campaign_id:
        deadline = time.time() + args.watch
        while time.time() < deadline:
            status, summary = api_call(args.base_url, args.api_key, "GET",
                                       "/api/campaigns/%s/summary" % campaign_id)
            if status == 200:
                stats = summary.get("stats", {})
                print("campaign %s: status=%s sent=%s opened=%s clicked=%s submitted=%s" % (
                    campaign_id, summary.get("status"),
                    stats.get("sent", "?"), stats.get("opened", "?"),
                    stats.get("clicked", "?"), stats.get("submitted", "?")))
                if summary.get("status") == "Completed":
                    break
            time.sleep(15)
    return 0


if __name__ == "__main__":
    sys.exit(main())
