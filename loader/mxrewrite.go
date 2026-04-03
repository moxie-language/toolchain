package loader

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
	"go/ast"
	"go/scanner"
	"go/token"
	"go/types"
	"strings"
)

// MoxiePurePackages tracks stdlib packages that have been fully rewritten
// to Moxie syntax (including string→[]byte). These get ALL rewrites
// (string literals, | concat) and restriction enforcement.
var MoxiePurePackages = map[string]bool{
	// Packages with zero restricted type usage in compiled code.
	// Restriction enforcement (no string, uintptr, complex, new, fallthrough)
	// applies to these packages. String literal wrapping + pipe concat enabled.
	"arena":                                       true,
	"compress/bzip2":                               true,
	"compress/flate":                               true,
	"compress/gzip":                                true,
	"compress/lzw":                                true,
	"compress/zlib":                                true,
	"container/heap":                               true,
	"container/list":                               true,
	"container/ring":                               true,
	"crypto/aes":                                   true,
	"crypto/boring":                                true,
	"crypto/internal/boring":                       true,
	"crypto/cipher":                                true,
	"crypto/des":                                   true,
	"crypto/ecdh":                                  true,
	"crypto/elliptic":                              true,
	"crypto/dsa":                                   true,
	"crypto/ecdsa":                                 true,
	"crypto/fips140":                               true,
	"crypto/hkdf":                                  true,
	"crypto/hmac":                                  true,
	"crypto/md5":                                   true,
	"crypto/internal/boring/bbig":                  true,
	"crypto/internal/boring/bcache":                true,
	"crypto/internal/boring/sig":                   true,
	"crypto/internal/boring/syso":                  true,
	"crypto/internal/entropy":                      true,
	"crypto/internal/fips140/aes":                  true,
	"crypto/internal/fips140/aes/gcm":              true,
	"crypto/internal/fips140/alias":                true,
	"crypto/internal/fips140/bigmod":               true,
	"crypto/internal/fips140/drbg":                 true,
	"crypto/internal/fips140/ecdh":                 true,
	"crypto/internal/fips140/ecdsa":                true,
	"crypto/internal/fips140/edwards25519":         true,
	"crypto/internal/fips140/edwards25519/field":   true,
	"crypto/internal/fips140/hmac":                 true,
	"crypto/internal/fips140/mlkem":                true,
	"crypto/internal/fips140/nistec":               true,
	"crypto/internal/fips140/nistec/fiat":          true,
	"crypto/internal/fips140/sha256":               true,
	"crypto/internal/fips140/sha3":                 true,
	"crypto/internal/fips140/sha512":               true,
	"crypto/internal/fips140/ssh":                  true,
	"crypto/internal/fips140/subtle":               true,
	"crypto/internal/fips140cache":                  true,
	"crypto/internal/fips140deps":                  true,
	"crypto/internal/fips140deps/byteorder":        true,
	"crypto/internal/fips140deps/cpu":              true,
	"crypto/internal/fips140deps/godebug":          true,
	"crypto/internal/fips140hash":                  true,
	"crypto/internal/fips140only":                  true,
	"crypto/internal/randutil":                     true,
	"crypto/internal/sysrand":                      true,
	"crypto/internal/sysrand/internal/seccomp":     true,
	"crypto/mlkem":                                 true,
	"crypto/pbkdf2":                                true,
	"crypto/rand":                                  true,
	"crypto/rc4":                                   true,
	"crypto/rsa":                                   true,
	"crypto/sha1":                                  true,
	"crypto/sha3":                                  true,
	"crypto/sha256":                                true,
	"crypto/sha512":                                true,
	"crypto/subtle":                                true,
	"encoding":                                     true,
	"encoding/ascii85":                             true,
	"errors":                                       true,
	"encoding/gob":                                 true,
	"hash":                                         true,
	"hash/adler32":                                 true,
	"hash/crc32":                                   true,
	"hash/crc64":                                   true,
	"hash/fnv":                                     true,
	"image":                                        true,
	"image/color":                                  true,
	"image/color/palette":                          true,
	"image/draw":                                   true,
	"image/gif":                                    true,
	"image/internal/imageutil":                     true,
	"image/jpeg":                                   true,
	"image/png":                                    true,
	"internal/binary":                              true,
	"internal/byteorder":                           true,
	"internal/cfg":                                 true,
	"internal/coverage/calloc":                     true,
	"internal/coverage/cmerge":                     true,
	"internal/coverage/rtcov":                      true,
	"internal/coverage/slicewriter":                true,
	"internal/coverage/uleb128":                    true,
	"internal/fmtsort":                             true,
	"internal/fuzz":                                true,
	"internal/futex":                               true,
	"internal/gclayout":                            true,
	"internal/godebugs":                            true,
	"internal/goexperiment":                        true,
	"internal/goos":                                true,
	"internal/goroot":                              true,
	"internal/goversion":                           true,
	"internal/oserror":                             true,
	"internal/saferio":                             true,
	"internal/synctest":                            true,
	"iter":                                         true,
	"log/internal":                                 true,
	"log/slog/internal":                            true,
	"maps":                                         true,
	"math":                                         true,
	"math/bits":                                    true,
	"math/rand":                                    true,
	"math/rand/v2":                                 true,
	"mime/quotedprintable":                         true,
	"slices":                                       true,
	"sort":                                         true,
	"structs":                                      true,
	"testing/iotest":                               true,
	"unicode/utf16":                                true,
	"unique":                                       true,
	"weak":                                         true,
}

