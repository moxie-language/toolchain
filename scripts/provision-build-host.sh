#!/bin/bash
# Provision a build host for the Moxie toolchain, from nothing.
#
# Every line here exists because a fresh machine was missing it and a build
# failed: this script IS the bootstrap documentation, versioned in the repo, so
# "it builds on my machine" stops being a statement about accumulated state.
# Run it as root on a clean Ubuntu 24.04 host, then run the bootstrap below.
#
# Versions are PINNED, not latest:
#
#   * Go 1.25.3 - legacy/build/patch-gotypes.go patches go/types by exact
#     source text, so a newer Go is a different source tree and the patch
#     refuses to apply (measured on 1.27.3). Bumping Go means re-checking every
#     anchor in that file in the same commit.
#   * LLVM 22 - the compiler's bitcode and its enum values; 23 reads 22 but the
#     purego layer misreports opcodes (see legacy/build.sh's header). The exact
#     tested build is recorded below and printed by --check.
#
# What the repo does NOT carry, and this script supplies:
#
#   * lib/compiler-rt-builtins - the repository tracked an ABSOLUTE symlink to
#     the author's llvm checkout, so a fresh clone has a dangling path. Fixed
#     here by cloning llvm-project at release/22.x and linking compiler-rt's
#     builtins into place.
#   * llvm-glue/libmoxie-llvm-glue.so - a build product of llvm-glue/build.sh
#     (clang++-22). Without it the legacy compiler dies at start-up with
#     "missing symbol MoxieCreateCompileUnit".
#   * unversioned LLVM tools - the compiler invokes `clang`, `opt`,
#     `llvm-link`, `llc` and `ld.lld` by bare name. On Debian/Ubuntu the
#     versioned packages install only `opt-22` etc., and the unversioned names
#     either do not exist or point at a different major (the author's host had
#     `opt` = 23). Symlinked in /usr/local/bin, which precedes /usr/bin.
#
# Usage:
#   provision-build-host.sh            # provision (idempotent)
#   provision-build-host.sh --check    # report, change nothing
#   provision-build-host.sh --bootstrap  # provision, then build and fixpoint
#
set -euo pipefail

GO_VERSION="1.25.3"
GO_SHA256="0335f314b6e7bfe08c3d0cfaa7c19db961b7b99fb20be62b0a826c992ad14e0f"
LLVM_MAJOR="22"
LLVM_TESTED="22.1.8"          # the build this tree was verified against
LLVM_BRANCH="release/22.x"    # compiler-rt sources for the target builtins
BUILD_ROOT="${BUILD_ROOT:-/srv/build}"
SRC_ROOT="${SRC_ROOT:-/srv/src}"
GO_ROOT="/opt/go${GO_VERSION}"
TOOLCHAIN="$BUILD_ROOT/toolchain"

CHECK_ONLY=0
RUN_BOOTSTRAP=0
for a in "$@"; do
	case "$a" in
	--check) CHECK_ONLY=1 ;;
	--bootstrap) RUN_BOOTSTRAP=1 ;;
	*) echo "usage: $0 [--check] [--bootstrap]" >&2; exit 2 ;;
	esac
done

say() { printf '\n== %s\n' "$*"; }
have() { command -v "$1" >/dev/null 2>&1; }
ok() { printf '   ok   %s\n' "$*"; }
miss() { printf '   MISS %s\n' "$*"; }

problems=0
want() { # want <description> <test command...>
	local desc="$1"; shift
	if "$@" >/dev/null 2>&1; then ok "$desc"; else miss "$desc"; problems=$((problems + 1)); fi
}

