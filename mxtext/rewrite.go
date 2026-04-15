package mxtext

// Moxie source rewrites.
//
// These transforms run before or during the parse/typecheck pipeline to
// bridge Moxie syntax to what Go's parser and type checker accept.
//
// 1. rewriteChanLiterals: text-level rewrite before parsing.
//    chan T{}  → make(chan T)
//    chan T{N} → make(chan T, N)
//
// 1b. rewriteSliceLiterals: text-level rewrite before parsing.
//    []T{:len}     → make([]T, len)
//    []T{:len:cap} → make([]T, len, cap)
//
// 2. rewriteStringLiterals: AST-level rewrite after parsing, before typecheck.
//    "hello" → []byte("hello")
//    "a" + "b" → []byte("a" + "b")
//
// 3. rewritePipeConcat: AST-level rewrite after first typecheck pass.
//    a | b (where both are []byte) → __moxie_concat(a, b)

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"go/types"
	"strings"
)

// isMoxieStringTarget returns true if a package should get string→[]byte
// rewrites (string literal wrapping, | concat, comparison rewrites).
//
// Permanently exempt packages implement low-level primitives or
// syscall interfaces that require native Go string/uintptr types.
func IsMoxieStringTarget(importPath string) bool {
	if strings.HasPrefix(importPath, "runtime") || // language primitives
		strings.HasPrefix(importPath, "internal/task") || // cooperative scheduler
		strings.HasPrefix(importPath, "internal/abi") || // ABI type descriptors
		strings.HasPrefix(importPath, "internal/reflectlite") || // type reflection
			strings.HasPrefix(importPath, "internal/itoa") || // used by reflectlite, returns string
		strings.HasPrefix(importPath, "syscall") || // syscall interfaces
		strings.HasPrefix(importPath, "internal/syscall") || // syscall internals
		strings.HasPrefix(importPath, "os") || // FDs, syscall wrappers
		strings.HasPrefix(importPath, "unsafe") || // language primitive
		strings.HasPrefix(importPath, "reflect") { // must handle all Go types
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// 1. Channel literal rewrite (text-level, before parsing)
// ---------------------------------------------------------------------------

// rewriteChanLiterals scans source bytes for channel literal syntax and
// rewrites to make(chan T) calls that Go's parser accepts.
//
// Patterns:
//   chan T{}   → make(chan T)
//   chan T{N}  → make(chan T, N)
//
// The rewrite is token-aware: it uses go/scanner to avoid matching inside
// strings or comments. It only rewrites when 'chan' is followed by a type
// expression and then '{' in expression context.
func RewriteChanLiterals(src []byte, fset *token.FileSet) []byte {
	// Tokenize the source.
	type tok struct {
		pos    int
		end    int
		tok    token.Token
		lit    string
		offset int // byte offset in src
	}

	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, nil, scanner.ScanComments)

	var toks []tok
	for {
		pos, t, lit := s.Scan()
		if t == token.EOF {
			break
		}
		offset := file.Offset(pos)
		end := offset + len(lit)
		if lit == "" {
			end = offset + len(t.String())
		}
		toks = append(toks, tok{pos: offset, end: end, tok: t, lit: lit})
	}

	// Scan for pattern: CHAN typeTokens... LBRACE [expr] RBRACE
	// where typeTokens form a valid channel element type.
	var result bytes.Buffer
	lastEnd := 0

	for i := 0; i < len(toks); i++ {
		if toks[i].tok != token.CHAN {
			continue
		}

		// Found 'chan'. Now find the type expression and the '{'.
		// Type expression is everything between 'chan' and '{'.
		// It could be: int32, *Foo, []byte, <-chan int, etc.

		chanIdx := i
		braceIdx := -1

		// Find the opening brace. Track nesting to handle complex types
		// like chan []byte (which contains no braces in the type).
		// Skip tokens that are part of the type expression.
		depth := 0
		for j := i + 1; j < len(toks); j++ {
			switch toks[j].tok {
			case token.LBRACE:
				if depth == 0 {
					braceIdx = j
				}
				depth++
			case token.RBRACE:
				depth--
			case token.LPAREN:
				depth++
			case token.RPAREN:
				depth--
			}
			if braceIdx >= 0 {
				break
			}
			// Stop if we hit something that can't be part of a type expression.
			if toks[j].tok == token.SEMICOLON || toks[j].tok == token.ASSIGN ||
				toks[j].tok == token.DEFINE || toks[j].tok == token.COMMA ||
				toks[j].tok == token.RPAREN {
				break
			}
		}

		if braceIdx < 0 || braceIdx <= chanIdx+1 {
			continue // no brace found, or nothing between chan and {
		}

		// Check this is in expression context by whitelisting tokens that
		// can precede a channel literal. In type contexts (var/func/field
		// declarations), the { is a block/body opener, not a literal.
		inExprContext := false
		if chanIdx > 0 {
			prev := toks[chanIdx-1].tok
			switch prev {
			case token.ASSIGN, token.DEFINE, // x = chan T{}, x := chan T{}
				token.COLON,                  // field: chan T{}
				token.COMMA,                  // f(a, chan T{})
				token.LPAREN,                 // f(chan T{})
				token.LBRACK,                 // []chan T{}
				token.LBRACE,                 // {chan T{}}
				token.RETURN,                 // return chan T{}
				token.SEMICOLON:              // ; chan T{}
				inExprContext = true
			}
		} else {
			inExprContext = true // first token
		}

		if !inExprContext {
			continue
		}

		// Find the matching closing brace.
		closeIdx := -1
		depth = 1
		for j := braceIdx + 1; j < len(toks); j++ {
			switch toks[j].tok {
			case token.LBRACE:
				depth++
			case token.RBRACE:
				depth--
				if depth == 0 {
					closeIdx = j
				}
			}
			if closeIdx >= 0 {
				break
			}
		}

		if closeIdx < 0 {
			continue
		}

		// Extract the type expression text (between chan and {).
		typeStart := toks[chanIdx+1].pos
		typeEnd := toks[braceIdx].pos
		typeText := strings.TrimSpace(string(src[typeStart:typeEnd]))

		if typeText == "" {
			continue
		}

		// Handle chan struct{}{} and chan interface{}{}: the first {} is
		// part of the type, the second {} is the channel literal body.
		if typeText == "struct" || typeText == "interface" {
			// closeIdx points to the } that closes struct{}/interface{}.
			// Look for another {…} pair after it — that's the literal body.
			if closeIdx+1 >= len(toks) || toks[closeIdx+1].tok != token.LBRACE {
				continue // just "chan struct{}" in type context, no literal
			}
			// Include the struct{}/interface{} braces in the type text.
			typeText = typeText + "{}"
			braceIdx = closeIdx + 1
			// Find the matching close for the literal body.
			closeIdx = -1
			depth = 1
			for j := braceIdx + 1; j < len(toks); j++ {
				switch toks[j].tok {
				case token.LBRACE:
					depth++
				case token.RBRACE:
					depth--
					if depth == 0 {
						closeIdx = j
					}
				}
				if closeIdx >= 0 {
					break
				}
			}
			if closeIdx < 0 {
				continue
			}
		}

		// Extract the buffer size expression (between { and }).
		var bufExpr string
		if closeIdx > braceIdx+1 {
			bufStart := toks[braceIdx+1].pos
			bufEnd := toks[closeIdx].pos
			bufExpr = strings.TrimSpace(string(src[bufStart:bufEnd]))
		}

		// Write everything before this channel literal.
		result.Write(src[lastEnd:toks[chanIdx].pos])

		// Write the replacement: (make)(chan T) or (make)(chan T, N).
		// Parenthesized (make) so AST restriction check skips generated calls.
		result.WriteString("(make)(chan ")
		result.WriteString(typeText)
		if bufExpr != "" {
			result.WriteString(", ")
			result.WriteString(bufExpr)
		}
		result.WriteString(")")

		lastEnd = toks[closeIdx].end
		i = closeIdx // skip past the closing brace
	}

	if lastEnd == 0 {
		return src // no rewrites
	}
	result.Write(src[lastEnd:])
	return result.Bytes()
}

// ---------------------------------------------------------------------------
// 1b. Slice size literal rewrite (text-level, before parsing)
// ---------------------------------------------------------------------------

// rewriteSliceLiterals scans source bytes for slice size literal syntax and
// rewrites to make() calls that Go's parser accepts.
//
// Patterns:
//   []T{:len}      → make([]T, len)
//   []T{:len:cap}  → make([]T, len, cap)
//
// The leading colon after { distinguishes this from regular composite literals
// ([]int{1, 2, 3} has no colon). The syntax mirrors Go's three-index slice
// expression a[low:high:max].
func RewriteSliceLiterals(src []byte, fset *token.FileSet) []byte {
	type tok struct {
		pos    int
		end    int
		tok    token.Token
		lit    string
	}

	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, nil, scanner.ScanComments)

	var toks []tok
	for {
		pos, t, lit := s.Scan()
		if t == token.EOF {
			break
		}
		offset := file.Offset(pos)
		end := offset + len(lit)
		if lit == "" {
			end = offset + len(t.String())
		}
		toks = append(toks, tok{pos: offset, end: end, tok: t, lit: lit})
	}

	var result bytes.Buffer
	lastEnd := 0

	for i := 0; i < len(toks); i++ {
		// Look for LBRACK RBRACK ... LBRACE COLON pattern.
		if toks[i].tok != token.LBRACK {
			continue
		}
		if i+1 >= len(toks) || toks[i+1].tok != token.RBRACK {
			continue
		}

		lbrackIdx := i

		// Scan forward past the element type to find LBRACE.
		braceIdx := -1
		depth := 0
		for j := i + 2; j < len(toks); j++ {
			switch toks[j].tok {
			case token.LBRACK:
				depth++
			case token.RBRACK:
				depth--
			case token.LPAREN:
				depth++
			case token.RPAREN:
				depth--
			case token.LBRACE:
				if depth == 0 {
					braceIdx = j
				}
			}
			if braceIdx >= 0 {
				break
			}
			// Stop at tokens that can't be part of a type expression.
			if depth == 0 && (toks[j].tok == token.SEMICOLON ||
				toks[j].tok == token.ASSIGN ||
				toks[j].tok == token.DEFINE ||
				toks[j].tok == token.COMMA) {
				break
			}
		}

		if braceIdx < 0 || braceIdx <= lbrackIdx+2 {
			continue // no brace, or nothing between [] and {
		}

		// Check that the token after { is COLON — this is the discriminator.
		if braceIdx+1 >= len(toks) || toks[braceIdx+1].tok != token.COLON {
			continue // regular composite literal, not slice size
		}

		// Find the closing brace, collecting colon positions for len:cap.
		// Track all bracket types so colons inside subscripts (e.g. buf[:2])
		// aren't mistaken for the len:cap separator.
		closeIdx := -1
		colonPositions := []int{braceIdx + 1} // first colon already found
		depth = 1
		bracketDepth := 0
		parenDepth := 0
		for j := braceIdx + 2; j < len(toks); j++ {
			switch toks[j].tok {
			case token.LBRACE:
				depth++
			case token.RBRACE:
				depth--
				if depth == 0 {
					closeIdx = j
				}
			case token.LBRACK:
				bracketDepth++
			case token.RBRACK:
				bracketDepth--
			case token.LPAREN:
				parenDepth++
			case token.RPAREN:
				parenDepth--
			case token.COLON:
				if depth == 1 && bracketDepth == 0 && parenDepth == 0 {
					colonPositions = append(colonPositions, j)
				}
			}
			if closeIdx >= 0 {
				break
			}
		}

		if closeIdx < 0 {
			continue
		}

		// Extract the type text (between [ and {, inclusive of []).
		typeText := string(src[toks[lbrackIdx].pos:toks[braceIdx].pos])
		typeText = strings.TrimSpace(typeText)

		// Detect the secure-allocator marker: a trailing `, secure` IDENT
		// just before the closing brace. This only applies to the
		// `[]T{:len}` form (no len:cap variant — secure allocations are
		// page-aligned and have an implicit cap).
		secureMarker := false
		secureExprEnd := closeIdx
		if len(colonPositions) == 1 && closeIdx-2 > colonPositions[0] {
			lastTok := toks[closeIdx-1]
			prevTok := toks[closeIdx-2]
			if lastTok.tok == token.IDENT && lastTok.lit == "secure" &&
				prevTok.tok == token.COMMA {
				secureMarker = true
				secureExprEnd = closeIdx - 2 // index of the COMMA
			}
		}

		if secureMarker {
			// []byte{:len, secure} → __moxie_secalloc(len)
			// Only []byte is supported in M2; other element types fall
			// through to the regular make path and trip a typecheck error
			// downstream because the resulting __moxie_secalloc returns
			// []byte.
			if typeText != "[]byte" {
				continue
			}
			lenStart := toks[colonPositions[0]+1].pos
			lenEnd := toks[secureExprEnd].pos
			lenExpr := strings.TrimSpace(string(src[lenStart:lenEnd]))
			if lenExpr == "" {
				continue
			}
			result.Write(src[lastEnd:toks[lbrackIdx].pos])
			result.WriteString("__moxie_secalloc(")
			result.WriteString(lenExpr)
			result.WriteString(")")
		} else if len(colonPositions) == 1 {
			// []T{:len} → make([]T, len)
			lenStart := toks[colonPositions[0]+1].pos
			lenEnd := toks[closeIdx].pos
			lenExpr := strings.TrimSpace(string(src[lenStart:lenEnd]))
			if lenExpr == "" {
				continue
			}

			result.Write(src[lastEnd:toks[lbrackIdx].pos])
			result.WriteString("(make)(")
			result.WriteString(typeText)
			result.WriteString(", ")
			result.WriteString(lenExpr)
			result.WriteString(")")
		} else if len(colonPositions) == 2 {
			// []T{:len:cap} → make([]T, len, cap)
			// len is between first colon and second colon
			lenStart := toks[colonPositions[0]+1].pos
			lenEnd := toks[colonPositions[1]].pos
			lenExpr := strings.TrimSpace(string(src[lenStart:lenEnd]))

			// cap is between second colon and closing brace
			capStart := toks[colonPositions[1]+1].pos
			capEnd := toks[closeIdx].pos
			capExpr := strings.TrimSpace(string(src[capStart:capEnd]))

			if lenExpr == "" || capExpr == "" {
				continue
			}

			result.Write(src[lastEnd:toks[lbrackIdx].pos])
			result.WriteString("(make)(")
			result.WriteString(typeText)
			result.WriteString(", ")
			result.WriteString(lenExpr)
			result.WriteString(", ")
			result.WriteString(capExpr)
			result.WriteString(")")
		} else {
			continue // malformed — more than 2 colons
		}

		lastEnd = toks[closeIdx].end
		i = closeIdx
	}

	if lastEnd == 0 {
		return src // no rewrites
	}
	result.Write(src[lastEnd:])
	return result.Bytes()
}

