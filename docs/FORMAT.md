# Moxie Formatting and Style Specification

This document specifies the canonical formatting for `.mx` source files. The
`moxiefmt` tool enforces these rules mechanically.

---

## 1. The 80-Column Single-Line Rule

If the content within a `{ }` block fits under 80 characters wide with `; `
separating statements, the single-line form is canonical.

This rule applies universally to all brace-delimited constructs:

- `if`, `for`, `select`, `switch`
- `struct`, `interface`
- `[]` (slice/array literals), `map[]` (map literals)
- `func` bodies

Single-line:

    if err != nil { return err }
    struct{ x int32; y int32 }
    for i := int32(0); i < n; i++ { sum += i }
    func name() { return 1 }

Multi-line (exceeds 80 chars, or author preference for clarity):

    struct{
        name string
        addr string
        port int32
    }

---

## 2. Indentation

Tabs, not spaces. One tab per nesting level. Tab width = 4 spaces. No mixed
indentation.

---

## 3. Multi-Line Break Rules

When a block exceeds the 80-column threshold:

- Opening `{` stays on the same line as the preamble
- Each statement gets its own line, indented one level
- Closing `}` on its own line, aligned with the preamble

---

## 4. Semicolons

Explicit semicolons separate statements on a single line. In multi-line form,
newlines replace semicolons - no trailing semicolons at line ends.

---

## 5. Spacing

- No space before `{` when it attaches to a type keyword or type name:
  `struct{`, `interface{`, `TypeName{`, `map[K]V{`, `[]T{`
- Space before `{` in control flow where `{` follows an expression:
  `if err != nil {`, `for i < n {`
- Space before `{` when it follows `)`:
  `func name() {`, `if f() {`
- Single space after `;` in single-line blocks
- Single space around binary operators
- No space between function name and `(`
- No space inside `()` or `[]`

Note (language constraint, not purely formatting): all return values must be
named. Return signatures take the form `(name type, name type)` with no spaces
between the parens and the list. This applies universally: top-level functions,
methods, and method fields within struct/interface definitions.

---

## 6. Blank Lines

- One blank line between top-level declarations
- No blank line after opening `{` or before closing `}`
- Optional single blank line within a function body to separate logical sections

---

## 7. Imports, Requires, Replace

Always multi-line with `()` wrapping, one statement per line. The single-line
form is not valid.

    import(
        "fmt"
        "os"
    )

    requires(
        example.com/pkg v1.2.3
    )

    replace(
        example.com/pkg => ../local/pkg
    )

---

## 8. Vertical Alignment

Contiguous items within `struct{`, `interface{`, and function bodies are
vertically aligned by column. A double blank line (two consecutive newlines with
no content) breaks an alignment group - items after the break start a new
independent alignment.

    struct{
        name    string
        address string
        port    int32


        x int32
        y int32
    }

Trailing `//` comments after statements are also vertically aligned within a
contiguous block. A single blank line continues the block; a double blank line
resets alignment.

    x := compute()     // first pass
    y := transform(x)  // second pass
    z := finalize(y)   // done

    a := other()  // new alignment group
    b := more()   // independent of above

---

## 9. Line Width

80 columns is the hard threshold for the single-line rule. Lines in multi-line
form should also target 80 columns but this is not enforced mechanically - the
formatter only uses 80 as the single-line/multi-line decision boundary.

---

## 10. Return Value Normalization

The formatter automatically wraps return values in `()` with named parameters.
Even a single return value is wrapped: `func f() (err error)` not
`func f() error`. This is a language constraint that `moxiefmt` enforces
mechanically.
