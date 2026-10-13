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
args = [a for a in raw if a not in ("-q", "--host", "--sw", "--app", "--bundle", "--bridge")]
QUIET = "-q" in raw
# --host <url> loads an emitted bundle host page-side instead of the
# repository's loader; the module URL it carries is the one that runs.
HOST = raw[raw.index("--host") + 1] if "--host" in raw else None
SW = raw[raw.index("--sw") + 1] if "--sw" in raw else None
APP = raw[raw.index("--app") + 1] if "--app" in raw else None
BUNDLE = raw[raw.index("--bundle") + 1] if "--bundle" in raw else None
BRIDGE = raw[raw.index("--bridge") + 1] if "--bridge" in raw else None
WASM = next((a for a in args if a not in ("-q", "--sw", "--app", "--bundle", "--bridge")
             and a not in (SW, APP, BUNDLE, BRIDGE)), str(ROOT / "app.wasm"))
TIMEOUT = 20000
SW_TIMEOUT = 45000


REQUESTS = []


class Handler(http.server.SimpleHTTPRequestHandler):
    def do_GET(self):
        REQUESTS.append(self.path.split("?")[0])
        super().do_GET()

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
        # A bundle's service worker lives in $sw/ but must control the whole
        # bundle, which the browser refuses unless the server allows the wider
        # scope. An app's own server has to send this too, so the test server
        # does - otherwise the gate would only ever test a worker that controls
        # its own directory and nothing else.
        self.send_header("Service-Worker-Allowed", "/")
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
        q = []
        if HOST:
            q.append("host=" + HOST)
        if SW:
            q.append("sw=" + SW)
        if APP:
            q.append("app=" + APP)
        if BUNDLE:
            q.append("bundle=" + BUNDLE)
        if BRIDGE:
            q.append("bridge=" + BRIDGE)
        if q:
            url += "?" + "&".join(q)
        if SW:
            # A service worker's console is its own; without this a boot failure
            # inside the worker is invisible and only "evaluation failed"
            # reaches the page.
            def _on_sw(sw):
                sw.on("console", lambda m: print("sw console: " + m.text, file=sys.stderr))
            page.context.on("serviceworker", _on_sw)
        page.goto(url, wait_until="load")
        try:
            page.wait_for_function("window.__done === true", timeout=SW_TIMEOUT if SW else TIMEOUT)
        except Exception:
            print("TIMEOUT: module did not finish loading")
            browser.close()
            srv.shutdown()
            return 1
        # The module is passive after _start: the page drives it through the
        # relay, so its last output can arrive after __done. Give the callbacks
        # room to land before reading the log.
        page.wait_for_timeout(600)
        out = page.evaluate(
            "() => ({logs: window.__logs, error: window.__error,"
            " errs: window.__consoleErrors, sw: window.__sw || null,"
            " swError: window.__swError || null,"
            " bundle: window.__bundle || null,"
            " bundleError: window.__bundleError || null})"
        )
        browser.close()
    srv.shutdown()

    if not QUIET:
        print("stdout: " + json.dumps(out["logs"]))
    if out["errs"]:
        print("console errors: " + json.dumps(out["errs"]))
    if out["error"]:
        print("JS error: " + out["error"])
        return 1
    # The service worker's boot is reported on stderr so -q keeps printing only
    # the module's stdout, and it is a failure: a worker that never activated,
    # or that activated without fetching the module staged for it, has not
    # booted - it has merely not crashed loudly yet.
    # A second bundle's exchange is judged the same way the service worker's is:
    # the page must have received the module's message (which is what proves the
    # port reached it), and the module's own stdout is compared by the caller.
    if BUNDLE:
        got = (out["bundle"] or {}).get("got", "")
        print("bundle: got=" + json.dumps(got), file=sys.stderr)
        if out["bundleError"]:
            print("bundle error: " + out["bundleError"], file=sys.stderr)
            return 1
        if got != "hello-from-module":
            return 1
    if SW:
        fetched = any(p.endswith("/$sw/sw.wasm") for p in REQUESTS)
        state = (out["sw"] or {}).get("state", "none")
        print("sw: state=" + state + " module_fetched=" + str(fetched).lower()
              + " regs=" + json.dumps((out["sw"] or {}).get("regs", [])), file=sys.stderr)
        if out["swError"]:
            print("sw error: " + out["swError"], file=sys.stderr)
            return 1
        if state != "activated" or not fetched:
            return 1
    # -q prints only the module's stdout, so callers can diff it against a
    # native run.
    for line in out["logs"]:
        print(line, end="")
    return 0


if __name__ == "__main__":
    sys.exit(main())