// ---------------------------------------------------------------------------
// 1c. String type annotation rewrite (AST-level, before typecheck)
// ---------------------------------------------------------------------------

// RewriteStringTypes converts `string` type identifiers to `[]byte` in all
// type positions: function params, returns, struct fields, var declarations,
// type specs, map keys/values, slice elements, and interface method signatures.
// This is the AST-level equivalent of what mxpurify does to source files,
// needed by moxiejs since it uses the standard Go type checker (which treats
// string and []byte as distinct types).
func RewriteStringTypes(file *ast.File) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			// Interface-mandated methods (Error() string, String() string) must
			// keep their `string` return type — they're bound to language-level
			// interfaces (error, fmt.Stringer) we can't rewrite. Their return
			// wrapping is deferred to WrapInterfaceMandatedReturns so it runs
			// AFTER RewriteStringConversions (which would otherwise undo the
			// string(...) wrap by converting it to []byte(...)).
			if isInterfaceMandatedMethod(node) {
				return false
			}
			rewriteFieldListTypes(node.Type.Params)
			rewriteFieldListTypes(node.Type.Results)
		case *ast.FuncLit:
			rewriteFieldListTypes(node.Type.Params)
			rewriteFieldListTypes(node.Type.Results)
		case *ast.FuncType:
			rewriteFieldListTypes(node.Params)
			rewriteFieldListTypes(node.Results)
		case *ast.Field:
			node.Type = rewriteStringTypeExpr(node.Type)
		case *ast.ValueSpec:
			if node.Type != nil {
				node.Type = rewriteStringTypeExpr(node.Type)
			}
		case *ast.TypeSpec:
			// `type X string` must stay as defined string type so
			// `const c X = "..."` remains legal. Rewriting to []byte
			// makes the const invalid (slice types can't be const).
			if id, ok := node.Type.(*ast.Ident); ok && id.Name == "string" {
				return false
			}
			node.Type = rewriteStringTypeExpr(node.Type)
		case *ast.ArrayType:
			node.Elt = rewriteStringTypeExpr(node.Elt)
		case *ast.MapType:
			// Do NOT rewrite map keys — []byte is not comparable in Go's type
			// system, so map[[]byte]V would be rejected. Leave key as string
			// (valid map key). The string/[]byte mismatch filter handles any
			// residual type errors from key lookups.
			node.Value = rewriteStringTypeExpr(node.Value)
		}
		return true
	})
}

func rewriteFieldListTypes(fl *ast.FieldList) {
	if fl == nil {
		return
	}
	for _, field := range fl.List {
		field.Type = rewriteStringTypeExpr(field.Type)
	}
}

func rewriteStringTypeExpr(expr ast.Expr) ast.Expr {
	ident, ok := expr.(*ast.Ident)
	if !ok || ident.Name != "string" {
		return expr
	}
	// Replace `string` with `[]byte`.
	return &ast.ArrayType{
		Elt: ast.NewIdent("byte"),
	}
}

// WrapInterfaceMandatedReturns wraps return statements inside interface-
// mandated methods (Error, String) with string(...) conversions. Must run
// AFTER RewriteStringConversions (which rewrites string→[]byte everywhere)
// so the wraps this function introduces are preserved.
func WrapInterfaceMandatedReturns(file *ast.File) {
	ast.Inspect(file, func(n ast.Node) bool {
		fd, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		if !isInterfaceMandatedMethod(fd) {
			return true
		}
		wrapReturnsInStringConv(fd.Body)
		return false
	})
}

// wrapReturnsInStringConv wraps every return-statement value in an explicit
// string(...) conversion. Used for interface-mandated methods that keep their
// string return type while the receiver fields have been rewritten to []byte.
func wrapReturnsInStringConv(body *ast.BlockStmt) {
	if body == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for i, r := range ret.Results {
			// Skip already-wrapped string(...) calls.
			if call, ok := r.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "string" {
					continue
				}
			}
			ret.Results[i] = &ast.CallExpr{
				Fun:  ast.NewIdent("string"),
				Args: []ast.Expr{r},
			}
		}
		return true
	})
}

// isInterfaceMandatedMethod returns true if the function is a method whose
// signature is mandated by a language built-in interface we can't rewrite:
//   - Error() string (error interface)
//   - String() string (fmt.Stringer, used by print formatting)
//
// These methods must keep their `string` return type; their bodies are also
// skipped by RewriteStringLiterals via funcReturnsString.
func isInterfaceMandatedMethod(fd *ast.FuncDecl) bool {
	if fd.Recv == nil || len(fd.Recv.List) != 1 {
		return false
	}
	if fd.Name == nil {
		return false
	}
	name := fd.Name.Name
	if name != "Error" && name != "String" {
		return false
	}
	// Must take no params and return exactly one string value.
	if fd.Type.Params != nil && len(fd.Type.Params.List) != 0 {
		return false
	}
	if fd.Type.Results == nil || len(fd.Type.Results.List) != 1 {
		return false
	}
	field := fd.Type.Results.List[0]
	if len(field.Names) != 0 {
		// Named return — still check the type.
	}
	ident, ok := field.Type.(*ast.Ident)
	return ok && ident.Name == "string"
}

// RewriteStringConversions rewrites `string(expr)` → `[]byte(expr)` in the
// AST. Since Moxie unifies string and []byte, these conversions are identity
// but the Go type checker rejects returning a string where []byte is expected.
// Must run after RewriteStringTypes so return types are already []byte.
func RewriteStringConversions(file *ast.File) {
	// Interface-mandated methods (Error/String) have return values wrapped
	// in `string(...)` by wrapReturnsInStringConv before this runs. We can
	// safely rewrite inner string(x) → []byte(x) throughout: the outer
	// wrap handles the string return-type requirement.
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "string" {
			return true
		}
		// string(x) → []byte(x)
		call.Fun = &ast.ArrayType{Elt: ast.NewIdent("byte")}
		return true
	})
}

// RewriteUnsafeString rewrites `unsafe.String(ptr, len)` → `unsafe.Slice(ptr,
// len)` in the AST. In stock Go the two functions differ in return type
// (`string` vs `[]T`), but in Moxie string and []byte are the same type so the
// swap is identity for byte-pointer callers. Every unsafe.String use in the
// stdlib passes a *byte (either `&b[0]` or `(*byte)(...)` cast), so the
// rewrite is universally safe for Moxie-target packages. Must run before
// typecheck so the return-type mismatch errors never get produced.
func RewriteUnsafeString(file *ast.File) {
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "unsafe" {
			return true
		}
		if sel.Sel.Name != "String" {
			return true
		}
		sel.Sel = ast.NewIdent("Slice")
		return true
	})
}

// ---------------------------------------------------------------------------
// 2. String literal rewrite (AST-level, after parsing, before typecheck)
// ---------------------------------------------------------------------------

// rewriteStringLiterals wraps string literals and string binary expressions
// in []byte() conversions throughout the AST of a user package.
//
// "hello"       → []byte("hello")
// "a" + "b"     → []byte("a" + "b")
//
// This makes Go's type checker see []byte instead of string for all text
// values in user code.
func RewriteStringLiterals(file *ast.File) {
	// Rewrite "X == \"\"" and "X != \"\"" to "len(X) == 0" and "len(X) != 0"
	// BEFORE wrapping literals. After RewriteStringTypes turns the LHS into
	// []byte, a slice == []byte("") would be rejected (slices only compare to
	// nil). len-based check is the correct semantic replacement.
	rewriteEmptyStringComparisons(file)
	// Split mixed-type const blocks so non-string specs stay untyped.
	splitConstBlocks(file)
	// Walk the AST and replace string expressions with []byte() wrapped versions.
	// We need to walk parent nodes to replace children in-place.
	rewriteStringExprs(file)
}

// rewriteEmptyStringComparisons converts `X == ""` → `len(X) == 0` and
// `X != ""` → `len(X) != 0` in the AST. Must run before RewriteStringLiterals'
// literal wrapping, since wrapping turns "" into []byte("") which then makes
// the comparison illegal (slice vs slice).
func rewriteEmptyStringComparisons(file *ast.File) {
	replace := func(expr *ast.Expr) {
		be, ok := (*expr).(*ast.BinaryExpr)
		if !ok {
			return
		}
		if be.Op != token.EQL && be.Op != token.NEQ {
			return
		}
		// Detect `X == ""` or `"" == X`.
		var other ast.Expr
		if isEmptyStringLit(be.Y) {
			other = be.X
		} else if isEmptyStringLit(be.X) {
			other = be.Y
		} else {
			return
		}
		// Replace with len(other) OP 0
		lenCall := &ast.CallExpr{
			Fun:  ast.NewIdent("len"),
			Args: []ast.Expr{other},
		}
		*expr = &ast.BinaryExpr{
			X:  lenCall,
			Op: be.Op,
			Y:  &ast.BasicLit{Kind: token.INT, Value: "0"},
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.IfStmt:
			replace(&node.Cond)
		case *ast.ForStmt:
			if node.Cond != nil {
				replace(&node.Cond)
			}
		case *ast.BinaryExpr:
			replace(&node.X)
			replace(&node.Y)
		case *ast.AssignStmt:
			for i := range node.Rhs {
				replace(&node.Rhs[i])
			}
		case *ast.ReturnStmt:
			for i := range node.Results {
				replace(&node.Results[i])
			}
		case *ast.CallExpr:
			for i := range node.Args {
				replace(&node.Args[i])
			}
		case *ast.UnaryExpr:
			replace(&node.X)
		case *ast.ParenExpr:
			replace(&node.X)
		case *ast.SwitchStmt:
			if node.Tag != nil {
				replace(&node.Tag)
			}
		}
		return true
	})
}

