// sw-register.js — register the bundle's service worker, from its own directory.
//
// The worker script is staged in $sw/ and its scope is the bundle's directory,
// which is wider than the script's own path: the browser allows that only when
// the server answers the script with `Service-Worker-Allowed: <scope>` (or
// wider). A deployment that omits the header gets a SecurityError here rather
// than a worker that silently controls nothing but $sw/.
//
// Emitted beside the module by `moxie build -target wasm`, and written against
// document.currentScript so it needs no knowledge of where the bundle was
// deployed: a site at the root and a bundle under /sub/dir/ both register the
// worker next to themselves and give it the same subtree as its scope. An
// absolute /$sw/ path (which is what apps wrote by hand) only works at a root.
(function () {
  if (!("serviceWorker" in navigator)) return;
  var here = new URL(".", document.currentScript.src);
  navigator.serviceWorker
    .register(new URL("$sw/wasm-host-sw.mjs", here).href, {
      type: "module",
      scope: here.pathname,
      updateViaCache: "none",
    })
    .then(function (reg) {
      // An installed update waits until every client of the old worker is gone
      // unless it is told to take over now; the module asks for that itself, so
      // only the first install is forced here.
      reg.addEventListener("updatefound", function () {
        var installing = reg.installing;
        if (!installing) return;
        installing.addEventListener("statechange", function () {
          if (installing.state === "installed" && navigator.serviceWorker.controller) {
            installing.postMessage("SKIP_WAITING");
          }
        });
      });
    })
    .catch(function (err) {
      console.error("[sw-register] failed: " + String(err));
    });
})();
