// moxielang.dev as a Cloudflare Worker: the same two answers as the Caddyfile in
// this directory, for a domain whose DNS is on Cloudflare. Route it as
// `moxielang.dev/*` (and `www.moxielang.dev/*` if that name exists).
const REPO = "https://github.com/moxie-language/toolchain";
const GO_IMPORT = "moxielang.dev git https://github.com/moxie-language/toolchain.git";

const PAGE = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8">
<meta name="go-import" content="${GO_IMPORT}">
<meta http-equiv="refresh" content="0; url=${REPO}">
<title>moxielang.dev - the Moxie toolchain</title></head>
<body><h1>moxielang.dev</h1>
<p>The Moxie toolchain lives at <a href="${REPO}">github.com/moxie-language/toolchain</a>.</p>
<script>location.replace("${REPO}");</script>
</body></html>`;

export default {
  fetch(request) {
    const url = new URL(request.url);
    if (url.searchParams.get("go-get") === "1") {
      return new Response(PAGE, {
        headers: {
          "content-type": "text/html; charset=utf-8",
          "cache-control": "public, max-age=300",
        },
      });
    }
    return Response.redirect(REPO, 301);
  },
};