# ── check ───────────────────────────────────────────────────────────────────
say "checking the host for what a Moxie build needs"
for t in git curl tar xz sha256sum; do want "$t" have "$t"; done
want "go ${GO_VERSION}" test -x "$GO_ROOT/bin/go"
want "clang++-${LLVM_MAJOR}" have "clang++-${LLVM_MAJOR}"
for t in clang opt llvm-link llc ld.lld llvm-config; do
	want "unversioned $t is LLVM ${LLVM_MAJOR}" sh -c \
		"have() { command -v \"\$1\" >/dev/null 2>&1; }; have $t && $t --version 2>/dev/null | grep -q 'version ${LLVM_MAJOR}\.'"
done
want "libLLVM-${LLVM_MAJOR}.so" test -e "/usr/lib/x86_64-linux-gnu/libLLVM-${LLVM_MAJOR}.so" 
want "glue libmoxie-llvm-glue.so" test -e "$TOOLCHAIN/llvm-glue/libmoxie-llvm-glue.so"
want "compiler-rt builtins sources" test -e "$TOOLCHAIN/lib/compiler-rt-builtins/absvdi2.c"
if [ -x "$TOOLCHAIN/legacy/moxie" ]; then
	ok "legacy compiler built"
else
	miss "legacy compiler built (run bootstrap)"
fi

if [ "$CHECK_ONLY" = 1 ]; then
	printf '\n%d missing\n' "$problems"
	[ "$problems" = 0 ] || exit 1
	exit 0
fi

# ── provision ───────────────────────────────────────────────────────────────
say "packages"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq \
	build-essential ca-certificates git curl tar xz-utils rsync tmux python3 \
	python3-pip pkg-config zlib1g-dev libssl-dev gdb >/dev/null
ok "base packages"

# Playwright + chromium: the wasm half of phases 1c and 5 runs every module in a
# real browser, and without a browser it reports 116 units SKIPPED rather than
# failed - which is exactly how a bug that trapped every wasm program in package
# init survived a green suite. Skipping is not verification.
say "playwright chromium (phase 1c/5 browser runs)"
python3 -m pip install --break-system-packages -q playwright
python3 -m playwright install --with-deps chromium >/dev/null
python3 -c 'from playwright.sync_api import sync_playwright
with sync_playwright() as p:
    b = p.chromium.launch(); b.close()' || { echo "playwright chromium does not launch" >&2; exit 1; }
ok "chromium launches under playwright"

say "LLVM ${LLVM_MAJOR} (apt.llvm.org, tested ${LLVM_TESTED})"
if [ ! -e /etc/apt/sources.list.d/llvm-${LLVM_MAJOR}.list ]; then
	curl -fsSL https://apt.llvm.org/llvm-snapshot.gpg.key \
		| gpg --dearmor -o /etc/apt/keyrings/llvm.gpg
	echo "deb [signed-by=/etc/apt/keyrings/llvm.gpg] http://apt.llvm.org/$(. /etc/os-release && echo "$VERSION_CODENAME")/ llvm-toolchain-$(. /etc/os-release && echo "$VERSION_CODENAME")-${LLVM_MAJOR} main" \
		> "/etc/apt/sources.list.d/llvm-${LLVM_MAJOR}.list"
	apt-get update -qq
fi
apt-get install -y -qq \
	"clang-${LLVM_MAJOR}" "clang++-${LLVM_MAJOR}" "lld-${LLVM_MAJOR}" \
	"llvm-${LLVM_MAJOR}" "llvm-${LLVM_MAJOR}-dev" "libclang-${LLVM_MAJOR}-dev" \
	"libpolly-${LLVM_MAJOR}-dev" >/dev/null
ok "$(clang-${LLVM_MAJOR} --version | head -1)"

say "unversioned LLVM tools -> ${LLVM_MAJOR}"
for t in clang clang++ opt llvm-link llc ld.lld llvm-extract llvm-as llvm-dis llvm-config; do
	if [ -x "/usr/lib/llvm-${LLVM_MAJOR}/bin/$t" ]; then
		ln -sf "/usr/lib/llvm-${LLVM_MAJOR}/bin/$t" "/usr/local/bin/$t"
	fi
