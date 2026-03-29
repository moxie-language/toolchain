package jsbackend

import (
	"fmt"
	"strings"
)

// Emitter builds JavaScript source code with proper indentation.
type Emitter struct {
	buf    strings.Builder
	indent int
	// Source file being emitted (for source map generation later).
	sourceFile string
}

// NewEmitter creates a new JS code emitter.
func NewEmitter() *Emitter {
	return &Emitter{}
}

// Indent increases the indentation level.
func (e *Emitter) Indent() {
	e.indent++
}

// Dedent decreases the indentation level.
func (e *Emitter) Dedent() {
	if e.indent > 0 {
		e.indent--
	}
}

// Line emits a line of code with proper indentation.
func (e *Emitter) Line(format string, args ...interface{}) {
	for i := 0; i < e.indent; i++ {
		e.buf.WriteString("  ")
	}
	fmt.Fprintf(&e.buf, format, args...)
	e.buf.WriteByte('\n')
}

// Raw emits raw text without indentation or newline.
func (e *Emitter) Raw(s string) {
	e.buf.WriteString(s)
}

// Newline emits an empty line.
func (e *Emitter) Newline() {
	e.buf.WriteByte('\n')
}

// Comment emits a JS comment.
func (e *Emitter) Comment(format string, args ...interface{}) {
	for i := 0; i < e.indent; i++ {
		e.buf.WriteString("  ")
	}
	e.buf.WriteString("// ")
	fmt.Fprintf(&e.buf, format, args...)
	e.buf.WriteByte('\n')
}

// Block opens a braced block: { with optional prefix.
func (e *Emitter) Block(format string, args ...interface{}) {
	for i := 0; i < e.indent; i++ {
		e.buf.WriteString("  ")
	}
	if format != "" {
		fmt.Fprintf(&e.buf, format, args...)
		e.buf.WriteString(" {\n")
	} else {
		e.buf.WriteString("{\n")
	}
	e.indent++
}

// EndBlock closes a braced block: }
func (e *Emitter) EndBlock() {
	e.indent--
	for i := 0; i < e.indent; i++ {
		e.buf.WriteString("  ")
	}
	e.buf.WriteString("}\n")
}

// EndBlockSuffix closes a braced block with a suffix (e.g., "} else {").
func (e *Emitter) EndBlockSuffix(suffix string) {
	e.indent--
	for i := 0; i < e.indent; i++ {
		e.buf.WriteString("  ")
	}
	fmt.Fprintf(&e.buf, "} %s\n", suffix)
}

// String returns the generated JavaScript source code.
func (e *Emitter) String() string {
	return e.buf.String()
}

// Reset clears the emitter buffer.
func (e *Emitter) Reset() {
	e.buf.Reset()
	e.indent = 0
}

// Len returns the current buffer length.
func (e *Emitter) Len() int {
	return e.buf.Len()
}

// ImportStatement emits an ES module import.
func (e *Emitter) ImportStatement(names string, from string) {
	e.Line("import %s from '%s';", names, from)
}

// ImportAll emits import * as X from '...'.
func (e *Emitter) ImportAll(alias string, from string) {
	e.Line("import * as %s from '%s';", alias, from)
}

// ExportFunction emits an exported async or sync function declaration.
func (e *Emitter) ExportFunction(name string, params string, async bool) {
	prefix := ""
	if async {
		prefix = "async "
	}
	e.Block("export %sfunction %s(%s)", prefix, name, params)
}

// Function emits a function declaration.
func (e *Emitter) Function(name string, params string, async bool) {
	prefix := ""
	if async {
		prefix = "async "
	}
	e.Block("%sfunction %s(%s)", prefix, name, params)
}

// JsString returns a properly escaped JavaScript string literal.
func JsString(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'':
			b.WriteString("\\'")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		case '\x00':
			b.WriteString("\\0")
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, "\\x%02x", r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// JsIdentifier converts a Go identifier to a valid JS identifier.
// Go allows unicode identifiers that JS also allows, but we need to handle
// cases like package paths becoming module names.
func JsIdentifier(name string) string {
	name = strings.ReplaceAll(name, "/", "$")
	name = strings.ReplaceAll(name, ".", "$")
	name = strings.ReplaceAll(name, "-", "_")
	if isJsReserved(name) {
		return "$" + name
	}
	return name
}

// isJsReserved checks if a name is a JS reserved word.
func isJsReserved(name string) bool {
	switch name {
	case "break", "case", "catch", "continue", "debugger", "default",
		"delete", "do", "else", "finally", "for", "function", "if",
		"in", "instanceof", "new", "return", "switch", "this",
		"throw", "try", "typeof", "var", "void", "while", "with",
		"class", "const", "enum", "export", "extends", "import",
		"super", "implements", "interface", "let", "package",
		"private", "protected", "public", "static", "yield",
		"await", "async":
		return true
	}
	return false
}

// PackageModuleName converts a Go package import path to a JS module filename.
func PackageModuleName(pkgPath string) string {
	name := strings.ReplaceAll(pkgPath, "/", "_")
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, "-", "_")
	return name + ".mjs"
}
