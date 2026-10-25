#!/usr/bin/env python3
"""Owned external HTTP endpoint for the native sandbox-network contract."""
from http.server import BaseHTTPRequestHandler, HTTPServer
import sys

owner = sys.argv[1]


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = owner.encode()
        self.send_response(200)
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)


HTTPServer(('0.0.0.0', 18181), Handler).serve_forever()
