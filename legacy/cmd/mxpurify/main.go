// mxpurify rewrites .mx source files from Go syntax to Moxie syntax.
//
// Transformations:
//   - string type → []byte
//   - int type → int32 (not int8/16/64)
//   - uint type → uint32 (not uint8/16/64)
//   - make(chan T) → chan T{}, make(chan T, n) → chan T{n}
//   - make(map[K]V) → map[K]V{}, make(map[K]V, n) → map[K]V{}
//   - make([]T, len) → []T{:len}, make([]T, len, cap) → []T{:len:cap}
//   - new(T) → &T{}
//
// Usage: mxpurify [-dry-run] [-v] file.mx [file2.mx ...]
//        mxpurify [-dry-run] [-v] -dir /path/to/package
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

var (
	dryRun   = flag.Bool("dry-run", false, "print changes without writing")
	verbose  = flag.Bool("v", false, "verbose output")
	dir      = flag.String("dir", "", "rewrite all .mx files in directory")
	noString = flag.Bool("no-string", false, "skip string→[]byte conversion (only do make/new)")
)

func main() {
	flag.Parse()

	var files []string
	if *dir != "" {
		entries, err := os.ReadDir(*dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".mx") && !strings.HasSuffix(e.Name(), "_test.mx") {
				files = append(files, filepath.Join(*dir, e.Name()))
			}
		}
	}
	files = append(files, flag.Args()...)

	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: mxpurify [-dry-run] [-v] [-dir DIR] [file.mx ...]")
		os.Exit(1)
	}

	totalChanges := 0
	for _, f := range files {
		n, err := rewriteFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
			continue
		}
		totalChanges += n
	}
	fmt.Printf("total: %d changes across %d files\n", totalChanges, len(files))
}

func rewriteFile(path string) (int, error) {
	fset := token.NewFileSet()
	// Parse as .go since .mx is syntactically Go.
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return 0, fmt.Errorf("parse: %w", err)
	}

	changes := 0

	// Walk AST and apply transformations.
	changes += rewriteTypes(file)
	changes += rewriteMakeCalls(file, fset)
	changes += rewriteNewCalls(file)

	// Write AST to buffer via go/printer.
	var buf strings.Builder
	cfg := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&buf, fset, file); err != nil {
		return 0, fmt.Errorf("print: %w", err)
	}

	// Text-level pass: make([]T, len) → []T{:len}, make([]T, len, cap) → []T{:len:cap}
	output := buf.String()
	output, sliceChanges := rewriteSliceMakes(output)
	changes += sliceChanges

	if changes == 0 {
		if *verbose {
			fmt.Printf("%s: no changes\n", path)
		}
		return 0, nil
	}

	if *dryRun {
		fmt.Printf("%s: %d changes (dry run)\n", path, changes)
		return changes, nil
	}

	if err := os.WriteFile(path, []byte(output), 0644); err != nil {
		return 0, err
	}

	if *verbose {
		fmt.Printf("%s: %d changes\n", path, changes)
	}
	return changes, nil
}

// rewriteTypes replaces type identifiers: string→[]byte.
// int/uint conversion is deferred until the compiler can alias int=int32.
func rewriteTypes(file *ast.File) int {
	if *noString {
		return 0
	}
	// Collect interface body positions to skip type constraints.
	ifaceRanges := collectInterfaceRanges(file)

	changes := 0
	ast.Inspect(file, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		// Skip idents inside interface type declarations (type constraints
		// like ~int | ~string). These can't be mechanically rewritten
		// because ~[]byte is invalid Go and ~int32 creates duplicates.
		if isInRanges(ident.Pos(), ifaceRanges) {
			return true
		}

		// Note: int/uint conversion is deferred until the compiler can
		// alias int=int32 in the type checker. len()/cap()/copy() return
		// int, so converting int→int32 creates pervasive type mismatches.
		return true
	})

	changes += rewriteStringType(file, ifaceRanges)
	return changes
}

type posRange struct{ start, end token.Pos }