func isEmptyStringLit(expr ast.Expr) bool {
	bl, ok := expr.(*ast.BasicLit)
	if !ok {
		return false
	}
	return bl.Kind == token.STRING && (bl.Value == `""` || bl.Value == "``")
}

// rewriteStringExprs walks the AST and wraps string-typed expressions in []byte().
func rewriteStringExprs(node ast.Node) {
	ast.Inspect(node, func(n ast.Node) bool {
		// Don't descend into []byte() wrappers we created — prevents
		// infinite recursion (walker would visit the inner string literal
		// and try to wrap it again).
		if expr, ok := n.(ast.Expr); ok && isSliceByteConversion(expr) {
			return false
		}
		// Const blocks are handled at file-scope by splitConstBlocks so
		// mixed string/non-string blocks can be split into a preserved
		// const (for untyped integer constants) and a companion var.
		if gd, ok := n.(*ast.GenDecl); ok && gd.Tok == token.CONST {
			return false
		}
		// Interface-mandated methods (Error/String) keep their string
		// return type, but RewriteStringTypes has already wrapped every
		// return value in `string(...)`. So we CAN wrap inner string
		// literals here — e.g. `return "strconv." | e.Func` becomes
		// `return string([]byte("strconv.") + e.Func + ...)`, which
		// RewriteTextConcat then lowers to __moxie_concat. The outer
		// string() conversion takes the []byte result back to string.
		switch parent := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range parent.Rhs {
				if wrapped := wrapStringExpr(rhs); wrapped != nil {
					parent.Rhs[i] = wrapped
				}
			}
		case *ast.ValueSpec:
			for i, val := range parent.Values {
				if wrapped := wrapStringExpr(val); wrapped != nil {
					parent.Values[i] = wrapped
				}
			}
		case *ast.ReturnStmt:
			for i, result := range parent.Results {
				if wrapped := wrapStringExpr(result); wrapped != nil {
					parent.Results[i] = wrapped
				}
			}
		case *ast.CallExpr:
			// Skip wrapping args to calls on exempt packages
			// (e.g. os.Open("file") — os is exempt, expects string).
			if !isExemptPackageCall(parent) {
				for i, arg := range parent.Args {
					if wrapped := wrapStringExpr(arg); wrapped != nil {
						parent.Args[i] = wrapped
					}
				}
			}
		case *ast.SendStmt:
			if wrapped := wrapStringExpr(parent.Value); wrapped != nil {
				parent.Value = wrapped
			}
		case *ast.KeyValueExpr:
			if wrapped := wrapStringExpr(parent.Value); wrapped != nil {
				parent.Value = wrapped
			}
		case *ast.BinaryExpr:
			// Wrap string literals on either side of comparison operators.
			if wrapped := wrapStringExpr(parent.X); wrapped != nil {
				parent.X = wrapped
			}
			if wrapped := wrapStringExpr(parent.Y); wrapped != nil {
				parent.Y = wrapped
			}
		case *ast.CaseClause:
			// Wrap string literals in switch case values.
			for i, val := range parent.List {
				if wrapped := wrapStringExpr(val); wrapped != nil {
					parent.List[i] = wrapped
				}
			}
		case *ast.CompositeLit:
			for i, elt := range parent.Elts {
				// Skip KeyValueExpr — handled above for values.
				if _, isKV := elt.(*ast.KeyValueExpr); isKV {
					continue
				}
				if wrapped := wrapStringExpr(elt); wrapped != nil {
					parent.Elts[i] = wrapped
				}
			}
		case *ast.IndexExpr:
			if wrapped := wrapStringExpr(parent.Index); wrapped != nil {
				parent.Index = wrapped
			}
		case *ast.IfStmt:
			// Wrap in if-init statements (e.g. if x := "val"; ...).
			// Cond is a BinaryExpr, handled above.
		case *ast.SwitchStmt:
			// Wrap switch tag if it's a string literal.
			if parent.Tag != nil {
				if wrapped := wrapStringExpr(parent.Tag); wrapped != nil {
					parent.Tag = wrapped
				}
			}
		}
		return true
	})
}

// wrapStringExpr returns a []byte(expr) wrapping if expr is a string-producing
// expression (string literal or binary + of string expressions). Returns nil
// if no wrapping is needed.
func wrapStringExpr(expr ast.Expr) ast.Expr {
	if !isStringExpr(expr) {
		return nil
	}
	// Already wrapped in []byte() — don't double-wrap.
	if isSliceByteConversion(expr) {
		return nil
	}
	return makeSliceByteCall(expr)
}

// isStringExpr returns true if the expression is syntactically a string literal
// or a binary +/| chain of string expressions (constant string concatenation).
func isStringExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.BinaryExpr:
		if e.Op == token.ADD || e.Op == token.OR {
			return isStringExpr(e.X) && isStringExpr(e.Y)
		}
	case *ast.ParenExpr:
		return isStringExpr(e.X)
	}
	return false
}

// isSliceByteConversion returns true if expr is []byte(...).
func isSliceByteConversion(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	arr, ok := call.Fun.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	ident, ok := arr.Elt.(*ast.Ident)
	return ok && ident.Name == "byte"
}

// convertStringConstsToVars converts pure string constants to var declarations
// with []byte values. Only converts specs where ALL values are string literals
// and there's no explicit type or iota. Leaves numeric/mixed consts untouched.
// splitConstBlocks walks the file's top-level declarations AND function
// bodies, splitting each const block that mixes string and non-string
// specs into a const block (non-string) and a var block (string, with
// literals wrapped in []byte). Keeping the non-string specs as const
// preserves their untyped-ness so comparisons like `rune >= runeSelf`
// still typecheck.
func splitConstBlocks(file *ast.File) {
	splitBlockStmtConsts(file)
	var newDecls []ast.Decl
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			newDecls = append(newDecls, decl)
			continue
		}
		hasString := false
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if vs.Type != nil {
				continue
			}
			for _, val := range vs.Values {
				if isStringExpr(val) {
					hasString = true
					break
				}
			}
			if hasString {
				break
			}
		}
		if !hasString {
			newDecls = append(newDecls, decl)
			continue
		}
		var varSpecs []ast.Spec
		var constSpecs []ast.Spec
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				constSpecs = append(constSpecs, spec)
				continue
			}
			allString := vs.Type == nil && len(vs.Values) > 0
			if allString {
				for _, val := range vs.Values {
					if !isStringExpr(val) {
						allString = false
						break
					}
				}
			}
			if !allString {
				constSpecs = append(constSpecs, vs)
				continue
			}
			for i, val := range vs.Values {
				if wrapped := wrapStringExpr(val); wrapped != nil {
					vs.Values[i] = wrapped
				}
			}
			for _, name := range vs.Names {
				if name.Obj != nil {
					name.Obj.Kind = ast.Var
				}
			}
			varSpecs = append(varSpecs, vs)
		}
		if len(varSpecs) == 0 {
			newDecls = append(newDecls, decl)
			continue
		}
		if len(constSpecs) == 0 {
			gd.Tok = token.VAR
			gd.Specs = varSpecs
			newDecls = append(newDecls, gd)
			continue
		}
		// Mixed: emit a const block with non-string specs, then a var
		// block with string specs.
		gd.Specs = constSpecs
		newDecls = append(newDecls, gd)
		newDecls = append(newDecls, &ast.GenDecl{
			Tok:   token.VAR,
			Specs: varSpecs,
		})
	}
	file.Decls = newDecls
}

// splitBlockStmtConsts walks function bodies and splits function-scoped
// const blocks the same way splitConstBlocks splits top-level const blocks.
// Function-scoped consts appear as DeclStmt{Decl:&GenDecl{Tok:CONST}}.
func splitBlockStmtConsts(file *ast.File) {
	ast.Inspect(file, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		var newStmts []ast.Stmt
		for _, stmt := range block.List {
			ds, ok := stmt.(*ast.DeclStmt)
			if !ok {
				newStmts = append(newStmts, stmt)
				continue
			}
			gd, ok := ds.Decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				newStmts = append(newStmts, stmt)
				continue
			}
			hasString := false
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if vs.Type != nil {
					continue
				}
				for _, val := range vs.Values {
					if isStringExpr(val) {
						hasString = true
						break
					}
				}
				if hasString {
					break
				}
			}
			if !hasString {
				newStmts = append(newStmts, stmt)
				continue
			}
			var varSpecs []ast.Spec
			var constSpecs []ast.Spec
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					constSpecs = append(constSpecs, spec)
					continue
				}
				allString := vs.Type == nil && len(vs.Values) > 0
				if allString {
					for _, val := range vs.Values {
						if !isStringExpr(val) {
							allString = false
							break
						}
					}
				}
				if !allString {
					constSpecs = append(constSpecs, vs)
					continue
				}
				for i, val := range vs.Values {
					if wrapped := wrapStringExpr(val); wrapped != nil {
						vs.Values[i] = wrapped
					}
				}
				for _, name := range vs.Names {
					if name.Obj != nil {
						name.Obj.Kind = ast.Var
					}
				}
				varSpecs = append(varSpecs, vs)
			}
			if len(varSpecs) == 0 {
				newStmts = append(newStmts, stmt)
				continue
			}
			if len(constSpecs) == 0 {
				gd.Tok = token.VAR
				gd.Specs = varSpecs
				newStmts = append(newStmts, stmt)
				continue
			}
			gd.Specs = constSpecs
			newStmts = append(newStmts, stmt)
			newStmts = append(newStmts, &ast.DeclStmt{
				Decl: &ast.GenDecl{
					Tok:   token.VAR,
					Specs: varSpecs,
				},
			})
		}
		block.List = newStmts
		return true
	})
}

// funcReturnsString returns true if a FuncDecl has string in its return types.
// isExemptPackageCall returns true if a call expression targets a function
// from a package exempt from string rewrites (e.g. os.Open, errors.New before
// conversion). These calls expect string parameters, not []byte.
func isExemptPackageCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return !IsMoxieStringTarget(ident.Name)
}

func funcReturnsString(fd *ast.FuncDecl) bool {
	return funcTypeReturnsString(fd.Type)
}

// funcTypeReturnsString returns true if a FuncType has string in its return types.
func funcTypeReturnsString(ft *ast.FuncType) bool {
	if ft.Results == nil {
		return false
	}
	for _, field := range ft.Results.List {
		if ident, ok := field.Type.(*ast.Ident); ok && ident.Name == "string" {
			return true
		}
	}
	return false
}

// makeSliceByteCall creates an AST node for []byte(expr).
func makeSliceByteCall(expr ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{
		Fun: &ast.ArrayType{
			Elt: &ast.Ident{Name: "byte"},
		},
		Args: []ast.Expr{expr},
	}
}

