#!/usr/bin/env python3
"""Run a Moxie wasm module in a real browser and report its stdout.

Serves the repository plus the module over HTTP (fetch cannot read file://),
loads tests/wasm/host.html, and returns the module's output and any JS error.
Exits non-zero if the module failed to load or produced an error.
"""
import functools
import http.server
import json
import pathlib
import socketserver
import sys
import threading

try:
    from playwright.sync_api import sync_playwright
except ImportError:
    print("playwright is required: pip install playwright && playwright install chromium")
    sys.exit(2)

ROOT = pathlib.Path(__file__).resolve().parent
REPO = ROOT.parent.parent
WASM = sys.argv[1] if len(sys.argv) > 1 else str(ROOT / "app.wasm")
TIMEOUT = 20000


class Handler(http.server.SimpleHTTPRequestHandler):
    def translate_path(self, path):
        path = path.split("?")[0].split("#")[0]
        if path.startswith("/jsruntime/"):
            return str(REPO) + path
        if path == "/":
            return str(ROOT / "host.html")
        return str(ROOT) + path

    def log_message(self, *args):
        pass


def main():
    (ROOT / "app.wasm").write_bytes(pathlib.Path(WASM).read_bytes())

    srv = socketserver.TCPServer(("127.0.0.1", 0), functools.partial(Handler, directory=str(ROOT)))
    srv.allow_reuse_address = True
    port = srv.server_address[1]
    threading.Thread(target=srv.serve_forever, daemon=True).start()

    with sync_playwright() as pw:
        browser = pw.chromium.launch()
        page = browser.new_page()
        page.goto(f"http://127.0.0.1:{port}/", wait_until="load")
        try:
            page.wait_for_function("window.__done === true", timeout=TIMEOUT)
        except Exception:
            print("TIMEOUT: module did not finish loading")
            browser.close()
            srv.shutdown()
            return 1
        page.wait_for_timeout(200)
        out = page.evaluate("() => ({logs: window.__logs, error: window.__error, errs: window.__consoleErrors})")
        browser.close()
    srv.shutdown()

    print("stdout: " + json.dumps(out["logs"]))
    if out["errs"]:
        print("console errors: " + json.dumps(out["errs"]))
    if out["error"]:
        print("JS error: " + out["error"])
        return 1
    for line in out["logs"]:
        print(line, end="")
    return 0


if __name__ == "__main__":
    sys.exit(main())
