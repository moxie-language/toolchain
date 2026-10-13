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
raw = sys.argv[1:]
args = [a for a in raw if a not in ("-q", "--host")]
QUIET = "-q" in raw
# --host <url> loads an emitted bundle host page-side instead of the
# repository's loader; the module URL it carries is the one that runs.
HOST = raw[raw.index("--host") + 1] if "--host" in raw else None
WASM = next((a for a in args if a != "-q"), str(ROOT / "app.wasm"))
TIMEOUT = 20000


class Handler(http.server.SimpleHTTPRequestHandler):
    def translate_path(self, path):
        path = path.split("?")[0].split("#")[0]
        if path.startswith("/jsruntime/"):
            return str(REPO) + path
        if path == "/":
            return str(ROOT / "host.html")
        return str(ROOT) + path

    def end_headers(self):
        # SharedArrayBuffer - the backing store for every channel that crosses
        # a spawn boundary - only exists in a cross-origin isolated context.
        # Without these two headers the browser leaves the constructor
        # undefined and channel_create fails before a domain can start.
        self.send_header("Cross-Origin-Opener-Policy", "same-origin")
        self.send_header("Cross-Origin-Embedder-Policy", "require-corp")
        super().end_headers()

    def log_message(self, *args):
        pass


def main():
    # The default host page loads /app.wasm; an emitted bundle host carries its
    # own module URL, so with --host there is nothing to copy unless a module
    # was named as well.
    if not HOST or pathlib.Path(WASM).exists():
        (ROOT / "app.wasm").write_bytes(pathlib.Path(WASM).read_bytes())

    srv = socketserver.TCPServer(("127.0.0.1", 0), functools.partial(Handler, directory=str(ROOT)))
    srv.allow_reuse_address = True
    port = srv.server_address[1]
    threading.Thread(target=srv.serve_forever, daemon=True).start()

    with sync_playwright() as pw:
        browser = pw.chromium.launch()
        page = browser.new_page()
        url = f"http://127.0.0.1:{port}/"
        if HOST:
            url += "?host=" + HOST
        page.goto(url, wait_until="load")
        try:
            page.wait_for_function("window.__done === true", timeout=TIMEOUT)
        except Exception:
            print("TIMEOUT: module did not finish loading")
            browser.close()
            srv.shutdown()
            return 1
        # The module is passive after _start: the page drives it through the
        # relay, so its last output can arrive after __done. Give the callbacks
        # room to land before reading the log.
        page.wait_for_timeout(600)
        out = page.evaluate("() => ({logs: window.__logs, error: window.__error, errs: window.__consoleErrors})")
        browser.close()
    srv.shutdown()

    if not QUIET:
        print("stdout: " + json.dumps(out["logs"]))
    if out["errs"]:
        print("console errors: " + json.dumps(out["errs"]))
    if out["error"]:
        print("JS error: " + out["error"])
        return 1
    # -q prints only the module's stdout, so callers can diff it against a
    # native run.
    for line in out["logs"]:
        print(line, end="")
    return 0


if __name__ == "__main__":
    sys.exit(main())