// FindExemptCrossBoundaryMismatches walks the AST of an exempt package and
// returns a list of argument expressions that need to be wrapped in
// []byte(...) to match the callee's rewritten []byte signature. Requires
// type info from a prior typecheck pass to inspect callee param types and
// caller arg types.
//
// Pattern: pkg.Func(x) where pkg is non-exempt, the Func's param is []byte,
// and x is of type string.
func FindExemptCrossBoundaryMismatches(files []*ast.File, info *types.Info) []ast.Expr {
	var result []ast.Expr
	for _, file := range files {
		imports := map[string]bool{}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, "\"")
			if imp.Name != nil {
				imports[imp.Name.Name] = true
				continue
			}
			name := path
			if i := strings.LastIndex(path, "/"); i >= 0 {
				name = path[i+1:]
			}
			imports[name] = true
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if !imports[pkgIdent.Name] {
				return true
			}
			if !IsMoxieStringTarget(pkgIdent.Name) {
				return true
			}
			// Get callee signature.
			tv, ok := info.Types[sel]
			if !ok {
				return true
			}
			sig, ok := tv.Type.(*types.Signature)
			if !ok {
				return true
			}
			params := sig.Params()
			for i, arg := range call.Args {
				if i >= params.Len() {
					break // variadic overflow
				}
				paramType := params.At(i).Type()
				// Only interested when param is []byte.
				slice, ok := paramType.(*types.Slice)
				if !ok {
					continue
				}
				basic, ok := slice.Elem().(*types.Basic)
				if !ok || basic.Kind() != types.Byte {
					continue
				}
				// Check arg's type: string means mismatch.
				argTV, ok := info.Types[arg]
				if !ok {
					continue
				}
				argBasic, ok := argTV.Type.(*types.Basic)
				if !ok {
					continue
				}
				if argBasic.Kind() == types.String || argBasic.Kind() == types.UntypedString {
					// Already wrapped in []byte(...)?
					if isSliceByteConversion(arg) {
						continue
					}
					result = append(result, arg)
				}
			}
			return true
		})
	}
	return result
}

// ApplyExemptCrossBoundaryMismatches wraps each identified arg expression
// in []byte(...). Identifies args by pointer equality — must be called
// with the exact nodes returned by FindExemptCrossBoundaryMismatches.
func ApplyExemptCrossBoundaryMismatches(files []*ast.File, exprs []ast.Expr) {
	if len(exprs) == 0 {
		return
	}
	targets := map[ast.Expr]bool{}
	for _, e := range exprs {
		targets[e] = true
	}
	// Remove from targets once wrapped to prevent infinite revisits.
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			for i, arg := range call.Args {
				if targets[arg] {
					call.Args[i] = makeSliceByteCall(arg)
					delete(targets, arg)
				}
			}
			return true
		})
	}
}

// makeStringCall wraps an expression in `string(...)`.
func makeStringCall(expr ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{
		Fun:  &ast.Ident{Name: "string"},
		Args: []ast.Expr{expr},
	}
}

// FindExemptStructLiteralMismatches walks the AST of an exempt package and
// returns value expressions in struct literals whose field type is []byte
// but the supplied value is string-typed (typically a literal). These need
// wrapping in []byte(...) so stock go/types accepts them.
//
// Pattern: &PathError{Op: "readdir unimplemented"} inside os where PathError
// comes from io/fs (non-exempt) and its Op field was rewritten to []byte.
func FindExemptStructLiteralMismatches(files []*ast.File, info *types.Info) []ast.Expr {
	var result []ast.Expr
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			tv, ok := info.Types[cl]
			if !ok {
				return true
			}
			t := tv.Type
			for {
				p, ok := t.(*types.Pointer)
				if !ok {
					break
				}
				t = p.Elem()
			}
			if t == nil {
				return true
			}
			st, ok := t.Underlying().(*types.Struct)
			if !ok {
				return true
			}
			for i, elt := range cl.Elts {
				var fieldType types.Type
				var value ast.Expr
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					keyIdent, ok := kv.Key.(*ast.Ident)
					if !ok {
						continue
					}
					for j := 0; j < st.NumFields(); j++ {
						if st.Field(j).Name() == keyIdent.Name {
							fieldType = st.Field(j).Type()
							break
						}
					}
					value = kv.Value
				} else {
					if i >= st.NumFields() {
						continue
					}
					fieldType = st.Field(i).Type()
					value = elt
				}
				if fieldType == nil {
					continue
				}
				slice, ok := fieldType.(*types.Slice)
				if !ok {
					continue
				}
				basic, ok := slice.Elem().(*types.Basic)
				if !ok || basic.Kind() != types.Byte {
					continue
				}
				vTV, ok := info.Types[value]
				if !ok {
					continue
				}
				vBasic, ok := vTV.Type.(*types.Basic)
				if !ok {
					continue
				}
				if vBasic.Kind() == types.String || vBasic.Kind() == types.UntypedString {
					if isSliceByteConversion(value) {
						continue
					}
					result = append(result, value)
				}
			}
			return true
		})
	}
	return result
}

// FindNonExemptReturnMismatches scans non-exempt package files for return
// statements where the enclosing function's result type is []byte but the
// returned expression is string-typed (typically from an interface-mandated
// String() method that kept its string return). These need wrapping in
// []byte(...).
func FindNonExemptReturnMismatches(files []*ast.File, info *types.Info) []ast.Expr {
	var result []ast.Expr
	walk := func(sig *types.Signature, body *ast.BlockStmt) {
		if sig == nil || body == nil {
			return
		}
		results := sig.Results()
		if results.Len() == 0 {
			return
		}
		ast.Inspect(body, func(n ast.Node) bool {
			// Don't descend into nested FuncLit — their returns bind to
			// their own signature, handled separately.
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			ret, ok := n.(*ast.ReturnStmt)
			if !ok {
				return true
			}
			for i, expr := range ret.Results {
				if i >= results.Len() {
					break
				}
				rt := results.At(i).Type()
				slice, ok := rt.(*types.Slice)
				if !ok {
					continue
				}
				basic, ok := slice.Elem().(*types.Basic)
				if !ok || basic.Kind() != types.Byte {
					continue
				}
				eTV, ok := info.Types[expr]
				if !ok {
					continue
				}
				eBasic, ok := eTV.Type.(*types.Basic)
				if !ok {
					continue
				}
				if eBasic.Kind() == types.String || eBasic.Kind() == types.UntypedString {
					if isSliceByteConversion(expr) {
						continue
					}
					result = append(result, expr)
				}
			}
			return true
		})
	}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if fn.Body == nil {
					return true
				}
				obj := info.Defs[fn.Name]
				if obj == nil {
					return true
				}
				sig, _ := obj.Type().(*types.Signature)
				walk(sig, fn.Body)
			case *ast.FuncLit:
				tv, ok := info.Types[fn]
				if !ok {
					return true
				}
				sig, _ := tv.Type.(*types.Signature)
				walk(sig, fn.Body)
			}
			return true
		})
	}
	return result
}

// ApplyNonExemptReturnMismatches wraps each identified return-expr in []byte(...).
func ApplyNonExemptReturnMismatches(files []*ast.File, exprs []ast.Expr) {
	if len(exprs) == 0 {
		return
	}
	targets := map[ast.Expr]bool{}
	for _, e := range exprs {
		targets[e] = true
	}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok {
				return true
			}
			for i, expr := range ret.Results {
				if targets[expr] {
					ret.Results[i] = makeSliceByteCall(expr)
					delete(targets, expr)
				}
			}
			return true
		})
	}
}

// AssignMismatch carries an assignment-RHS expression plus the direction of
// the wrap needed: "toBytes" wraps in []byte(...), "toString" wraps in string(...).
type AssignMismatch struct {
	Expr ast.Expr
	Kind string
}

// FindNonExemptAssignMismatches scans for `a = b` or `a, b = c, d` assigns
// where the LHS and RHS straddle the string/[]byte boundary. Returns a list
// of fixes keyed by RHS expression pointer.
func FindNonExemptAssignMismatches(files []*ast.File, info *types.Info) []AssignMismatch {
	var result []AssignMismatch
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			if assign.Tok != token.ASSIGN {
				return true
			}
			if len(assign.Lhs) != len(assign.Rhs) {
				return true
			}
			for i, lhs := range assign.Lhs {
				lTV, ok := info.Types[lhs]
				if !ok {
					continue
				}
				rhs := assign.Rhs[i]
				rTV, ok := info.Types[rhs]
				if !ok {
					continue
				}
				// LHS []byte, RHS string → wrap in []byte(...).
				if lSlice, ok := lTV.Type.(*types.Slice); ok {
					if lb, ok := lSlice.Elem().(*types.Basic); ok && lb.Kind() == types.Byte {
						if rBasic, ok := rTV.Type.(*types.Basic); ok && (rBasic.Kind() == types.String || rBasic.Kind() == types.UntypedString) {
							if !isSliceByteConversion(rhs) {
								result = append(result, AssignMismatch{Expr: rhs, Kind: "toBytes"})
							}
							continue
						}
					}
				}
				// LHS string, RHS []byte → wrap in string(...).
				if lBasic, ok := lTV.Type.(*types.Basic); ok && lBasic.Kind() == types.String {
					if rSlice, ok := rTV.Type.(*types.Slice); ok {
						if rb, ok := rSlice.Elem().(*types.Basic); ok && rb.Kind() == types.Byte {
							if !isStringConversion(rhs) {
								result = append(result, AssignMismatch{Expr: rhs, Kind: "toString"})
							}
						}
					}
				}
			}
			return true
		})
	}
	return result
}

// ApplyNonExemptAssignMismatches wraps each identified RHS per its Kind.
func ApplyNonExemptAssignMismatches(files []*ast.File, fixes []AssignMismatch) {
	if len(fixes) == 0 {
		return
	}
	targets := map[ast.Expr]string{}
	for _, f := range fixes {
		targets[f.Expr] = f.Kind
	}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, rhs := range assign.Rhs {
				if kind, ok := targets[rhs]; ok {
					switch kind {
					case "toBytes":
						assign.Rhs[i] = makeSliceByteCall(rhs)
					case "toString":
						assign.Rhs[i] = makeStringCall(rhs)
					}
					delete(targets, rhs)
				}
			}
			return true
		})
	}
}

// isStringConversion reports whether expr is already a `string(x)` call.
func isStringConversion(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "string"
}

// ByteConvFix describes a rewrite to apply to a `[]byte(x)` CallExpr that
// was produced by RewriteStringConversions from a non-slice arg.
//
//	Kind "compLit": []byte(x) → []byte{x} (for byte/untypedInt args).
//	Kind "revert":  []byte(x) → string(x) (for rune/int32/int args; phase 2
//	                wrapping will re-wrap when flowing into []byte contexts).
type ByteConvFix struct {
	Call *ast.CallExpr
	Kind string
}

// FindByteToSliceConversions scans for `[]byte(x)` calls where `x` is not a
// slice or string. These arise from the aggressive `string(x)` → `[]byte(x)`
// rewrite in RewriteStringConversions, which is correct for slice args but
// breaks for single-byte/rune args.
func FindByteToSliceConversions(files []*ast.File, info *types.Info) []ByteConvFix {
	var result []ByteConvFix
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			at, ok := call.Fun.(*ast.ArrayType)
			if !ok || at.Len != nil {
				return true
			}
			elt, ok := at.Elt.(*ast.Ident)
			if !ok || elt.Name != "byte" {
				return true
			}
			argTV, ok := info.Types[call.Args[0]]
			if !ok {
				return true
			}
			basic, ok := argTV.Type.(*types.Basic)
			if !ok {
				return true
			}
			switch basic.Kind() {
			case types.Byte, types.UntypedInt:
				// byte (uint8): []byte{x} is exact single-byte slice.
				result = append(result, ByteConvFix{Call: call, Kind: "compLit"})
			case types.Int32, types.Int, types.UntypedRune:
				// rune: revert to string(x) so UTF-8 semantics kick in.
				// Phase 2 wrapping handles the []byte context.
				result = append(result, ByteConvFix{Call: call, Kind: "revert"})
			}
			return true
		})
	}
	return result
}