func collectInterfaceRanges(file *ast.File) []posRange {
	var ranges []posRange
	ast.Inspect(file, func(n ast.Node) bool {
		iface, ok := n.(*ast.InterfaceType)
		if ok && iface.Methods != nil {
			ranges = append(ranges, posRange{
				start: iface.Methods.Opening,
				end:   iface.Methods.Closing,
			})
		}
		return true
	})
	return ranges
}

func isInRanges(pos token.Pos, ranges []posRange) bool {
	for _, r := range ranges {
		if pos >= r.start && pos <= r.end {
			return true
		}
	}
	return false
}

// collectExemptStringPositions finds string idents in Error()/String()
// return types that must stay string (Go built-in interface satisfaction).
// Also collects string idents inside the bodies of these methods so that
// string() conversions needed at the interface boundary are preserved.
func collectExemptStringPositions(file *ast.File) map[token.Pos]bool {
	exempt := make(map[token.Pos]bool)
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil {
			continue
		}
		switch fd.Name.Name {
		case "Error", "String":
			if fd.Type.Params != nil && len(fd.Type.Params.List) > 0 {
				continue
			}
			if fd.Type.Results == nil || len(fd.Type.Results.List) != 1 {
				continue
			}
			// Exempt the return type.
			if ident, ok := fd.Type.Results.List[0].Type.(*ast.Ident); ok && ident.Name == "string" {
				exempt[ident.NamePos] = true
			}
			// Exempt all string idents in the method body (string() conversions
			// are needed to satisfy the string return type).
			if fd.Body != nil {
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					if ident, ok := n.(*ast.Ident); ok && ident.Name == "string" {
						exempt[ident.NamePos] = true
					}
					return true
				})
			}
		}
	}
	return exempt
}

// isTypePosition heuristically checks if an ident is used as a type (not a
// conversion call, not part of a longer name, etc.).
// This uses parent-node analysis to distinguish type positions.
func isTypePosition(ident *ast.Ident, file *ast.File) bool {
	// Walk the AST to find the parent of this ident.
	parent := findParent(file, ident)
	if parent == nil {
		return false
	}

	switch p := parent.(type) {
	case *ast.Field:
		// Function parameters, struct fields, return types.
		return p.Type == ident
	case *ast.ValueSpec:
		// var x int
		return p.Type == ident
	case *ast.ArrayType:
		// []int, [N]int
		return p.Elt == ident
	case *ast.MapType:
		// map[int]V or map[K]int
		return p.Key == ident || p.Value == ident
	case *ast.ChanType:
		// chan int
		return p.Value == ident
	case *ast.StarExpr:
		// *int
		return p.X == ident
	case *ast.TypeSpec:
		// type X int
		return p.Type == ident
	case *ast.CompositeLit:
		// T{...} — the type of a composite literal
		return p.Type == ident
	case *ast.FuncType:
		// standalone type usage in func type
		return false
	case *ast.UnaryExpr:
		// ~int in type constraints
		if p.Op == token.TILDE {
			return true
		}
	case *ast.CallExpr:
		// int(x) — type conversion. We want to change this too.
		return p.Fun == ident
	case *ast.BinaryExpr:
		// int | uint in type constraints
		return true
	case *ast.InterfaceType:
		return false
	case *ast.Ellipsis:
		// ...int (variadic)
		return p.Elt == ident
	case *ast.CaseClause:
		// case int: in type switch
		for _, e := range p.List {
			if e == ident {
				return true
			}
		}
	case *ast.TypeAssertExpr:
		// x.(int)
		return p.Type == ident
	}
	return false
}

// findParent returns the immediate parent node of target in the AST.
func findParent(root ast.Node, target ast.Node) ast.Node {
	var parent ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil || parent != nil {
			return false
		}
		// Check all child nodes.
		found := false
		ast.Inspect(n, func(child ast.Node) bool {
			if child == n {
				return true // skip self
			}
			if child == target {
				found = true
				return false
			}
			return false // don't recurse — we only want direct children
		})
		if found {
			parent = n
			return false
		}
		return true
	})
	return parent
}

