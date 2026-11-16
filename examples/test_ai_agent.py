#!/usr/bin/env python3
"""Tests for examples/ai_agent.py against a mock gophish2 API (stdlib only)."""

import json
import os
import sys
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import ai_agent

GENERATED = {
    "email_subject": "Facture en attente {{.FirstName}}",
    "email_text": "Bonjour {{.FirstName}}, consultez votre facture: {{.URL}}",
    "email_html": "<html><body><a href=\"{{.URL}}\">Facture</a></body></html>",
    "landing_html": "<html><body><form><input name=\"username\"><input type=\"password\" name=\"password\"></form></body></html>",
    "landing_redirect_url": "https://example.com/training",
    "capture_passwords": True,
    "rationale": "invoice fraud",
}

CALLS = []


class MockHandler(BaseHTTPRequestHandler):
    def _send(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _body(self):
        length = int(self.headers.get("Content-Length", 0))
        return json.loads(self.rfile.read(length).decode() or "{}")

    def do_GET(self):
        CALLS.append(("GET", self.path, None))
        if self.path == "/api/ai/config":
            self._send(200, {"enabled": True, "model": "mock"})
        elif self.path.startswith("/api/campaigns/") and self.path.endswith("/summary"):
            self._send(200, {"status": "Completed", "stats": {"sent": 2, "opened": 1, "clicked": 1, "submitted": 0}})
        else:
            self._send(404, {"success": False, "message": "not found"})

    def do_POST(self):
        payload = self._body()
        CALLS.append(("POST", self.path, payload))
        if self.path == "/api/ai/generate":
            self._send(200, {"success": True, "data": {"content": GENERATED, "template_name": "T", "page_name": "P", "applied": False}})
        elif self.path == "/api/ai/campaign":
            self._send(201, {"success": True, "data": {"content": GENERATED, "applied": True, "template_id": 7, "page_id": 8, "campaign_id": 9}})
        else:
            self._send(404, {"success": False, "message": "not found"})

    def log_message(self, *args):
        pass


class AIAgentTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = HTTPServer(("127.0.0.1", 0), MockHandler)
        cls.port = cls.server.server_address[1]
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()

    def setUp(self):
        CALLS.clear()

    def base_args(self):
        return ["--base-url", "http://127.0.0.1:%d" % self.port, "--api-key", "k", "--scenario", "invoice_fraud"]

    def test_dry_run_does_not_apply(self):
        rc = ai_agent.main(self.base_args())
        self.assertEqual(rc, 0)
        posts = [c for c in CALLS if c[0] == "POST"]
        self.assertEqual(len(posts), 1)
        self.assertEqual(posts[0][1], "/api/ai/generate")

    def test_apply_requires_authorization(self):
        rc = ai_agent.main(self.base_args() + ["--apply"])
        self.assertEqual(rc, 2)
        self.assertFalse(any(c[1] == "/api/ai/campaign" for c in CALLS))

    def test_apply_creates_and_watches(self):
        rc = ai_agent.main(self.base_args() + [
            "--apply", "--authorization", "SEC-1",
            "--campaign-name", "C", "--url", "https://phish.example.com",
            "--smtp", "SMTP", "--groups", "G1, G2", "--watch", "1",
        ])
        self.assertEqual(rc, 0)
        campaign_calls = [c for c in CALLS if c[0] == "POST" and c[1] == "/api/ai/campaign"]
        self.assertEqual(len(campaign_calls), 1)
        payload = campaign_calls[0][2]
        self.assertTrue(payload["apply"])
        self.assertEqual(payload["authorization"], "SEC-1")
        self.assertEqual(payload["content"], GENERATED)
        self.assertEqual(payload["groups"], ["G1", "G2"])
        self.assertEqual(payload["campaign_name"], "C")
        self.assertTrue(any(c[0] == "GET" and c[1] == "/api/campaigns/9/summary" for c in CALLS))

    def test_ai_disabled(self):
        class DisabledHandler(MockHandler):
            def do_GET(self):
                if self.path == "/api/ai/config":
                    self._send(200, {"enabled": False})
                else:
                    super().do_GET()
        server = HTTPServer(("127.0.0.1", 0), DisabledHandler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        try:
            rc = ai_agent.main(["--base-url", "http://127.0.0.1:%d" % server.server_address[1], "--api-key", "k"])
            self.assertEqual(rc, 1)
        finally:
            server.shutdown()


if __name__ == "__main__":
    unittest.main()