// ApplyByteToSliceConversions walks files and applies each ByteConvFix.
func ApplyByteToSliceConversions(files []*ast.File, fixes []ByteConvFix) {
	if len(fixes) == 0 {
		return
	}
	targets := map[*ast.CallExpr]string{}
	for _, f := range fixes {
		targets[f.Call] = f.Kind
	}
	replace := func(e ast.Expr) ast.Expr {
		ce, ok := e.(*ast.CallExpr)
		if !ok {
			return e
		}
		kind, ok := targets[ce]
		if !ok {
			return e
		}
		switch kind {
		case "compLit":
			return &ast.CompositeLit{
				Type: &ast.ArrayType{Elt: ast.NewIdent("byte")},
				Elts: []ast.Expr{ce.Args[0]},
			}
		case "revert":
			// rune → UTF-8 bytes: []byte(string(rune)).
			// Stock go/types accepts: string(rune) → string (UTF-8),
			// []byte(string) → []byte.
			return &ast.CallExpr{
				Fun: &ast.ArrayType{Elt: ast.NewIdent("byte")},
				Args: []ast.Expr{
					&ast.CallExpr{
						Fun:  ast.NewIdent("string"),
						Args: []ast.Expr{ce.Args[0]},
					},
				},
			}
		}
		return e
	}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.AssignStmt:
				for i, r := range v.Rhs {
					v.Rhs[i] = replace(r)
				}
			case *ast.ValueSpec:
				for i, r := range v.Values {
					v.Values[i] = replace(r)
				}
			case *ast.ReturnStmt:
				for i, r := range v.Results {
					v.Results[i] = replace(r)
				}
			case *ast.CallExpr:
				for i, a := range v.Args {
					v.Args[i] = replace(a)
				}
			case *ast.KeyValueExpr:
				v.Value = replace(v.Value)
			case *ast.BinaryExpr:
				v.X = replace(v.X)
				v.Y = replace(v.Y)
			case *ast.UnaryExpr:
				v.X = replace(v.X)
			case *ast.ParenExpr:
				v.X = replace(v.X)
			case *ast.IndexExpr:
				v.X = replace(v.X)
				v.Index = replace(v.Index)
			case *ast.SliceExpr:
				v.X = replace(v.X)
				if v.Low != nil {
					v.Low = replace(v.Low)
				}
				if v.High != nil {
					v.High = replace(v.High)
				}
				if v.Max != nil {
					v.Max = replace(v.Max)
				}
			case *ast.CompositeLit:
				for i, e := range v.Elts {
					v.Elts[i] = replace(e)
				}
			case *ast.SelectorExpr:
				v.X = replace(v.X)
			case *ast.StarExpr:
				v.X = replace(v.X)
			case *ast.TypeAssertExpr:
				v.X = replace(v.X)
			case *ast.IncDecStmt:
				v.X = replace(v.X)
			case *ast.SendStmt:
				v.Chan = replace(v.Chan)
				v.Value = replace(v.Value)
			case *ast.ExprStmt:
				v.X = replace(v.X)
			case *ast.ForStmt:
				if v.Cond != nil {
					v.Cond = replace(v.Cond)
				}
			case *ast.IfStmt:
				if v.Cond != nil {
					v.Cond = replace(v.Cond)
				}
			case *ast.SwitchStmt:
				if v.Tag != nil {
					v.Tag = replace(v.Tag)
				}
			case *ast.CaseClause:
				for i, e := range v.List {
					v.List[i] = replace(e)
				}
			case *ast.RangeStmt:
				v.X = replace(v.X)
			}
			return true
		})
	}
}

// ApplyExemptStructLiteralMismatches wraps each identified struct-literal
// value in []byte(...). Identifies by pointer equality — must be called
// with the exact nodes returned by FindExemptStructLiteralMismatches.
func ApplyExemptStructLiteralMismatches(files []*ast.File, exprs []ast.Expr) {
	if len(exprs) == 0 {
		return
	}
	targets := map[ast.Expr]bool{}
	for _, e := range exprs {
		targets[e] = true
	}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			for i, elt := range cl.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if targets[kv.Value] {
						orig := kv.Value
						kv.Value = makeSliceByteCall(kv.Value)
						delete(targets, orig)
					}
				} else {
					if targets[elt] {
						cl.Elts[i] = makeSliceByteCall(elt)
						delete(targets, elt)
					}
				}
			}
			return true
		})
	}
}

// NonExemptBoundaryFix describes an arg that needs wrapping; the Kind
// field picks the wrap form.
type NonExemptBoundaryFix struct {
	Arg  ast.Expr
	Kind string // "toString" or "toBytes"
}

// FindNonExemptCrossBoundaryMismatches walks the AST of a Moxie-target
// (non-exempt) package and returns a list of call arguments that need a
// type-bridge wrap to reconcile stock go/types with the Moxie string==[]byte
// identity.
//
// Two directions are handled:
//  1. []byte arg → string param: happens when calling into an exempt package
//     whose signature still uses native Go string (e.g. syscall.Open). Wrap
//     in `string(...)`.
//  2. string arg → []byte param: happens when the arg came from an exempt
//     package's return (e.g. runtime.GOROOT()) but is being passed to a
//     non-exempt callee whose signature was rewritten to []byte. Wrap in
//     `[]byte(...)`.
func FindNonExemptCrossBoundaryMismatches(files []*ast.File, info *types.Info) []NonExemptBoundaryFix {
	var result []NonExemptBoundaryFix
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			// Special-case builtin append: additional args after the slice
			// must match the element type. If the slice is [][]byte and a
			// subsequent arg is a string, wrap in []byte(...).
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "append" && len(call.Args) >= 2 {
				if tv, ok := info.Types[call.Args[0]]; ok {
					if slice, ok := tv.Type.(*types.Slice); ok {
						if eb, ok := slice.Elem().(*types.Slice); ok {
							if bb, ok := eb.Elem().(*types.Basic); ok && bb.Kind() == types.Byte {
								// Element type is []byte; wrap string args.
								for i := 1; i < len(call.Args); i++ {
									arg := call.Args[i]
									argTV, ok := info.Types[arg]
									if !ok {
										continue
									}
									if ab, ok := argTV.Type.(*types.Basic); ok && (ab.Kind() == types.String || ab.Kind() == types.UntypedString) {
										if isSliceByteConversion(arg) {
											continue
										}
										result = append(result, NonExemptBoundaryFix{Arg: arg, Kind: "toBytes"})
									}
								}
							}
						}
					}
				}
				return true
			}
			// Special-case builtin delete(m, k): map keys stay as string
			// after the []byte rewrite, so if k is []byte, wrap in string().
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "delete" && len(call.Args) == 2 {
				if tv, ok := info.Types[call.Args[0]]; ok {
					if m, ok := tv.Type.Underlying().(*types.Map); ok {
						if kb, ok := m.Key().(*types.Basic); ok && kb.Kind() == types.String {
							arg := call.Args[1]
							if argTV, ok := info.Types[arg]; ok {
								if slice, ok := argTV.Type.(*types.Slice); ok {
									if bb, ok := slice.Elem().(*types.Basic); ok && bb.Kind() == types.Byte {
										result = append(result, NonExemptBoundaryFix{Arg: arg, Kind: "toString"})
									}
								}
							}
						}
					}
				}
				return true
			}
			var sig *types.Signature
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				if tv, ok := info.Types[fn]; ok {
					sig, _ = tv.Type.(*types.Signature)
				}
			case *ast.Ident:
				if tv, ok := info.Types[fn]; ok {
					sig, _ = tv.Type.(*types.Signature)
				}
			}
			if sig == nil {
				return true
			}
			params := sig.Params()
			for i, arg := range call.Args {
				if i >= params.Len() {
					break
				}
				paramType := params.At(i).Type()
				argTV, ok := info.Types[arg]
				if !ok {
					continue
				}
				// paramString := paramType is *types.Basic with Kind string
				if pb, ok := paramType.(*types.Basic); ok && pb.Kind() == types.String {
					// Arg should be []byte; if so, wrap in string.
					if slice, ok := argTV.Type.(*types.Slice); ok {
						if bb, ok := slice.Elem().(*types.Basic); ok && bb.Kind() == types.Byte {
							// Already string(...)?
							if c, ok := arg.(*ast.CallExpr); ok {
								if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "string" && len(c.Args) == 1 {
									continue
								}
							}
							result = append(result, NonExemptBoundaryFix{Arg: arg, Kind: "toString"})
						}
					}
					continue
				}
				// paramType is []byte?
				if slice, ok := paramType.(*types.Slice); ok {
					if bb, ok := slice.Elem().(*types.Basic); ok && bb.Kind() == types.Byte {
						// Arg should be string; if so, wrap in []byte.
						if ab, ok := argTV.Type.(*types.Basic); ok && (ab.Kind() == types.String || ab.Kind() == types.UntypedString) {
							if isSliceByteConversion(arg) {
								continue
							}
							result = append(result, NonExemptBoundaryFix{Arg: arg, Kind: "toBytes"})
						}
					}
				}
			}
			return true
		})
	}
	return result
}

// ApplyNonExemptCrossBoundaryMismatches wraps each identified arg expression
// per its Kind: `string(...)` for toString, `[]byte(...)` for toBytes.
// Identifies args by pointer equality — must be called with the exact nodes
// returned by FindNonExemptCrossBoundaryMismatches.
func ApplyNonExemptCrossBoundaryMismatches(files []*ast.File, fixes []NonExemptBoundaryFix) {
	if len(fixes) == 0 {
		return
	}
	targets := map[ast.Expr]string{}
	for _, f := range fixes {
		targets[f.Arg] = f.Kind
	}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			for i, arg := range call.Args {
				if kind, ok := targets[arg]; ok {
					switch kind {
					case "toString":
						call.Args[i] = makeStringCall(arg)
					case "toBytes":
						call.Args[i] = makeSliceByteCall(arg)
					}
					delete(targets, arg)
				}
			}
			return true
		})
	}
}

// RewriteExemptCrossBoundaryCalls wraps string literals passed as arguments
// to calls that cross from an exempt package (e.g. syscall, os, runtime)
// into a non-exempt package whose signatures have already been rewritten to
// []byte. The exempt package keeps native Go string types internally, but
// its calls into errors.New/fmt.Errorf/etc must match the rewritten []byte
// signatures seen by stock go/types.
func RewriteExemptCrossBoundaryCalls(file *ast.File) {
	// Collect import names that are in scope (both explicit names and the
	// trailing path component of each import). Local identifiers that don't
	// match an import must not be treated as package references.
	imports := map[string]bool{}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, "\"")
		if imp.Name != nil {
			imports[imp.Name.Name] = true
			continue
		}
		// Default package ident is the last path segment.
		name := path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			name = path[i+1:]
		}
		imports[name] = true
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		// Only wrap when pkgIdent is actually an imported package name.
		if !imports[pkgIdent.Name] {
			return true
		}
		// Only wrap when calling into a NON-exempt package.
		if !IsMoxieStringTarget(pkgIdent.Name) {
			return true
		}
		// Wrap only string literals. Wrapping arbitrary identifiers would
		// mis-type args like uintptr or int. Variable-typed string args
		// are handled in a type-info-driven second pass.
		for i, arg := range call.Args {
			if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				call.Args[i] = makeSliceByteCall(lit)
			}
		}
		return true
	})
}

