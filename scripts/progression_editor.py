#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
WarLink Interactive Progression Database Editor Local Server.
Runs on http://127.0.0.1:8888 and opens default browser.
"""

import http.server
import json
import os
import shutil
import socketserver
import subprocess
import sys
import time
import urllib.parse
import webbrowser

PORT = 8888
BASE_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
DB_PATH = os.path.join(BASE_DIR, "internal", "progression", "progression_db.json")
HTML_PATH = os.path.join(BASE_DIR, "scripts", "progression_editor.html")
ICONS_DIR = os.path.join(BASE_DIR, "guides", "icons")
ASSETS_DIR = os.path.join(BASE_DIR, "assets")

class ProgressionEditorHandler(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.send_header("Cache-Control", "no-cache, no-store, must-revalidate")
        super().end_headers()

    def do_OPTIONS(self):
        self.send_response(200)
        self.end_headers()

    def do_GET(self):
        url = urllib.parse.urlparse(self.path)
        path = url.path

        if path in ("/", "/index.html"):
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.end_headers()
            with open(HTML_PATH, "rb") as f:
                self.wfile.write(f.read())
            return

        if path == "/api/db":
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.end_headers()
            with open(DB_PATH, "rb") as f:
                self.wfile.write(f.read())
            return

        # Serve icons from guides/icons
        if "items/" in path or path.startswith("/guides/icons/") or path.startswith("/static/"):
            filename = os.path.basename(path)
            target = os.path.join(ICONS_DIR, filename)
            if os.path.exists(target):
                self.send_response(200)
                if filename.endswith(".webp"):
                    self.send_header("Content-Type", "image/webp")
                elif filename.endswith(".png"):
                    self.send_header("Content-Type", "image/png")
                else:
                    self.send_header("Content-Type", "application/octet-stream")
                self.end_headers()
                with open(target, "rb") as f:
                    self.wfile.write(f.read())
                return

        # Serve assets
        if path.startswith("/assets/"):
            rel = path[len("/assets/"):]
            target = os.path.join(ASSETS_DIR, rel)
            if os.path.exists(target):
                self.send_response(200)
                self.send_header("Content-Type", "image/webp")
                self.end_headers()
                with open(target, "rb") as f:
                    self.wfile.write(f.read())
                return

        super().do_GET()

    def do_POST(self):
        url = urllib.parse.urlparse(self.path)
        path = url.path

        if path == "/api/save":
            content_length = int(self.headers.get("Content-Length", 0))
            body = self.rfile.read(content_length)
            try:
                data = json.loads(body.decode("utf-8"))
                # Ensure version & unlocks count
                data["version"] = data.get("version", "2.0.0")
                if "unlocks" in data:
                    data["total_unlocks"] = len(data["unlocks"])

                # Create backup
                backup_name = f"progression_db.json.bak.{int(time.time())}"
                backup_path = os.path.join(os.path.dirname(DB_PATH), backup_name)
                try:
                    shutil.copyfile(DB_PATH, backup_path)
                except Exception as berr:
                    print(f"[WARN] Failed to create backup: {berr}")

                # Save updated DB
                with open(DB_PATH, "w", encoding="utf-8") as f:
                    json.dump(data, f, ensure_ascii=False, indent=2)

                print(f"[SAVE] Successfully saved {data.get('total_unlocks')} unlocks to {DB_PATH}")

                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps({
                    "success": True,
                    "total_unlocks": data.get("total_unlocks"),
                    "backup": backup_name
                }).encode("utf-8"))
            except Exception as e:
                self.send_response(500)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps({"error": str(e)}).encode("utf-8"))
            return

        if path == "/api/deploy":
            # Deploy to Stockholm VPS
            try:
                ssh_key = os.path.expanduser(r"~/.ssh/id_ed25519")
                cmd = [
                    "scp",
                    "-i", ssh_key,
                    "-o", "StrictHostKeyChecking=no",
                    DB_PATH,
                    "root@138.124.103.99:/opt/warlink-server/progression_db.json"
                ]
                res = subprocess.run(cmd, capture_output=True, text=True, timeout=15)
                if res.returncode == 0:
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    self.wfile.write(json.dumps({"success": True}).encode("utf-8"))
                else:
                    self.send_response(500)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    self.wfile.write(json.dumps({"error": res.stderr}).encode("utf-8"))
            except Exception as e:
                self.send_response(500)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps({"error": str(e)}).encode("utf-8"))
            return

        self.send_response(404)
        self.end_headers()

def run_server():
    socketserver.TCPServer.allow_reuse_address = True
    server_address = ("127.0.0.1", PORT)
    httpd = socketserver.TCPServer(server_address, ProgressionEditorHandler)
    url = f"http://127.0.0.1:{PORT}"
    print(f"======================================================")
    print(f" WARLINK // PROGRESSION DATABASE EDITOR RUNNING")
    print(f" URL: {url}")
    print(f" Press Ctrl+C to stop the server.")
    print(f"======================================================")

    try:
        webbrowser.open(url)
    except Exception as e:
        print(f"Could not open browser automatically: {e}")

    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down server...")
        httpd.server_close()

if __name__ == "__main__":
    run_server()