done
hash -r
ok "clang now: $(clang --version | head -1)"

say "Go ${GO_VERSION} at ${GO_ROOT}"
if [ ! -x "$GO_ROOT/bin/go" ]; then
	cd /tmp
	curl -fsSL -o "go${GO_VERSION}.tar.gz" "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
	echo "${GO_SHA256}  go${GO_VERSION}.tar.gz" | sha256sum -c -
	rm -rf "/tmp/go-extract"
	mkdir -p "/tmp/go-extract"
	tar -C "/tmp/go-extract" -xzf "go${GO_VERSION}.tar.gz"
	rm -rf "$GO_ROOT"
	mv "/tmp/go-extract/go" "$GO_ROOT"
	rm -rf "/tmp/go-extract" "go${GO_VERSION}.tar.gz"
fi
ok "$("$GO_ROOT/bin/go" version)"

say "toolchain checkout at ${TOOLCHAIN}"
mkdir -p "$BUILD_ROOT" "$SRC_ROOT"
if [ ! -d "$TOOLCHAIN/.git" ]; then
	git clone -q -b dev https://github.com/moxie-language/toolchain.git "$TOOLCHAIN"
fi
ok "$(git -C "$TOOLCHAIN" log --oneline -1)"

say "compiler-rt builtins (llvm-project ${LLVM_BRANCH})"
if [ ! -e "$TOOLCHAIN/lib/compiler-rt-builtins/absvdi2.c" ]; then
	rm -f "$TOOLCHAIN/lib/compiler-rt-builtins"   # the tracked absolute symlink
	if [ ! -d "$SRC_ROOT/llvm-${LLVM_MAJOR}/.git" ]; then
		git clone -q --depth 1 --filter=blob:none --sparse \
			-b "$LLVM_BRANCH" https://github.com/llvm/llvm-project.git \
			"$SRC_ROOT/llvm-${LLVM_MAJOR}"
		git -C "$SRC_ROOT/llvm-${LLVM_MAJOR}" sparse-checkout set compiler-rt/lib/builtins
	fi
	ln -sfn "$SRC_ROOT/llvm-${LLVM_MAJOR}/compiler-rt/lib/builtins" \
		"$TOOLCHAIN/lib/compiler-rt-builtins"
fi
ok "$(ls "$TOOLCHAIN/lib/compiler-rt-builtins" | wc -l) builtins sources"

say "LLVM glue (llvm-glue/build.sh)"
if [ ! -e "$TOOLCHAIN/llvm-glue/libmoxie-llvm-glue.so" ]; then
	( cd "$TOOLCHAIN/llvm-glue" && CXX="clang++-${LLVM_MAJOR}" sh build.sh >/dev/null )
fi
ok "$(nm -D "$TOOLCHAIN/llvm-glue/libmoxie-llvm-glue.so" | grep -c ' T Moxie') Moxie shims"

say "legacy compiler"
if [ ! -x "$TOOLCHAIN/legacy/moxie" ]; then
	( cd "$TOOLCHAIN/legacy" && PATH="$GO_ROOT/bin:$PATH" GOROOT="$GO_ROOT" \
		bash build.sh >/tmp/provision-legacy.log 2>&1 ) \
		|| { echo "legacy build failed; log: /tmp/provision-legacy.log" >&2; exit 1; }
fi
ok "$("$TOOLCHAIN/legacy/moxie" version 2>&1 | head -1)"