// rewriteStringType replaces string idents with []byte in type positions.
// This requires parent-node replacement since we change node type (Ident → ArrayType).
func rewriteStringType(file *ast.File, ifaceRanges []posRange) int {
	// Collect positions of return type fields in Error()/String() methods.
	// These must stay string to satisfy Go's built-in interfaces.
	exemptPositions := collectExemptStringPositions(file)

	changes := 0
	byteSlice := func(pos token.Pos) *ast.ArrayType {
		return &ast.ArrayType{
			Lbrack: pos,
			Elt:    &ast.Ident{Name: "byte", NamePos: pos},
		}
	}

	// Walk fields (function params, returns, struct fields).
	ast.Inspect(file, func(n ast.Node) bool {
		// Skip nodes inside interface bodies (type constraints).
		if n != nil {
			if pos := n.Pos(); pos.IsValid() && isInRanges(pos, ifaceRanges) {
				return false
			}
		}
		switch node := n.(type) {
		case *ast.Field:
			if ident, ok := node.Type.(*ast.Ident); ok && ident.Name == "string" {
				if exemptPositions[ident.NamePos] {
					return true // Keep string for Error()/String() returns
				}
				node.Type = byteSlice(ident.NamePos)
				changes++
			}
		case *ast.ValueSpec:
			if ident, ok := node.Type.(*ast.Ident); ok && ident.Name == "string" && !exemptPositions[ident.NamePos] {
				node.Type = byteSlice(ident.NamePos)
				changes++
			}
		case *ast.ArrayType:
			if ident, ok := node.Elt.(*ast.Ident); ok && ident.Name == "string" && !exemptPositions[ident.NamePos] {
				node.Elt = byteSlice(ident.NamePos)
				changes++
			}
		case *ast.MapType:
			// Don't change map keys from string — maps need string keys internally.
			// Only change map values.
			if ident, ok := node.Value.(*ast.Ident); ok && ident.Name == "string" && !exemptPositions[ident.NamePos] {
				node.Value = byteSlice(ident.NamePos)
				changes++
			}
		case *ast.ChanType:
			if ident, ok := node.Value.(*ast.Ident); ok && ident.Name == "string" && !exemptPositions[ident.NamePos] {
				node.Value = byteSlice(ident.NamePos)
				changes++
			}
		case *ast.StarExpr:
			if ident, ok := node.X.(*ast.Ident); ok && ident.Name == "string" && !exemptPositions[ident.NamePos] {
				node.X = byteSlice(ident.NamePos)
				changes++
			}
		case *ast.TypeSpec:
			if ident, ok := node.Type.(*ast.Ident); ok && ident.Name == "string" && !exemptPositions[ident.NamePos] {
				node.Type = byteSlice(ident.NamePos)
				changes++
			}
		case *ast.CallExpr:
			// string(x) → []byte(x) — type conversion (skip inside Error/String methods)
			if ident, ok := node.Fun.(*ast.Ident); ok && ident.Name == "string" && !exemptPositions[ident.NamePos] {
				node.Fun = byteSlice(ident.NamePos)
				changes++
			}
		case *ast.Ellipsis:
			// ...string (variadic params)
			if ident, ok := node.Elt.(*ast.Ident); ok && ident.Name == "string" && !exemptPositions[ident.NamePos] {
				node.Elt = byteSlice(ident.NamePos)
				changes++
			}
		case *ast.CaseClause:
			// case string: (type switches)
			for i, expr := range node.List {
				if ident, ok := expr.(*ast.Ident); ok && ident.Name == "string" && !exemptPositions[ident.NamePos] {
					node.List[i] = byteSlice(ident.NamePos)
					changes++
				}
			}
		case *ast.TypeAssertExpr:
			// x.(string) → x.([]byte)
			if ident, ok := node.Type.(*ast.Ident); ok && ident.Name == "string" && !exemptPositions[ident.NamePos] {
				node.Type = byteSlice(ident.NamePos)
				changes++
			}
		}
		return true
	})
	return changes
}