// MoxieSyntaxPackages is no longer needed — text-level rewrites (chan/slice
// literals) now fire on ALL packages. Only files with the new syntax
// ([]T{:n}, chan T{}) are affected; unconverted files pass through unchanged.

// isUserImportPath returns true for user/third-party packages.
// Same logic as compiler/restrict.go isUserPackage but for import paths.
func isUserImportPath(path string) bool {
	if !strings.Contains(path, ".") {
		return false
	}
	if strings.HasPrefix(path, "vendor/") {
		return false
	}
	return true
}

// isMoxieStringTarget returns true if a package should get string→[]byte
// rewrites (string literal wrapping, | concat, comparison rewrites).
// Runtime internals that implement Go interfaces with string methods are exempt.
func isMoxieStringTarget(importPath string) bool {
	// Exempt packages that have NOT been converted to []byte text.
	// Their source still uses native Go string type, so string literal
	// wrapping and comparison rewriting would break them.
	if strings.HasPrefix(importPath, "runtime") ||
		strings.HasPrefix(importPath, "internal/task") ||
		strings.HasPrefix(importPath, "internal/abi") ||
		strings.HasPrefix(importPath, "internal/reflectlite") ||
		importPath == "internal/itoa" ||
		strings.HasPrefix(importPath, "internal/fmtsort") ||
		strings.HasPrefix(importPath, "sync") ||
		strings.HasPrefix(importPath, "internal/sync") ||
		strings.HasPrefix(importPath, "syscall") ||
		strings.HasPrefix(importPath, "internal/syscall") ||
		strings.HasPrefix(importPath, "io") ||
		strings.HasPrefix(importPath, "os") ||
		strings.HasPrefix(importPath, "path") ||
		strings.HasPrefix(importPath, "unsafe") ||
		importPath == "time" ||
		importPath == "fmt" ||
		importPath == "strings" ||
		importPath == "bytes" ||
		importPath == "strconv" ||
		importPath == "errors" ||
		strings.HasPrefix(importPath, "math/big") ||
		strings.HasPrefix(importPath, "unicode") ||
		strings.HasPrefix(importPath, "reflect") ||
		strings.HasPrefix(importPath, "encoding") ||
		importPath == "bufio" ||
		importPath == "context" {
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
func rewriteChanLiterals(src []byte, fset *token.FileSet) []byte {
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

		// Write the replacement: make(chan T) or make(chan T, N).
		result.WriteString("make(chan ")
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
func rewriteSliceLiterals(src []byte, fset *token.FileSet) []byte {
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

		if len(colonPositions) == 1 {
			// []T{:len} → make([]T, len)
			lenStart := toks[colonPositions[0]+1].pos
			lenEnd := toks[closeIdx].pos
			lenExpr := strings.TrimSpace(string(src[lenStart:lenEnd]))
			if lenExpr == "" {
				continue
			}

			result.Write(src[lastEnd:toks[lbrackIdx].pos])
			result.WriteString("make(")
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
			result.WriteString("make(")
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
func rewriteStringLiterals(file *ast.File) {
	// Walk the AST and replace string expressions with []byte() wrapped versions.
	// We need to walk parent nodes to replace children in-place.
	rewriteStringExprs(file)
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
		// Convert string const declarations to var with []byte values.
		// Go const can only hold string; after mxpurify converts
		// fields/params to []byte, const strings can't be assigned.
		if gd, ok := n.(*ast.GenDecl); ok && gd.Tok == token.CONST {
			convertStringConstsToVars(gd)
			return false
		}
		// Skip function bodies that return string — these are interface-
		// mandated methods (Error(), String()) that cannot return []byte.
		if fd, ok := n.(*ast.FuncDecl); ok && funcReturnsString(fd) {
			return false
		}
		if fl, ok := n.(*ast.FuncLit); ok && funcTypeReturnsString(fl.Type) {
			return false
		}
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
// or a binary + of string expressions (constant string concatenation).
func isStringExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
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
func convertStringConstsToVars(gd *ast.GenDecl) {
	// Check if this is a pure string const block.
	// If any spec uses iota or has non-string values, skip entirely.
	hasString := false
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			return
		}
		// Has explicit type — leave as-is (could be int, byte, etc.)
		if vs.Type != nil {
			continue
		}
		for _, val := range vs.Values {
			if isStringExpr(val) {
				hasString = true
			}
		}
	}
	if !hasString {
		return
	}
	// Convert string-only specs to var with []byte wrapping.
	// Keep non-string specs as const.
	gd.Tok = token.VAR
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		allString := true
		for _, val := range vs.Values {
			if !isStringExpr(val) {
				allString = false
				break
			}
		}
		if !allString || vs.Type != nil {
			// Non-string spec in a now-var block. This is OK —
			// Go allows mixed const/var blocks, and var init from
			// literal is valid.
			continue
		}
		for i, val := range vs.Values {
			if wrapped := wrapStringExpr(val); wrapped != nil {
				vs.Values[i] = wrapped
			}
		}
		// Update the Object.Kind so the type checker sees these as
		// variables, not constants. The parser sets Kind=Con for const
		// declarations and the type checker uses this.
		for _, name := range vs.Names {
			if name.Obj != nil {
				name.Obj.Kind = ast.Var
			}
		}
	}
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
	return !isMoxieStringTarget(ident.Name)
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

// pipeRewrite records a | expression that should become __moxie_concat.
type pipeRewrite struct {
	parent ast.Node
	expr   *ast.BinaryExpr
}

// findPipeConcat walks the AST and finds | and + expressions where operands
// are []byte, using type information from a completed typecheck pass.
// Catches both explicit | (pipe concat) and + (string concat that wasn't
// converted by mxpurify).
func findPipeConcat(files []*ast.File, info *types.Info) []pipeRewrite {
	var rewrites []pipeRewrite
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			bin, ok := n.(*ast.BinaryExpr)
			if !ok || (bin.Op != token.OR && bin.Op != token.ADD) {
				return true
			}
			// Check if operands are []byte. Use both type info and syntactic
			// detection (the []byte(...) wrapper pattern from string literal
			// rewrite), since type info may be incomplete after errors.
			xType := info.TypeOf(bin.X)
			yType := info.TypeOf(bin.Y)
			xOk := (xType != nil && isByteSlice(xType)) || isSliceByteConversion(bin.X)
			yOk := (yType != nil && isByteSlice(yType)) || isSliceByteConversion(bin.Y)
			// For | (pipe), also detect nested | as likely byte concat —
			// but only if the OTHER side is confirmed []byte. Otherwise
			// we'd false-positive on integer bitwise OR chains.
			if bin.Op == token.OR {
				if !xOk && yOk {
					if inner, ok := bin.X.(*ast.BinaryExpr); ok && inner.Op == token.OR {
						xOk = true
					}
				}
				if !yOk && xOk {
					if inner, ok := bin.Y.(*ast.BinaryExpr); ok && inner.Op == token.OR {
						yOk = true
					}
				}
			}
			if xOk && yOk {
				rewrites = append(rewrites, pipeRewrite{expr: bin})
			}
			return true
		})
	}
	return rewrites
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

// applyPipeRewrites replaces | binary expressions with __moxie_concat calls.
// It walks the AST and replaces matching BinaryExpr nodes in-place.
func applyPipeRewrites(files []*ast.File, rewrites []pipeRewrite) {
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
	// Replace: a | b → __moxie_concat(a, b)
	return &ast.CallExpr{
		Fun:  &ast.Ident{Name: "__moxie_concat"},
		Args: []ast.Expr{bin.X, bin.Y},
	}
}

// filterPipeErrors removes type errors about | on []byte from the error list.
func filterPipeErrors(errs []error) []error {
	var filtered []error
	for _, err := range errs {
		msg := err.Error()
		// Go type checker error for | on slices looks like:
		// "invalid operation: ... (operator | not defined on ...)"
		if strings.Contains(msg, "operator |") && strings.Contains(msg, "[]") {
			continue
		}
		// Also filter "operator | not defined on untyped string" which can
		// happen when | is used between string literals before the string
		// rewrite converts them to []byte.
		if strings.Contains(msg, "operator |") && strings.Contains(msg, "string") {
			continue
		}
		// Filter "operator + not defined on []byte" — unconverted string concat.
		if strings.Contains(msg, "operator +") && strings.Contains(msg, "[]byte") {
			continue
		}
		// Filter mismatched types from + between []byte and string.
		if strings.Contains(msg, "mismatched types") && strings.Contains(msg, "operator +") {
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
func findByteComparisons(files []*ast.File, info *types.Info) []*ast.BinaryExpr {
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
			if isByteSlice(xType) || isByteSlice(yType) {
				result = append(result, bin)
			}
			return true
		})
	}
	return result
}

// applyByteComparisonRewrites replaces []byte comparison expressions with
// __moxie_eq / __moxie_lt function calls.
func applyByteComparisonRewrites(files []*ast.File, exprs []*ast.BinaryExpr) {
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
func findByteSwitches(files []*ast.File, info *types.Info) []*ast.SwitchStmt {
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
func applyByteSwitchRewrites(switches []*ast.SwitchStmt) {
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

// filterByteCompareErrors removes type errors about []byte comparison.
func filterByteCompareErrors(errs []error) []error {
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
