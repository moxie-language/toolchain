# moxielang.dev, the vanity import root

`moxielang.dev` is the toolchain's canonical address. Three kinds of client ask
for it and they need three different answers, from the same URL space:

| client | request | answer |
| --- | --- | --- |
| Go tooling (`go get`, `go list`) | `GET /<path>?go-get=1` | **200** with `<meta name="go-import" content="moxielang.dev git https://github.com/moxie-language/toolchain.git">`. A redirect here is a resolution failure, which is why the discovery document is served and not bounced. |
| a browser | `GET /`, `GET /<path>` | **301** to `https://github.com/moxie-language/toolchain`. A real redirect, not a meta refresh: a client-side bounce is invisible in `curl -I`, unverifiable, and useless to anything that is not a browser. |
| git's HTTP transport | `GET /info/refs?service=git-upload-pack` | **301** to `https://github.com/moxie-language/toolchain.git{uri}`, path and query preserved, so `git clone https://moxielang.dev` works. git follows a redirect only when the target is still a git endpoint. |

`www.moxielang.dev` is a canonical-host redirect to the bare name.

## What is deployed

On the host the A records point at (`smesh.lol`, Caddy 2.11):

- `/var/www/moxielang/index.html` - this directory's `public/index.html`.
- `/etc/caddy/Caddyfile` - the `moxielang.dev` and `www.moxielang.dev` blocks
  from this directory's `Caddyfile`, with `{$VANITY_ADDR}` resolved to
  `moxielang.dev` and `{$VANITY_ROOT}` to `/var/www/moxielang`.
  TLS is Caddy's: Let's Encrypt, automatic (the certificate for the name was
  obtained on the first reload).

DNS: `A`/`AAAA` for `moxielang.dev` and `www.moxielang.dev` at that host.
Nothing else is needed - there is no Cloudflare in front of it.

## Verification

```sh
# 1. The discovery document (Go's view).
curl -s "https://moxielang.dev/pkg/mxutil?go-get=1" | grep go-import
#   <meta name="go-import" content="moxielang.dev git https://github.com/moxie-language/toolchain.git">

# 2. The browser redirect (a real 301).
curl -sI https://moxielang.dev/ | head -3
#   HTTP/2 301
#   location: https://github.com/moxie-language/toolchain

# 3. git over the vanity address.
git ls-remote https://moxielang.dev
#   warning: redirecting to https://github.com/moxie-language/toolchain.git/
#   73bcff23...  HEAD
#   73bcff23...  refs/heads/dev
```

Checked on 2026-11-03: the certificate was issued on the reload, and all three
lines above came back exactly as shown.

## What consumes the discovery document

`moxie get` reads it. An argument with no scheme is resolved through the
address's own document before any checkout is attempted, so

```sh
moxie get moxielang.dev/pkg/mxutil
#   clone https://github.com/moxie-language/toolchain.git -> ~/moxie/moxielang.dev
#   ~/moxie/moxielang.dev/pkg/mxutil
```

works with no knowledge of the repository host, and it resolves the module root
to `moxielang.dev` - not to the `moxielang.dev/pkg` that a host-plus-one-segment
guess would produce. `-offline` skips the fetch and uses what is checked out.

## Not part of the contract: `go get` of a package

The repository is Moxie source (`.mx`), not Go source, and it has no root
`go.mod`. `go get moxielang.dev/pkg/mxutil` therefore resolves the module root
and stops with "no Go files" - correct, and not something a `go.mod` would fix
without shipping Go packages that do not exist.
