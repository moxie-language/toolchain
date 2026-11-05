#!/usr/bin/env python3
"""Run a Moxie wasm module in a real browser and report its stdout.

Serves the repository plus the module over HTTP (fetch cannot read file://),
loads tests/wasm/host.html, and returns the module's output and any JS error.
Exits non-zero if the module failed to load or produced an error.
"""
import functools
import http.server
import json
import os
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
MODULE = WASM
# How long the page waits for the module to boot. The caller owns this bound:
# a module that never yields can only be stopped from outside, so validate.sh
# wraps the whole run in RUN_TIMEOUT and this is the friendlier, more specific
# bound inside it. RUN_WAIT_MS raises it for a module whose load is genuinely
# slow (a 7MB crypto closure instantiating in a worker), which is information
# the gate should show as a slow pass rather than as a boot failure.
TIMEOUT = int(os.environ.get("RUN_WAIT_MS", "20000"))
# How long the page keeps collecting output after it reports the module's fate.
# A fixture that asserts another domain did NOT act needs a window longer than
# that domain's own delay; a panic/tree-stop test is the reason this exists.
SETTLE = int(os.environ.get("RUN_SETTLE_MS", "600"))
SW_TIMEOUT = int(os.environ.get("RUN_WAIT_MS", "45000"))


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
        if path == "/app.wasm":
            # The module under test is served from where it actually is, not
            # from tests/wasm/app.wasm. Staging it there made every concurrent
            # validation run overwrite the others' module - phase 5 runs one
            # unit per fixture, four at a time - so a unit could execute a
            # different fixture, or fail to load at all, depending on timing.
            # That is exactly how it presented: the same fixture passed alone
            # and failed in the sweep, with a different member of the set each
            # run.
            return str(pathlib.Path(MODULE).resolve())
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


# Chromium refuses to navigate to some ports whatever the server is: its
# restricted list (net/base/port_util.cc) covers 1-1023 plus a handful of
# well-known service ports. The kernel handed one of those out on an ephemeral
# bind in a phase-5 run (2049), and `Page.goto` failed with ERR_UNSAFE_PORT -
# one random fixture red per sweep, different each time. Rebind until the port
# is navigable; the list is fixed, so this terminates.
CHROMIUM_UNSAFE_PORTS = frozenset(
    list(range(1, 1024)) + [2049, 3659, 4045, 4190, 5060, 5061, 6000,
                            6566, 6679, 6697, 10080] + list(range(6665, 6670)))


def bind_test_server():
    for _ in range(32):
        srv = socketserver.TCPServer(
            ("127.0.0.1", 0), functools.partial(Handler, directory=str(ROOT)))
        srv.allow_reuse_address = True
        if srv.server_address[1] not in CHROMIUM_UNSAFE_PORTS:
            return srv
        srv.server_close()
    raise SystemExit("run.py: could not bind a port Chromium will navigate to")


def main():
    # The default host page loads /app.wasm; an emitted bundle host carries its
    # own module URL, so with --host nothing needs staging. Nothing is copied in
    # either case: translate_path serves /app.wasm from the module this process
    # was given, which is what keeps concurrent runs independent.

    srv = bind_test_server()
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
            # A module that parks or spins never sets __done, and its logs are
            # the only evidence of how far it got. Dropping them made a hang
            # look like a module that never started.
            try:
                sofar = page.evaluate(
                    "() => ({logs: window.__logs || [], errs: window.__consoleErrors || []})"
                )
                print("TIMEOUT: module did not finish loading; stdout so far: "
                      + json.dumps(sofar["logs"]))
                for e in sofar["errs"]:
                    sys.stderr.write(e if e.endswith("\n") else e + "\n")
            except Exception:
                print("TIMEOUT: module did not finish loading")
            browser.close()
            srv.shutdown()
            return 1
        # The module is passive after _start: the page drives it through the
        # relay, so its last output can arrive after __done. Give the callbacks
        # room to land before reading the log.
        page.wait_for_timeout(SETTLE)
        out = page.evaluate(
            "() => ({logs: window.__logs, error: window.__error,"
            " panicReason: window.__panicReason || null,"
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
        # A module's stderr is diagnostics, and -q promises that stdout carries
        # the program's output and nothing else: the caller diffs it against the
        # native run. So in quiet mode the diagnostics go to this process's
        # stderr, where they stay readable in the log and stay out of the diff.
        # A runtime warning the wasm target emits (a codec arena escape, say) is
        # then visible without making the fixture look wrong.
        if QUIET:
            for e in out["errs"]:
                sys.stderr.write(e if e.endswith("\n") else e + "\n")
        else:
            print("console errors: " + json.dumps(out["errs"]))
    if out["error"]:
        # A module that traps never reaches the end of main, so the logs it did
        # produce are the only evidence of how far it got - and in -q mode they
        # used to be dropped on this path, which cost a whole round of
        # instrumenting a wasm-only crash blind. One line, so the caller's
        # `tail -3` still shows the error itself.
        print("JS error: " + out["error"])
        if QUIET and out["logs"]:
            print("stdout so far: " + " | ".join(out["logs"]))
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
    # A panic in a spawned domain stops the whole tree; the page records the
    # action on the hook host.html defines. That is a failure for an ordinary
    # fixture and the expected outcome for a panic test, so report it as its own
    # verdict and let the caller decide.
    if out["panicReason"] is not None:
        print("panic action: " + out["panicReason"], file=sys.stderr)
        for line in out["logs"]:
            print(line, end="")
        return 1
    # -q prints only the module's stdout, so callers can diff it against a
    # native run.
    for line in out["logs"]:
        print(line, end="")
    return 0


if __name__ == "__main__":
    sys.exit(main())