// RewriteTextConcat converts syntactically-detectable text concatenations
// (ADD or OR between text operands) to __moxie_concat(X, Y) calls and
// syntactic text comparisons (EQL/NEQ/LSS/LEQ/GTR/GEQ) to __moxie_eq /
// __moxie_lt calls, BEFORE typecheck. Used by moxiejs, which lacks native
// moxie's patched go/types that accepts []byte ops.
//
// Runs after RewriteStringLiterals so stringlit operands are already wrapped
// in []byte(...). An operand is considered text when it is:
//   - []byte(...) CallExpr (including the wraps from RewriteStringLiterals)
//   - a ParenExpr whose inner expr is text
//   - an existing __moxie_concat(...) call
func RewriteTextConcat(file *ast.File) {
	replace := func(expr ast.Expr) ast.Expr {
		return rewriteTextConcatExpr(expr)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		// Skip const declarations — __moxie_concat/__moxie_eq/__moxie_lt
		// are runtime calls and cannot appear in const expressions.
		// `const X = GOARCH == "amd64" || ...` must stay Go-native.
		if gd, ok := n.(*ast.GenDecl); ok && gd.Tok == token.CONST {
			return false
		}
		// Skip `[]byte(...)` casts whose body is purely string literals
		// so stock Go can still constant-fold `[]byte("a"+"b")`. If the
		// body references a variable (as in `[]byte("prefix" | x)` where
		// x was rewritten to []byte), rewrite the inner concat so stock
		// go/types accepts it.
		if call, ok := n.(*ast.CallExpr); ok && isSliceByteConversion(call) {
			if allStringLitBinaryArg(call) {
				return false
			}
			// Unwrap: `[]byte(X + Y)` → replace with the rewritten X ⊕ Y
			// (which will be a __moxie_concat(...) call returning []byte,
			// so the outer []byte(...) wrap is redundant). We transform in
			// place via the parent-node replacements below.
		}
		switch parent := n.(type) {
		case *ast.AssignStmt:
			for i := range parent.Rhs {
				parent.Rhs[i] = replace(parent.Rhs[i])
			}
		case *ast.ValueSpec:
			for i := range parent.Values {
				parent.Values[i] = replace(parent.Values[i])
			}
		case *ast.ReturnStmt:
			for i := range parent.Results {
				parent.Results[i] = replace(parent.Results[i])
			}
		case *ast.CallExpr:
			for i := range parent.Args {
				parent.Args[i] = replace(parent.Args[i])
			}
		case *ast.SendStmt:
			parent.Value = replace(parent.Value)
		case *ast.BinaryExpr:
			parent.X = replace(parent.X)
			parent.Y = replace(parent.Y)
		case *ast.ParenExpr:
			parent.X = replace(parent.X)
		case *ast.IndexExpr:
			parent.Index = replace(parent.Index)
		case *ast.KeyValueExpr:
			parent.Value = replace(parent.Value)
		case *ast.CompositeLit:
			for i := range parent.Elts {
				parent.Elts[i] = replace(parent.Elts[i])
			}
		case *ast.IfStmt:
			if parent.Cond != nil {
				parent.Cond = replace(parent.Cond)
			}
		case *ast.ForStmt:
			if parent.Cond != nil {
				parent.Cond = replace(parent.Cond)
			}
		case *ast.SwitchStmt:
			if parent.Tag != nil {
				parent.Tag = replace(parent.Tag)
			}
		case *ast.CaseClause:
			for i := range parent.List {
				parent.List[i] = replace(parent.List[i])
			}
		case *ast.ExprStmt:
			parent.X = replace(parent.X)
		case *ast.IncDecStmt:
			parent.X = replace(parent.X)
		case *ast.UnaryExpr:
			parent.X = replace(parent.X)
		case *ast.StarExpr:
			parent.X = replace(parent.X)
		case *ast.SliceExpr:
			if parent.Low != nil {
				parent.Low = replace(parent.Low)
			}
			if parent.High != nil {
				parent.High = replace(parent.High)
			}
			if parent.Max != nil {
				parent.Max = replace(parent.Max)
			}
		case *ast.TypeAssertExpr:
			parent.X = replace(parent.X)
		}
		return true
	})
}

// rewriteTextConcatExpr recursively transforms BinaryExpr ADD/OR nodes whose
// operands are syntactically text into __moxie_concat calls, and comparison
// BinaryExprs (EQL/NEQ/LSS/LEQ/GTR/GEQ) whose operands are syntactically
// text into __moxie_eq / __moxie_lt calls. Returns the rewritten expression
// (or the original when no rewrite applies).
func rewriteTextConcatExpr(expr ast.Expr) ast.Expr {
	if expr == nil {
		return expr
	}
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		e.X = rewriteTextConcatExpr(e.X)
		e.Y = rewriteTextConcatExpr(e.Y)
		switch e.Op {
		case token.ADD, token.OR:
			xText := isSyntacticTextExpr(e.X)
			yText := isSyntacticTextExpr(e.Y)
			// Only rewrite when at least one side is syntactically
			// text. This keeps bitwise `|` on user-defined int
			// types intact (e.g. `Int16(b[0]) | Int16(b[1])<<8`)
			// while correctly catching text concat (since
			// RewriteStringLiterals has already wrapped every
			// stringlit in `[]byte(...)`).
			if !xText && !yText {
				return e
			}
			// Once we commit to rewriting (because at least one side
			// is text), any nested `+`/`|` on the other side must also
			// be part of a text concat chain. Force-rewrite them so
			// `out + short + []byte(":")` doesn't leave an inner
			// `out + short` BinaryExpr under a `[]byte(...)` wrap,
			// which fails stock go/types as `[]byte + string`.
			e.X = forceTextConcat(e.X)
			e.Y = forceTextConcat(e.Y)
			// Wrap non-text operands in `[]byte(...)` so the
			// __moxie_concat([]byte, []byte) signature is satisfied
			// regardless of whether the operand is string-typed (method
			// calls like e.Err.Error()) or []byte-typed (field accesses
			// post-RewriteStringTypes). Identity conversion for []byte,
			// explicit conversion for string — both accepted by
			// stock go/types.
			return &ast.CallExpr{
				Fun:  &ast.Ident{Name: "__moxie_concat"},
				Args: []ast.Expr{wrapForMoxieConcat(e.X), wrapForMoxieConcat(e.Y)},
			}
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			// Slice comparison rewrite: same syntactic rule. If
			// either operand is syntactically text, rewrite so the
			// standard typechecker doesn't reject slice-to-slice
			// comparison.
			if !isSyntacticTextExpr(e.X) && !isSyntacticTextExpr(e.Y) {
				return e
			}
			return rewriteTextCompare(e)
		}
		return e
	case *ast.ParenExpr:
		e.X = rewriteTextConcatExpr(e.X)
		return e
	case *ast.CallExpr:
		// Don't descend into `[]byte(...)` casts — see the walker comment.
		if isSliceByteConversion(e) {
			return e
		}
		for i := range e.Args {
			e.Args[i] = rewriteTextConcatExpr(e.Args[i])
		}
		return e
	case *ast.UnaryExpr:
		e.X = rewriteTextConcatExpr(e.X)
		return e
	}
	return expr
}

// wrapForMoxieConcat wraps an expression in `[]byte(...)` unless it's
// already a syntactic text expression. Used when constructing arguments
// to __moxie_concat so that operands like `e.Err.Error()` (string-returning
// method calls) or `e.Func` (named []byte field accesses) end up as []byte
// per stock go/types — Moxie's identity-conversion rule lets []byte→[]byte
// pass too.
func wrapForMoxieConcat(e ast.Expr) ast.Expr {
	if isSyntacticTextExpr(e) {
		return e
	}
	return &ast.CallExpr{
		Fun:  &ast.ArrayType{Elt: ast.NewIdent("byte")},
		Args: []ast.Expr{e},
	}
}

// allStringLitBinaryArg reports whether `call` is a `[]byte(X)` cast whose
// body X is a tree of ADD/OR binary expressions over string literals only.
// In that case stock Go can constant-fold (`[]byte("a"+"b")`), so the
// walker must NOT descend and rewrite the inner `+` to __moxie_concat.
// Anything else — an Ident, SelectorExpr, CallExpr, etc. — means the body
// references a variable and must be lowered.
func allStringLitBinaryArg(call *ast.CallExpr) bool {
	if len(call.Args) != 1 {
		return false
	}
	var check func(e ast.Expr) bool
	check = func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.BasicLit:
			return x.Kind == token.STRING
		case *ast.BinaryExpr:
			if x.Op != token.ADD && x.Op != token.OR {
				return false
			}
			return check(x.X) && check(x.Y)
		case *ast.ParenExpr:
			return check(x.X)
		}
		return false
	}
	return check(call.Args[0])
}

// forceTextConcat rewrites any ADD/OR BinaryExpr inside expr as a
// __moxie_concat call, even when neither operand is syntactically text.
// Used by rewriteTextConcatExpr when the enclosing expression has already
// committed to a text-concat rewrite, so nested `+`/`|` chains between
// variables (whose types we don't know yet) must be lowered too.
func forceTextConcat(expr ast.Expr) ast.Expr {
	if expr == nil {
		return expr
	}
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		if e.Op == token.ADD || e.Op == token.OR {
			e.X = forceTextConcat(e.X)
			e.Y = forceTextConcat(e.Y)
			return &ast.CallExpr{
				Fun:  &ast.Ident{Name: "__moxie_concat"},
				Args: []ast.Expr{wrapForMoxieConcat(e.X), wrapForMoxieConcat(e.Y)},
			}
		}
		return e
	case *ast.ParenExpr:
		e.X = forceTextConcat(e.X)
		return e
	}
	return expr
}

// rewriteTextCompare lowers a BinaryExpr comparison between syntactically
// text operands to the appropriate __moxie_eq / __moxie_lt form.
func rewriteTextCompare(e *ast.BinaryExpr) ast.Expr {
	// Wrap non-text operands in []byte(...) so __moxie_eq / __moxie_lt's
	// []byte parameters see consistent types. Mirrors wrapForMoxieConcat.
	// Handles the common case of cross-package selectors like runtime.GOOS
	// (untyped string constant from exempt runtime) being compared to a
	// string literal that's already been wrapped as []byte(...).
	x := wrapForMoxieConcat(e.X)
	y := wrapForMoxieConcat(e.Y)
	eq := func(x, y ast.Expr) *ast.CallExpr {
		return &ast.CallExpr{Fun: &ast.Ident{Name: "__moxie_eq"}, Args: []ast.Expr{x, y}}
	}
	lt := func(x, y ast.Expr) *ast.CallExpr {
		return &ast.CallExpr{Fun: &ast.Ident{Name: "__moxie_lt"}, Args: []ast.Expr{x, y}}
	}
	not := func(x ast.Expr) *ast.UnaryExpr { return &ast.UnaryExpr{Op: token.NOT, X: x} }
	switch e.Op {
	case token.EQL:
		return eq(x, y)
	case token.NEQ:
		return not(eq(x, y))
	case token.LSS:
		return lt(x, y)
	case token.LEQ:
		return not(lt(y, x))
	case token.GTR:
		return lt(y, x)
	case token.GEQ:
		return not(lt(x, y))
	}
	return e
}

// isSyntacticTextExpr returns true when the expression is recognisable as a
// text value before typecheck: a []byte(...) conversion, a string literal,
// a __moxie_concat call, a slice expression (which in Moxie-target packages
// almost always yields a []byte sub-slice), or a parenthesised text expression.
func isSyntacticTextExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.CallExpr:
		if isSliceByteConversion(e) {
			return true
		}
		if isMoxieConcatCall(e) {
			return true
		}
	case *ast.ParenExpr:
		return isSyntacticTextExpr(e.X)
	case *ast.SliceExpr:
		// foo[i:j] — in Moxie-target packages, slicing produces a
		// []byte sub-slice for string/[]byte variables. Treating it
		// as text lets comparisons like `line[i:i+n] == prefix`
		// rewrite to __moxie_eq without post-typecheck type info.
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// 2b. Builtin int→int32 wrapping (AST-level, after parsing, before typecheck)
// ---------------------------------------------------------------------------