// rewriteMakeCalls rewrites make() calls to Moxie literal syntax.
func rewriteMakeCalls(file *ast.File, fset *token.FileSet) int {
	changes := 0
	rewriteInExprs(file, func(expr ast.Expr) ast.Expr {
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return nil
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "make" {
			return nil
		}
		if len(call.Args) < 1 {
			return nil
		}

		typeArg := call.Args[0]

		switch t := typeArg.(type) {
		case *ast.ChanType:
			// make(chan T) → chan T{}, make(chan T, n) → chan T{n}
			changes++
			if len(call.Args) >= 2 {
				return &ast.CompositeLit{
					Type: t,
					Elts: []ast.Expr{call.Args[1]},
				}
			}
			return &ast.CompositeLit{Type: t}

		case *ast.MapType:
			// make(map[K]V) → map[K]V{}, make(map[K]V, n) → map[K]V{}
			// Drop capacity hint.
			changes++
			return &ast.CompositeLit{Type: t}

		case *ast.ArrayType:
			if t.Len != nil {
				return nil // [N]T, not a slice
			}
			// make([]T, len) or make([]T, len, cap)
			// We can't represent []T{:len} in the Go AST since it's not valid Go.
			// Instead, keep the make() call but print a TODO comment.
			// The text-level rewrite in the compiler handles the Moxie syntax.
			//
			// For now, leave slice makes as-is — they'll be rewritten
			// by the compiler's text-level pass when we convert them to
			// []T{:len} syntax manually or in a second pass.
			return nil
		}
		return nil
	})
	return changes
}

// rewriteSliceMakes converts make([]T, len) and make([]T, len, cap) in text.
// Returns modified text and number of changes. Uses tokenizer to match:
//   make([]T, len)      → []T{:len}
//   make([]T, len, cap) → []T{:len:cap}
func rewriteSliceMakes(src string) (string, int) {
	fset := token.NewFileSet()
	srcBytes := []byte(src)
	file := fset.AddFile("", fset.Base(), len(srcBytes))
	var s scanner.Scanner
	s.Init(file, srcBytes, nil, scanner.ScanComments)

	type tok struct {
		pos int
		end int
		tok token.Token
		lit string
	}
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

	var result strings.Builder
	lastEnd := 0
	changes := 0

	for i := 0; i < len(toks); i++ {
		// Look for: make LPAREN LBRACK RBRACK ...
		if toks[i].tok != token.IDENT || toks[i].lit != "make" {
			continue
		}
		if i+3 >= len(toks) {
			continue
		}
		if toks[i+1].tok != token.LPAREN {
			continue
		}
		if toks[i+2].tok != token.LBRACK {
			continue
		}
		if toks[i+3].tok != token.RBRACK {
			continue
		}

		makeIdx := i
		lparenIdx := i + 1
		lbrackIdx := i + 2

		// Find the matching close paren for the make call.
		closeParenIdx := -1
		depth := 1
		for j := lparenIdx + 1; j < len(toks); j++ {
			switch toks[j].tok {
			case token.LPAREN:
				depth++
			case token.RPAREN:
				depth--
				if depth == 0 {
					closeParenIdx = j
				}
			}
			if closeParenIdx >= 0 {
				break
			}
		}
		if closeParenIdx < 0 {
			continue
		}

		// Find commas at depth 1 (inside the make call, not nested).
		// make([]T, len) has 1 comma, make([]T, len, cap) has 2.
		var commaPositions []int
		depth = 0
		for j := lparenIdx + 1; j < closeParenIdx; j++ {
			switch toks[j].tok {
			case token.LPAREN, token.LBRACK, token.LBRACE:
				depth++
			case token.RPAREN, token.RBRACK, token.RBRACE:
				depth--
			case token.COMMA:
				if depth == 0 {
					commaPositions = append(commaPositions, j)
				}
			}
		}

		if len(commaPositions) < 1 || len(commaPositions) > 2 {
			continue // not make([]T, len) or make([]T, len, cap)
		}

		// Extract slice type: everything from [ to just before first comma.
		typeText := strings.TrimSpace(string(srcBytes[toks[lbrackIdx].pos:toks[commaPositions[0]].pos]))

		if len(commaPositions) == 1 {
			// make([]T, len) → []T{:len}
			lenText := strings.TrimSpace(string(srcBytes[toks[commaPositions[0]].end:toks[closeParenIdx].pos]))
			result.Write(srcBytes[lastEnd:toks[makeIdx].pos])
			result.WriteString(typeText)
			result.WriteString("{:")
			result.WriteString(lenText)
			result.WriteString("}")
			changes++
		} else {
			// make([]T, len, cap) → []T{:len:cap}
			lenText := strings.TrimSpace(string(srcBytes[toks[commaPositions[0]].end:toks[commaPositions[1]].pos]))
			capText := strings.TrimSpace(string(srcBytes[toks[commaPositions[1]].end:toks[closeParenIdx].pos]))
			result.Write(srcBytes[lastEnd:toks[makeIdx].pos])
			result.WriteString(typeText)
			result.WriteString("{:")
			result.WriteString(lenText)
			result.WriteString(":")
			result.WriteString(capText)
			result.WriteString("}")
			changes++
		}

		lastEnd = toks[closeParenIdx].end
		i = closeParenIdx
	}

	if lastEnd == 0 {
		return src, 0
	}
	result.Write(srcBytes[lastEnd:])
	return result.String(), changes
}

