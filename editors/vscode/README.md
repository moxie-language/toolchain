# Moxie language support for VS Code / VSCodium

Everything Moxie's own compiler knows, in the editor: syntax highlighting,
snippets for the shapes the language requires, and diagnostics straight from
`moxie build`. No second grammar to drift - the keyword, builtin and type sets
mirror `pkg/token` and `pkg/types`, and diagnostics are the compiler's own
positioned errors.

## What it does

- **Highlighting** (`syntaxes/moxie.tmLanguage.json`): keywords per
  `pkg/token/tokens.mx` (note what is *absent*: no `go`, no `fallthrough`), the
  builtin types (`byte`/`rune` as aliases of `uint8`/`int32`, no `int`/`uint`),
  the builtin functions (`push`, `pop`, `resize`, `len`, `cap`, `copy`,
  `delete`, `close`, `clear`, `min`, `max`, `panic`, `recover`, `print`,
  `println`, `spawn`), `//:build` / `//:linkname` / `//export` pragmas, and the
  names Moxie *removed* (`make`, `new`, `append`, `reflect`, `any`, `int`,
  `uint`, `complex64/128`) marked as invalid so their use stands out.
- **Snippets** (`snippets/moxie.json`): named returns, pointer receiver
  methods, sovereign self-mutating types, `init()` + zero-value globals,
  `[]T{:n:cap}` presizing, `mxutil.Ensure` before a spread push, a spawn
  worker, interface assertions.
- **Diagnostics**: on open and on save the document's package is built with
  `moxie build` and its errors appear as problems, in place. `Moxie: Build the
  package and report diagnostics` (Ctrl+Alt+B) runs it on demand and shows the
  full output.
- **Completion / hover**: keywords, builtins and types with a one-line
  explanation of the Moxie semantics, not the Go one.
- **Go to definition**: a workspace index of `func`, `type`, `var` and `const`
  declarations (including pointer receiver methods).

## Install

VSCodium reads `~/.vscode-oss/extensions/`:

```sh
mkdir -p ~/.vscode-oss/extensions
rm -rf ~/.vscode-oss/extensions/mleku.moxie-lang-*
cp -r editors/vscode ~/.vscode-oss/extensions/mleku.moxie-lang-1.1.0
```

Then reload the window. `code`/`codium --install-extension <path>.vsix` works
too when the folder is packaged with `vsce`.

## Settings

| setting | default | meaning |
| --- | --- | --- |
| `moxie.path` | `moxie` | the compiler to run; an absolute path to a tree's `./moxie` works |
| `moxie.moxieRoot` | *(empty)* | `MOXIEROOT` for the compiler; empty means the workspace folder |
| `moxie.buildOnSave` | `true` | build the package on save for diagnostics |
| `moxie.buildFlags` | `[]` | extra flags, e.g. `-allow-closure-captures` |
| `moxie.trace` | `off` | `messages`/`verbose` echoes each build into the Moxie output channel |

## Why a build, not a parser

The editor could ship its own parser, and then it would disagree with the
compiler about named returns, pointer receivers, `string`/`[]byte`, or which
names are predeclared. Running the real compiler keeps the editor honest: what
it underlines is what a build rejects.