// rewriteBuiltinIntReturns wraps len(), cap(), and copy() calls in int32()
// conversions. These builtins return int (from Go's universe), but Moxie
// uses int32 as the standard sized integer. Without this wrapping, mixing
// len() results with int32 values causes type checker errors.
//
//	len(x)       → int32(len(x))
//	cap(x)       → int32(cap(x))
//	copy(dst,src)→ int32(copy(dst,src))
func rewriteBuiltinIntReturns(file *ast.File) {
	// Builtins whose return type is int.
	intBuiltins := map[string]bool{"len": true, "cap": true, "copy": true}

	ast.Inspect(file, func(n ast.Node) bool {
		// Skip const declarations — const expressions must stay untyped.
		if gd, ok := n.(*ast.GenDecl); ok && gd.Tok == token.CONST {
			return false
		}
		// Don't descend into int32() wrappers we just created — prevents
		// infinite recursion (walker would find len() inside and re-wrap).
		if call, ok := n.(*ast.CallExpr); ok {
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "int32" {
				return false
			}
		}
		switch parent := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range parent.Rhs {
				if wrapped := wrapIntBuiltin(rhs, intBuiltins); wrapped != nil {
					parent.Rhs[i] = wrapped
				}
			}
		case *ast.ValueSpec:
			for i, val := range parent.Values {
				if wrapped := wrapIntBuiltin(val, intBuiltins); wrapped != nil {
					parent.Values[i] = wrapped
				}
			}
		case *ast.ReturnStmt:
			for i, result := range parent.Results {
				if wrapped := wrapIntBuiltin(result, intBuiltins); wrapped != nil {
					parent.Results[i] = wrapped
				}
			}
		case *ast.CallExpr:
			for i, arg := range parent.Args {
				if wrapped := wrapIntBuiltin(arg, intBuiltins); wrapped != nil {
					parent.Args[i] = wrapped
				}
			}
		case *ast.BinaryExpr:
			if wrapped := wrapIntBuiltin(parent.X, intBuiltins); wrapped != nil {
				parent.X = wrapped
			}
			if wrapped := wrapIntBuiltin(parent.Y, intBuiltins); wrapped != nil {
				parent.Y = wrapped
			}
		case *ast.IndexExpr:
			if wrapped := wrapIntBuiltin(parent.Index, intBuiltins); wrapped != nil {
				parent.Index = wrapped
			}
		case *ast.SendStmt:
			if wrapped := wrapIntBuiltin(parent.Value, intBuiltins); wrapped != nil {
				parent.Value = wrapped
			}
		case *ast.KeyValueExpr:
			if wrapped := wrapIntBuiltin(parent.Value, intBuiltins); wrapped != nil {
				parent.Value = wrapped
			}
		}
		return true
	})
}

// wrapIntBuiltin checks if expr is a call to a builtin that returns int,
// and if so, wraps it in int32(). Returns nil if no wrapping needed.
func wrapIntBuiltin(expr ast.Expr, builtins map[string]bool) ast.Expr {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || !builtins[ident.Name] {
		return nil
	}
	// Already wrapped in int32() — don't double-wrap.
	// (Check grandparent, but simpler: check if Fun is already int32.)
	return &ast.CallExpr{
		Fun:  &ast.Ident{Name: "int32"},
		Args: []ast.Expr{call},
	}
}

// ---------------------------------------------------------------------------
// 3. Pipe concatenation rewrite (AST-level, after first typecheck pass)
// ---------------------------------------------------------------------------

// RewriteConstPipes changes | to + inside const declarations where operands
// are string literals. Go's compiler folds "a" + "b" at compile time, but
// __moxie_concat is a runtime call and cannot appear in a const.
func RewriteConstPipes(files []*ast.File) {
	for _, file := range files {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, val := range vs.Values {
					constPipeToAdd(val)
				}
			}
		}
	}
}

// constPipeToAdd recursively rewrites | to + in binary expressions
// where all leaves are string literals.
func constPipeToAdd(e ast.Expr) bool {
	switch n := e.(type) {
	case *ast.BasicLit:
		return n.Kind == token.STRING
	case *ast.BinaryExpr:
		xLit := constPipeToAdd(n.X)
		yLit := constPipeToAdd(n.Y)
		if xLit && yLit && n.Op == token.OR {
			n.Op = token.ADD
		}
		return xLit && yLit
	case *ast.ParenExpr:
		return constPipeToAdd(n.X)
	}
	return false
}

// PipeRewrite records a | expression that should become __moxie_concat.
type PipeRewrite struct {
	parent ast.Node
	expr   *ast.BinaryExpr
}

// findPipeConcat walks the AST and finds | and + expressions where operands
// are []byte, using type information from a completed typecheck pass.
// Catches both explicit | (pipe concat) and + (string concat that wasn't
// converted by mxpurify).
func FindPipeConcat(files []*ast.File, info *types.Info) []PipeRewrite {
	var rewrites []PipeRewrite
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			// Don't descend into const declarations — __moxie_concat is a
			// runtime call and cannot appear in const expressions.
			if gd, ok := n.(*ast.GenDecl); ok && gd.Tok == token.CONST {
				return false
			}
			// Don't descend into `[]byte(...)` casts — their inner
			// expression must stay Go-native so the type checker can
			// constant-fold stringlit + stringlit.
			if call, ok := n.(*ast.CallExpr); ok && isSliceByteConversion(call) {
				return false
			}
			bin, ok := n.(*ast.BinaryExpr)
			if !ok || (bin.Op != token.OR && bin.Op != token.ADD) {
				return true
			}
			xType := info.TypeOf(bin.X)
			yType := info.TypeOf(bin.Y)
			xOk := (xType != nil && isTextType(xType)) || isSliceByteConversion(bin.X) || isMoxieConcatCall(bin.X) || isStringLit(bin.X)
			yOk := (yType != nil && isTextType(yType)) || isSliceByteConversion(bin.Y) || isMoxieConcatCall(bin.Y) || isStringLit(bin.Y)
			if bin.Op == token.OR || bin.Op == token.ADD {
				if !xOk && yOk {
					if inner, ok := bin.X.(*ast.BinaryExpr); ok && (inner.Op == token.OR || inner.Op == token.ADD) {
						xOk = true
					}
				}
				if !yOk && xOk {
					if inner, ok := bin.Y.(*ast.BinaryExpr); ok && (inner.Op == token.OR || inner.Op == token.ADD) {
						yOk = true
					}
				}
			}
			if xOk && yOk {
				rewrites = append(rewrites, PipeRewrite{expr: bin})
			}
			return true
		})
	}
	return rewrites
}

// isStringLit returns true if e is a string literal (token.STRING).
func isStringLit(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}

// CheckPlusOnText walks the AST and returns errors for any + or += used on
// text types. Call this for user packages only — stdlib/vendor may still use +.
func CheckPlusOnText(files []*ast.File, info *types.Info, fset *token.FileSet) []error {
	var errs []error
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.BinaryExpr:
				if node.Op != token.ADD {
					return true
				}
				xType := info.TypeOf(node.X)
				yType := info.TypeOf(node.Y)
				xText := (xType != nil && isTextType(xType)) || isSliceByteConversion(node.X) || isStringLit(node.X)
				yText := (yType != nil && isTextType(yType)) || isSliceByteConversion(node.Y) || isStringLit(node.Y)
				if xText || yText {
					pos := fset.Position(node.Pos())
					errs = append(errs, fmt.Errorf("%s: moxie: '+' is not allowed for text concatenation, use | operator", pos))
				}
			case *ast.AssignStmt:
				if node.Tok != token.ADD_ASSIGN || len(node.Lhs) != 1 {
					return true
				}
				lhsType := info.TypeOf(node.Lhs[0])
				if lhsType != nil && isTextType(lhsType) {
					pos := fset.Position(node.Pos())
					errs = append(errs, fmt.Errorf("%s: moxie: '+=' is not allowed for text concatenation, use |= operator", pos))
				}
			}
			return true
		})
	}
	return errs
}

// isByteSlice returns true if t is []byte (or []uint8).
func isByteSlice(t types.Type) bool {
	sl, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	basic, ok := sl.Elem().(*types.Basic)
	return ok && basic.Kind() == types.Byte
}

// isMoxieConcatCall returns true if the expression is a __moxie_concat call.
// Needed for chained + detection after earlier rewrites replaced inner + nodes.
func isMoxieConcatCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == "__moxie_concat"
}

// isTextType returns true if t is []byte or string (equivalent under Moxie's
// string=[]byte unification).
func isTextType(t types.Type) bool {
	if isByteSlice(t) {
		return true
	}
	basic, ok := t.Underlying().(*types.Basic)
	return ok && basic.Info()&types.IsString != 0
}

func isTextLike(t types.Type) bool {
	if t == nil {
		return false
	}
	if isByteSlice(t) {
		return true
	}
	basic, ok := t.Underlying().(*types.Basic)
	return ok && basic.Info()&types.IsString != 0
}

// RewriteAddAssign converts `s += expr` and `s |= expr` to
// `s = __moxie_concat(s, expr)` for text types. Both forms compile, but
// CheckPlusOnText rejects += for user packages.
func RewriteAddAssign(files []*ast.File, info *types.Info) int {
	count := 0
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 {
				return true
			}
			if assign.Tok != token.ADD_ASSIGN && assign.Tok != token.OR_ASSIGN {
				return true
			}
			lhsType := info.TypeOf(assign.Lhs[0])
			if lhsType == nil || !isTextType(lhsType) {
				return true
			}
			assign.Tok = token.ASSIGN
			// Wrap non-text operands in []byte(...) so callable-returning
			// strings (like itoa.Itoa(...)) match __moxie_concat's []byte
			// signature under stock go/types.
			assign.Rhs[0] = &ast.CallExpr{
				Fun: &ast.Ident{Name: "__moxie_concat"},
				Args: []ast.Expr{
					wrapForMoxieConcat(assign.Lhs[0]),
					wrapForMoxieConcat(assign.Rhs[0]),
				},
			}
			count++
			return true
		})
	}
	return count
}

// applyPipeRewrites replaces | binary expressions with __moxie_concat calls.
// It walks the AST and replaces matching BinaryExpr nodes in-place.
func ApplyPipeRewrites(files []*ast.File, rewrites []PipeRewrite) {
	// Build a set of expressions to rewrite.
	rewriteSet := make(map[*ast.BinaryExpr]bool)
	for _, r := range rewrites {
		rewriteSet[r.expr] = true
	}
	if len(rewriteSet) == 0 {
		return
	}

	// Walk AST and replace in parent nodes.
	for _, file := range files {
		replaceInNode(file, rewriteSet)
	}
}

// replaceInNode walks a node and replaces any child expressions that are
// in the rewrite set with __moxie_concat(left, right) calls.
func replaceInNode(node ast.Node, set map[*ast.BinaryExpr]bool) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch parent := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range parent.Rhs {
				parent.Rhs[i] = maybeReplacePipe(rhs, set)
			}
		case *ast.ValueSpec:
			for i, val := range parent.Values {
				parent.Values[i] = maybeReplacePipe(val, set)
			}
		case *ast.ReturnStmt:
			for i, result := range parent.Results {
				parent.Results[i] = maybeReplacePipe(result, set)
			}
		case *ast.CallExpr:
			for i, arg := range parent.Args {
				parent.Args[i] = maybeReplacePipe(arg, set)
			}
		case *ast.SendStmt:
			parent.Value = maybeReplacePipe(parent.Value, set)
		case *ast.BinaryExpr:
			parent.X = maybeReplacePipe(parent.X, set)
			parent.Y = maybeReplacePipe(parent.Y, set)
		case *ast.ParenExpr:
			parent.X = maybeReplacePipe(parent.X, set)
		case *ast.IndexExpr:
			parent.Index = maybeReplacePipe(parent.Index, set)
		case *ast.KeyValueExpr:
			parent.Value = maybeReplacePipe(parent.Value, set)
		case *ast.CompositeLit:
			for i, elt := range parent.Elts {
				parent.Elts[i] = maybeReplacePipe(elt, set)
			}
		}
		return true
	})
}