// rewriteNewCalls rewrites new(T) to &T{}.
func rewriteNewCalls(file *ast.File) int {
	changes := 0
	rewriteInExprs(file, func(expr ast.Expr) ast.Expr {
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return nil
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "new" || len(call.Args) != 1 {
			return nil
		}
		changes++
		return &ast.UnaryExpr{
			Op: token.AND,
			X: &ast.CompositeLit{
				Type: call.Args[0],
			},
		}
	})
	return changes
}

// rewriteInExprs walks the AST and replaces expressions using a transform function.
// If transform returns non-nil, the expression is replaced.
func rewriteInExprs(file *ast.File, transform func(ast.Expr) ast.Expr) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range node.Rhs {
				if repl := transform(rhs); repl != nil {
					node.Rhs[i] = repl
				}
			}
		case *ast.ValueSpec:
			for i, val := range node.Values {
				if repl := transform(val); repl != nil {
					node.Values[i] = repl
				}
			}
		case *ast.ReturnStmt:
			for i, result := range node.Results {
				if repl := transform(result); repl != nil {
					node.Results[i] = repl
				}
			}
		case *ast.CallExpr:
			for i, arg := range node.Args {
				if repl := transform(arg); repl != nil {
					node.Args[i] = repl
				}
			}
			if repl := transform(node.Fun); repl != nil {
				// Unlikely but handle func-position make/new
			}
		case *ast.SendStmt:
			if repl := transform(node.Value); repl != nil {
				node.Value = repl
			}
		case *ast.ExprStmt:
			if repl := transform(node.X); repl != nil {
				node.X = repl
			}
		case *ast.KeyValueExpr:
			if repl := transform(node.Value); repl != nil {
				node.Value = repl
			}
		case *ast.CompositeLit:
			for i, elt := range node.Elts {
				if repl := transform(elt); repl != nil {
					node.Elts[i] = repl
				}
			}
		case *ast.BinaryExpr:
			if repl := transform(node.X); repl != nil {
				node.X = repl
			}
			if repl := transform(node.Y); repl != nil {
				node.Y = repl
			}
		case *ast.UnaryExpr:
			if repl := transform(node.X); repl != nil {
				node.X = repl
			}
		case *ast.ParenExpr:
			if repl := transform(node.X); repl != nil {
				node.X = repl
			}
		case *ast.SelectorExpr:
			// new(T).Method() — new is SelectorExpr.X
			if repl := transform(node.X); repl != nil {
				node.X = repl
			}
		case *ast.IfStmt:
			// init statement might have assignments
		case *ast.SwitchStmt:
			// init and tag
		case *ast.IndexExpr:
			if repl := transform(node.Index); repl != nil {
				node.Index = repl
			}
		}
		return true
	})
}
