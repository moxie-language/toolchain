# Moxie Formatting (moxiefmt)

## 80-Column Single-Line Rule

If content within `{ }` fits under 80 chars with `; ` separating statements, the single-line form is canonical:

```moxie
if err != nil { return err }
struct{ x int32; y int32 }
for i := int32(0); i < n; i++ { sum += i }
func name() (n int32) { return 1 }
```

Multi-line when exceeding 80 chars or for clarity.

## Indentation

Tabs, not spaces. One tab per nesting level. Tab width = 4 spaces. No mixing.

## Multi-Line Blocks

- Opening `{` on same line as preamble
- Each statement on own line, indented one level
- Closing `}` on own line, aligned with preamble

## Semicolons

Explicit `;` separates statements on single line. Multi-line: newlines replace semicolons, no trailing `;`.

## Spacing Rules

- No space before `{` when attaching to type keyword: `struct{`, `interface{`, `TypeName{`, `map[K]V{`, `[]T{`
- Space before `{` after expressions: `if err != nil {`, `for i < n {`
- Space before `{` after `)`: `func name() {`
- Single space after `;` in single-line blocks
- Single space around binary operators
- No space between function name and `(`
- No space inside `()` or `[]`

## Blank Lines

- One blank line between top-level declarations
- No blank line after opening `{` or before closing `}`
- Optional single blank line within function body

## Imports

Always multi-line with `()`:
```moxie
import(
    "fmt"
    "os"
)
```

## Vertical Alignment

Contiguous items in `struct{`, `interface{`, function bodies are vertically aligned by column. Double blank line breaks alignment group.

```moxie
struct{
    name    string
    address string
    port    int32
}
```

Trailing `//` comments also vertically aligned within contiguous block.

## Return Values

All return values wrapped in `()` with named parameters. Even single returns:
```moxie
func f() (err error)       // correct
func f() error             // wrong
```

## WARNING: moxiefmt -w

`moxiefmt -w` (in-place mode) is **destructive** - it can split statements and change semantics. Never use in-place mode on untested code. Always review output before accepting.