if [ "$RUN_BOOTSTRAP" = 1 ]; then
	# The bootstrap protocol, encoded here rather than documented and hoped for:
	# build.sh produces s1 only, so the fixpoint is this script's job. Every
	# step uses `-a` (build.sh does) because a compiler built against a different
	# runtime must not reuse cached .bc.
	#
	# s1 MUST come from legacy/moxie, so MOXIE pins it. Left to itself build.sh
	# prefers ./moxie-new, then ./moxie, then legacy/moxie - a developer's order,
	# which is right for iterating on a tree whose compiler works and wrong for
	# a bootstrap, where ./moxie may be the very binary being replaced. Measured:
	# a stale ./moxie built s1, s1 inherited its short interface-dispatch table,
	# and s1 then panicked compiling compress_roundtrip with "no impl for Read
	# (16 impl(s), none matches)" - a bootstrap that reproduces the bug it is
	# supposed to replace.
	#
	# The bake is LAST and uses the installed compiler, because the bake is
	# compiler-specific: sysroot/runtime_go.bc is read by application links, and
	# a blob written by a different compiler generation is exactly the drift that
	# makes apps fail to link with undefined symbols.
	say "bootstrap: s1, s2, s3, fixpoint, bake"
	blog=/tmp/provision-bootstrap.log
	: >"$blog"
	cd "$TOOLCHAIN"
	export PATH="$GO_ROOT/bin:$PATH" GOROOT="$GO_ROOT" MOXIEROOT="$TOOLCHAIN"
	step() { # step <description> <command...>
		local desc="$1"; shift
		printf '   ...  %s\n' "$desc"
		if ! "$@" >>"$blog" 2>&1; then
			printf '   FAIL %s\n' "$desc" >&2
			tail -25 "$blog" >&2
			printf '\nbootstrap failed at: %s (log: %s)\n' "$desc" "$blog" >&2
			exit 1
		fi
	}
	step "s1 (legacy builds _mxc_stage4)" env MOXIE="$TOOLCHAIN/legacy/moxie" bash ./build.sh
	step "s2 (s1 rebuilds _mxc_stage4)" ./moxie build -a -o mxc-stage4-2 ./_mxc_stage4
	step "s3 (s2 rebuilds _mxc_stage4)" ./mxc-stage4-2 build -a -o mxc-stage4-3 ./_mxc_stage4
	s2="$(sha256sum mxc-stage4-2 | cut -d' ' -f1)"
	s3="$(sha256sum mxc-stage4-3 | cut -d' ' -f1)"
	# Convergence may take one generation longer than the minimum: the legacy
	# pipeline differs (interp + LowerInterfaces + globaldce) and the first
	# bootstrap inherits that, so s1 != s2 is expected and s2 == s3 is not the
	# only acceptable arrival - s3 == s4 is the other (CLAUDE.md, Convergence).
	# What is never acceptable is no generation reproducing itself at all.
	# Measured on the box: this tree took the extra generation, s3 == s4.
	if [ "$s2" = "$s3" ]; then
		ok "fixpoint: s2 == s3 (${s2:0:16})"
		final=mxc-stage4-2
	else
		step "s4 (s3 rebuilds _mxc_stage4; s2 != s3, so one extra generation)" \
			./mxc-stage4-3 build -a -o mxc-stage4-4 ./_mxc_stage4
		s4="$(sha256sum mxc-stage4-4 | cut -d' ' -f1)"
		if [ "$s3" != "$s4" ]; then
			printf '   FAIL fixpoint: no generation reproduces itself\n' >&2
			printf '         s2=%s\n         s3=%s\n         s4=%s (log: %s)\n' \
				"$s2" "$s3" "$s4" "$blog" >&2
			exit 1
		fi
		ok "fixpoint: s3 == s4 (${s3:0:16})"
		final=mxc-stage4-3
	fi
	cp "$final" ./moxie
	ok "installed $final as ./moxie"
	step "runtime bake (with the installed compiler)" ./moxie build-runtime
	ok "baked $(sha256sum sysroot/runtime_go.bc | cut -d' ' -f1 | cut -c1-16)"
fi

printf '\nprovisioned. Re-run with --check to verify, then:\n'
printf '  %s --bootstrap   # s1 -> s2/s3 (or s3/s4) -> bake, the whole protocol\n' "$0"
printf '  cd %s && bash run_tests.sh      # the full gate\n' "$TOOLCHAIN"