func maybeReplacePipe(expr ast.Expr, set map[*ast.BinaryExpr]bool) ast.Expr {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok || !set[bin] {
		return expr
	}
	// Replace: a | b → __moxie_concat([]byte(a), []byte(b))
	// Wrap non-text operands so string-returning method calls (err.Error())
	// and string-typed vars match __moxie_concat's []byte param type.
	return &ast.CallExpr{
		Fun:  &ast.Ident{Name: "__moxie_concat"},
		Args: []ast.Expr{wrapForMoxieConcat(bin.X), wrapForMoxieConcat(bin.Y)},
	}
}

// FilterPipeErrors removes type errors about | and + on text types from the
// error list. Both are rewritten to __moxie_concat — the user-facing rejection
// of + happens separately via CheckPlusOnText.
func FilterPipeErrors(errs []error) []error {
	var filtered []error
	for _, err := range errs {
		msg := err.Error()
		if strings.Contains(msg, "operator |") && strings.Contains(msg, "[]") {
			continue
		}
		if strings.Contains(msg, "operator |") && strings.Contains(msg, "string") {
			continue
		}
		if strings.Contains(msg, "operator +") && strings.Contains(msg, "[]byte") {
			continue
		}
		if strings.Contains(msg, "mismatched types") && strings.Contains(msg, "operator +") {
			continue
		}
		if strings.Contains(msg, "operator |=") {
			continue
		}
		if strings.Contains(msg, "operator +=") && strings.Contains(msg, "[]byte") {
			continue
		}
		filtered = append(filtered, err)
	}
	return filtered
}

// ---------------------------------------------------------------------------
// 4. Byte slice comparison rewrite (AST-level, after first typecheck pass)
// ---------------------------------------------------------------------------
//
// Moxie uses []byte as its text type. Go's type checker doesn't allow
// ==, !=, <, <=, >, >= on slices. This rewrite converts []byte comparisons
// to __moxie_eq / __moxie_lt calls, and converts switch statements on []byte
// to tag-less switches with __moxie_eq calls.

// findByteComparisons finds binary expressions comparing two []byte values.
// If one side is []byte and the other is string (e.g. map-key string compared
// against a []byte var), wraps the string side in []byte(...) so the emitted
// __moxie_eq/__moxie_lt receives matching types.
func FindByteComparisons(files []*ast.File, info *types.Info) []*ast.BinaryExpr {
	var result []*ast.BinaryExpr
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			bin, ok := n.(*ast.BinaryExpr)
			if !ok {
				return true
			}
			switch bin.Op {
			case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			default:
				return true
			}
			xType := info.TypeOf(bin.X)
			yType := info.TypeOf(bin.Y)
			if xType == nil || yType == nil {
				return true
			}
			xBytes := isByteSlice(xType)
			yBytes := isByteSlice(yType)
			if !xBytes && !yBytes {
				return true
			}
			// Bridge string↔[]byte mismatches by wrapping string side.
			if xBytes && !yBytes && isStringKind(yType) {
				bin.Y = wrapInByteSlice(bin.Y)
			}
			if yBytes && !xBytes && isStringKind(xType) {
				bin.X = wrapInByteSlice(bin.X)
			}
			result = append(result, bin)
			return true
		})
	}
	return result
}

// wrapInByteSlice wraps expr in []byte(expr).
func wrapInByteSlice(expr ast.Expr) ast.Expr {
	return &ast.CallExpr{
		Fun:  &ast.ArrayType{Elt: ast.NewIdent("byte")},
		Args: []ast.Expr{expr},
	}
}

// applyByteComparisonRewrites replaces []byte comparison expressions with
// __moxie_eq / __moxie_lt function calls.
func ApplyByteComparisonRewrites(files []*ast.File, exprs []*ast.BinaryExpr) {
	set := make(map[*ast.BinaryExpr]bool)
	for _, e := range exprs {
		set[e] = true
	}
	if len(set) == 0 {
		return
	}
	for _, file := range files {
		replaceComparisons(file, set)
	}
}

func replaceComparisons(node ast.Node, set map[*ast.BinaryExpr]bool) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch parent := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range parent.Rhs {
				parent.Rhs[i] = maybeReplaceCmp(rhs, set)
			}
		case *ast.ValueSpec:
			for i, val := range parent.Values {
				parent.Values[i] = maybeReplaceCmp(val, set)
			}
		case *ast.ReturnStmt:
			for i, result := range parent.Results {
				parent.Results[i] = maybeReplaceCmp(result, set)
			}
		case *ast.CallExpr:
			for i, arg := range parent.Args {
				parent.Args[i] = maybeReplaceCmp(arg, set)
			}
		case *ast.IfStmt:
			parent.Cond = maybeReplaceCmp(parent.Cond, set)
		case *ast.ForStmt:
			if parent.Cond != nil {
				parent.Cond = maybeReplaceCmp(parent.Cond, set)
			}
		case *ast.BinaryExpr:
			// Handle nested: (a == b) && (c == d)
			parent.X = maybeReplaceCmp(parent.X, set)
			parent.Y = maybeReplaceCmp(parent.Y, set)
		case *ast.UnaryExpr:
			parent.X = maybeReplaceCmp(parent.X, set)
		case *ast.ParenExpr:
			parent.X = maybeReplaceCmp(parent.X, set)
		case *ast.CaseClause:
			for i, val := range parent.List {
				parent.List[i] = maybeReplaceCmp(val, set)
			}
		case *ast.SendStmt:
			parent.Value = maybeReplaceCmp(parent.Value, set)
		case *ast.CompositeLit:
			for i, elt := range parent.Elts {
				parent.Elts[i] = maybeReplaceCmp(elt, set)
			}
		}
		return true
	})
}

func maybeReplaceCmp(expr ast.Expr, set map[*ast.BinaryExpr]bool) ast.Expr {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok || !set[bin] {
		return expr
	}
	switch bin.Op {
	case token.EQL:
		// a == b → __moxie_eq(a, b)
		return &ast.CallExpr{
			Fun:  &ast.Ident{Name: "__moxie_eq"},
			Args: []ast.Expr{bin.X, bin.Y},
		}
	case token.NEQ:
		// a != b → !__moxie_eq(a, b)
		return &ast.UnaryExpr{
			Op: token.NOT,
			X: &ast.CallExpr{
				Fun:  &ast.Ident{Name: "__moxie_eq"},
				Args: []ast.Expr{bin.X, bin.Y},
			},
		}
	case token.LSS:
		// a < b → __moxie_lt(a, b)
		return &ast.CallExpr{
			Fun:  &ast.Ident{Name: "__moxie_lt"},
			Args: []ast.Expr{bin.X, bin.Y},
		}
	case token.LEQ:
		// a <= b → !__moxie_lt(b, a)
		return &ast.UnaryExpr{
			Op: token.NOT,
			X: &ast.CallExpr{
				Fun:  &ast.Ident{Name: "__moxie_lt"},
				Args: []ast.Expr{bin.Y, bin.X},
			},
		}
	case token.GTR:
		// a > b → __moxie_lt(b, a)
		return &ast.CallExpr{
			Fun:  &ast.Ident{Name: "__moxie_lt"},
			Args: []ast.Expr{bin.Y, bin.X},
		}
	case token.GEQ:
		// a >= b → !__moxie_lt(a, b)
		return &ast.UnaryExpr{
			Op: token.NOT,
			X: &ast.CallExpr{
				Fun:  &ast.Ident{Name: "__moxie_lt"},
				Args: []ast.Expr{bin.X, bin.Y},
			},
		}
	}
	return expr
}

// findByteSwitches finds switch statements that switch on a []byte expression.
func FindByteSwitches(files []*ast.File, info *types.Info) []*ast.SwitchStmt {
	var result []*ast.SwitchStmt
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok || sw.Tag == nil {
				return true
			}
			tagType := info.TypeOf(sw.Tag)
			if tagType != nil && isByteSlice(tagType) {
				result = append(result, sw)
			}
			return true
		})
	}
	return result
}

// applyByteSwitchRewrites converts switch statements on []byte to tag-less
// switches with __moxie_eq calls.
//
//	switch x { case "a": ... }  →  switch { case __moxie_eq(x, []byte("a")): ... }
func ApplyByteSwitchRewrites(switches []*ast.SwitchStmt) {
	for _, sw := range switches {
		tag := sw.Tag
		sw.Tag = nil // make it a tag-less switch
		for _, stmt := range sw.Body.List {
			cc, ok := stmt.(*ast.CaseClause)
			if !ok || cc.List == nil {
				continue // default clause
			}
			for i, val := range cc.List {
				cc.List[i] = &ast.CallExpr{
					Fun:  &ast.Ident{Name: "__moxie_eq"},
					Args: []ast.Expr{tag, val},
				}
			}
		}
	}
}

// FindByteMapKeys returns IndexExpr nodes where the container is a
// map[string]V and the index expression has type []byte. moxiejs leaves
// map keys as `string` (because []byte is not comparable under stock
// go/types) but Moxie source often indexes such maps with []byte values.
// Each matched IndexExpr has its Index wrapped in a string(...) conversion
// by ApplyByteMapKeyRewrites so the second typecheck accepts it.
//
// Same pattern for assignments `m[k] = v`: if the map value type is string
// and v is []byte, the RHS is wrapped in string(v). Collected via
// FindByteMapValues.
func FindByteMapKeys(files []*ast.File, info *types.Info) []*ast.IndexExpr {
	var result []*ast.IndexExpr
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			idx, ok := n.(*ast.IndexExpr)
			if !ok {
				return true
			}
			containerType := info.TypeOf(idx.X)
			if containerType == nil {
				return true
			}
			mapType, ok := containerType.Underlying().(*types.Map)
			if !ok {
				return true
			}
			if !isStringKind(mapType.Key()) {
				return true
			}
			indexType := info.TypeOf(idx.Index)
			if indexType == nil {
				return true
			}
			if isByteSlice(indexType) {
				result = append(result, idx)
			}
			return true
		})
	}
	return result
}

// ApplyByteMapKeyRewrites wraps each matched IndexExpr's Index in a
// string(...) conversion call.
func ApplyByteMapKeyRewrites(exprs []*ast.IndexExpr) {
	for _, idx := range exprs {
		idx.Index = &ast.CallExpr{
			Fun:  ast.NewIdent("string"),
			Args: []ast.Expr{idx.Index},
		}
	}
}

// isStringKind reports whether t is the built-in `string` (not a named
// alias or composite). Named types whose underlying is string also count.
func isStringKind(t types.Type) bool {
	basic, ok := t.Underlying().(*types.Basic)
	return ok && basic.Kind() == types.String
}

// filterByteCompareErrors removes type errors about []byte comparison.
func FilterByteCompareErrors(errs []error) []error {
	var filtered []error
	for _, err := range errs {
		msg := err.Error()
		if strings.Contains(msg, "slice can only be compared to nil") {
			continue
		}
		if strings.Contains(msg, "mismatched types []byte and untyped string") {
			continue
		}
		if strings.Contains(msg, "cannot convert") && strings.Contains(msg, "untyped string") && strings.Contains(msg, "[]byte") {
			continue
		}
		// "invalid case" errors from switch on []byte
		if strings.Contains(msg, "invalid case") && strings.Contains(msg, "[]byte") {
			continue
		}
		filtered = append(filtered, err)
	}
	return filtered
}

// filterStringByteMismatch removes type errors about string/[]byte mismatches.
// In moxie, string and []byte are the same type, so these errors are spurious.
// FilterStringByteMismatch drops type errors caused by the string/[]byte
// unification gap — the standard Go type checker sees them as different types
// but Moxie treats them as identical. Called after pipe/comparison rewrites.
func FilterStringByteMismatch(errs []error) []error {
	var filtered []error
	for _, err := range errs {
		msg := err.Error()
		if strings.Contains(msg, "[]byte") && strings.Contains(msg, "string") &&
			(strings.Contains(msg, "cannot use") || strings.Contains(msg, "cannot convert") ||
				strings.Contains(msg, "mismatched types") || strings.Contains(msg, "does not satisfy")) {
			continue
		}
		filtered = append(filtered, err)
	}
	return filtered
}
