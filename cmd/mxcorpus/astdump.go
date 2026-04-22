package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// dumpAST produces a compact text representation of a declaration's AST.
// Format: indented tree with node types and key identifiers.
func dumpAST(fset *token.FileSet, decl ast.Decl) string {
	var b strings.Builder
	dumpNode(&b, fset, decl, 0)
	return b.String()
}

func dumpNode(b *strings.Builder, fset *token.FileSet, n ast.Node, depth int) {
	if n == nil {
		return
	}
	indent := strings.Repeat("  ", depth)

	switch x := n.(type) {
	case *ast.FuncDecl:
		recv := ""
		if x.Recv != nil && len(x.Recv.List) > 0 {
			recv = " recv=" + exprStr(x.Recv.List[0].Type)
		}
		fmt.Fprintf(b, "%sFuncDecl %s%s\n", indent, x.Name.Name, recv)
		if x.Type != nil {
			dumpFuncType(b, fset, x.Type, depth+1)
		}
		if x.Body != nil {
			dumpBlock(b, fset, x.Body, depth+1)
		}

	case *ast.GenDecl:
		fmt.Fprintf(b, "%sGenDecl %s\n", indent, x.Tok)
		for _, spec := range x.Specs {
			dumpSpec(b, fset, spec, depth+1)
		}

	default:
		fmt.Fprintf(b, "%s%T\n", indent, n)
	}
}

func dumpFuncType(b *strings.Builder, fset *token.FileSet, ft *ast.FuncType, depth int) {
	indent := strings.Repeat("  ", depth)
	if ft.TypeParams != nil && len(ft.TypeParams.List) > 0 {
		fmt.Fprintf(b, "%sTypeParams\n", indent)
		for _, f := range ft.TypeParams.List {
			dumpField(b, f, depth+1)
		}
	}
	if ft.Params != nil && len(ft.Params.List) > 0 {
		fmt.Fprintf(b, "%sParams\n", indent)
		for _, f := range ft.Params.List {
			dumpField(b, f, depth+1)
		}
	}
	if ft.Results != nil && len(ft.Results.List) > 0 {
		fmt.Fprintf(b, "%sResults\n", indent)
		for _, f := range ft.Results.List {
			dumpField(b, f, depth+1)
		}
	}
}

func dumpField(b *strings.Builder, f *ast.Field, depth int) {
	indent := strings.Repeat("  ", depth)
	names := ""
	for i, n := range f.Names {
		if i > 0 {
			names += ","
		}
		names += n.Name
	}
	if names != "" {
		fmt.Fprintf(b, "%s%s %s\n", indent, names, exprStr(f.Type))
	} else {
		fmt.Fprintf(b, "%s%s\n", indent, exprStr(f.Type))
	}
}

func dumpSpec(b *strings.Builder, fset *token.FileSet, spec ast.Spec, depth int) {
	indent := strings.Repeat("  ", depth)
	switch s := spec.(type) {
	case *ast.ImportSpec:
		path := ""
		if s.Path != nil {
			path = s.Path.Value
		}
		name := ""
		if s.Name != nil {
			name = s.Name.Name + " "
		}
		fmt.Fprintf(b, "%sImport %s%s\n", indent, name, path)
	case *ast.TypeSpec:
		fmt.Fprintf(b, "%sType %s %s\n", indent, s.Name.Name, exprStr(s.Type))
		if st, ok := s.Type.(*ast.StructType); ok && st.Fields != nil {
			for _, f := range st.Fields.List {
				dumpField(b, f, depth+1)
			}
		}
		if it, ok := s.Type.(*ast.InterfaceType); ok && it.Methods != nil {
			for _, f := range it.Methods.List {
				dumpField(b, f, depth+1)
			}
		}
	case *ast.ValueSpec:
		names := ""
		for i, n := range s.Names {
			if i > 0 {
				names += ","
			}
			names += n.Name
		}
		typ := ""
		if s.Type != nil {
			typ = " " + exprStr(s.Type)
		}
		fmt.Fprintf(b, "%sValue %s%s\n", indent, names, typ)
	}
}

func dumpBlock(b *strings.Builder, fset *token.FileSet, block *ast.BlockStmt, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(b, "%sBlock\n", indent)
	for _, stmt := range block.List {
		dumpStmt(b, fset, stmt, depth+1)
	}
}

func dumpStmt(b *strings.Builder, fset *token.FileSet, stmt ast.Stmt, depth int) {
	indent := strings.Repeat("  ", depth)
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		refs := collectRefs(s.Results...)
		if len(refs) > 0 {
			fmt.Fprintf(b, "%sReturn [%s]\n", indent, strings.Join(refs, ","))
		} else {
			fmt.Fprintf(b, "%sReturn\n", indent)
		}
	case *ast.AssignStmt:
		lhs := exprListStr(s.Lhs)
		allExprs := append(s.Lhs, s.Rhs...)
		refs := collectRefs(allExprs...)
		if len(refs) > 0 {
			fmt.Fprintf(b, "%sAssign %s %s [%s]\n", indent, lhs, s.Tok, strings.Join(refs, ","))
		} else {
			fmt.Fprintf(b, "%sAssign %s %s\n", indent, lhs, s.Tok)
		}
	case *ast.ExprStmt:
		refs := collectRefs(s.X)
		if len(refs) > 0 {
			fmt.Fprintf(b, "%sExpr %s [%s]\n", indent, exprStr(s.X), strings.Join(refs, ","))
		} else {
			fmt.Fprintf(b, "%sExpr %s\n", indent, exprStr(s.X))
		}
	case *ast.IfStmt:
		var allExprs []ast.Expr
		if s.Init != nil {
			if as, ok := s.Init.(*ast.AssignStmt); ok {
				allExprs = append(allExprs, as.Lhs...)
				allExprs = append(allExprs, as.Rhs...)
			}
		}
		allExprs = append(allExprs, s.Cond)
		refs := collectRefs(allExprs...)
		if len(refs) > 0 {
			fmt.Fprintf(b, "%sIf [%s]\n", indent, strings.Join(refs, ","))
		} else {
			fmt.Fprintf(b, "%sIf\n", indent)
		}
		if s.Body != nil {
			dumpBlock(b, fset, s.Body, depth+1)
		}
		if s.Else != nil {
			fmt.Fprintf(b, "%sElse\n", indent)
			dumpStmt(b, fset, s.Else, depth+1)
		}
	case *ast.ForStmt:
		var allExprs []ast.Expr
		if s.Init != nil {
			if as, ok := s.Init.(*ast.AssignStmt); ok {
				allExprs = append(allExprs, as.Lhs...)
				allExprs = append(allExprs, as.Rhs...)
			}
		}
		allExprs = append(allExprs, s.Cond)
		if s.Post != nil {
			if as, ok := s.Post.(*ast.AssignStmt); ok {
				allExprs = append(allExprs, as.Lhs...)
				allExprs = append(allExprs, as.Rhs...)
			}
			if inc, ok := s.Post.(*ast.IncDecStmt); ok {
				allExprs = append(allExprs, inc.X)
			}
		}
		refs := collectRefs(allExprs...)
		if len(refs) > 0 {
			fmt.Fprintf(b, "%sFor [%s]\n", indent, strings.Join(refs, ","))
		} else {
			fmt.Fprintf(b, "%sFor\n", indent)
		}
		if s.Body != nil {
			dumpBlock(b, fset, s.Body, depth+1)
		}
	case *ast.RangeStmt:
		allExprs := []ast.Expr{s.X}
		if s.Key != nil {
			allExprs = append(allExprs, s.Key)
		}
		if s.Value != nil {
			allExprs = append(allExprs, s.Value)
		}
		refs := collectRefs(allExprs...)
		rangeStr := exprStr(s.X)
		if len(refs) > 0 {
			fmt.Fprintf(b, "%sRange %s [%s]\n", indent, rangeStr, strings.Join(refs, ","))
		} else {
			fmt.Fprintf(b, "%sRange %s\n", indent, rangeStr)
		}
		if s.Body != nil {
			dumpBlock(b, fset, s.Body, depth+1)
		}
	case *ast.SwitchStmt:
		var allExprs []ast.Expr
		if s.Init != nil {
			if as, ok := s.Init.(*ast.AssignStmt); ok {
				allExprs = append(allExprs, as.Lhs...)
				allExprs = append(allExprs, as.Rhs...)
			}
		}
		allExprs = append(allExprs, s.Tag)
		refs := collectRefs(allExprs...)
		if len(refs) > 0 {
			fmt.Fprintf(b, "%sSwitch [%s]\n", indent, strings.Join(refs, ","))
		} else {
			fmt.Fprintf(b, "%sSwitch\n", indent)
		}
		if s.Body != nil {
			dumpBlock(b, fset, s.Body, depth+1)
		}
	case *ast.TypeSwitchStmt:
		fmt.Fprintf(b, "%sTypeSwitch\n", indent)
		if s.Body != nil {
			dumpBlock(b, fset, s.Body, depth+1)
		}
	case *ast.SelectStmt:
		fmt.Fprintf(b, "%sSelect\n", indent)
		if s.Body != nil {
			dumpBlock(b, fset, s.Body, depth+1)
		}
	case *ast.DeclStmt:
		dumpNode(b, fset, s.Decl, depth)
	case *ast.BlockStmt:
		dumpBlock(b, fset, s, depth)
	case *ast.CaseClause:
		if s.List == nil {
			fmt.Fprintf(b, "%sDefault\n", indent)
		} else {
			refs := collectRefs(s.List...)
			if len(refs) > 0 {
				fmt.Fprintf(b, "%sCase [%s]\n", indent, strings.Join(refs, ","))
			} else {
				fmt.Fprintf(b, "%sCase\n", indent)
			}
		}
		for _, st := range s.Body {
			dumpStmt(b, fset, st, depth+1)
		}
	case *ast.CommClause:
		if s.Comm == nil {
			fmt.Fprintf(b, "%sDefault\n", indent)
		} else {
			fmt.Fprintf(b, "%sComm\n", indent)
		}
		for _, st := range s.Body {
			dumpStmt(b, fset, st, depth+1)
		}
	case *ast.BranchStmt:
		fmt.Fprintf(b, "%s%s\n", indent, s.Tok)
	case *ast.DeferStmt:
		refs := collectRefs(s.Call)
		if len(refs) > 0 {
			fmt.Fprintf(b, "%sDefer %s [%s]\n", indent, exprStr(s.Call.Fun), strings.Join(refs, ","))
		} else {
			fmt.Fprintf(b, "%sDefer %s\n", indent, exprStr(s.Call.Fun))
		}
	case *ast.GoStmt:
		refs := collectRefs(s.Call)
		if len(refs) > 0 {
			fmt.Fprintf(b, "%sGo %s [%s]\n", indent, exprStr(s.Call.Fun), strings.Join(refs, ","))
		} else {
			fmt.Fprintf(b, "%sGo %s\n", indent, exprStr(s.Call.Fun))
		}
	case *ast.SendStmt:
		refs := collectRefs(s.Chan, s.Value)
		if len(refs) > 0 {
			fmt.Fprintf(b, "%sSend %s [%s]\n", indent, exprStr(s.Chan), strings.Join(refs, ","))
		} else {
			fmt.Fprintf(b, "%sSend %s\n", indent, exprStr(s.Chan))
		}
	case *ast.IncDecStmt:
		refs := collectRefs(s.X)
		if len(refs) > 0 {
			fmt.Fprintf(b, "%s%s %s [%s]\n", indent, exprStr(s.X), s.Tok, strings.Join(refs, ","))
		} else {
			fmt.Fprintf(b, "%s%s %s\n", indent, exprStr(s.X), s.Tok)
		}
	case *ast.LabeledStmt:
		fmt.Fprintf(b, "%sLabel %s\n", indent, s.Label.Name)
		dumpStmt(b, fset, s.Stmt, depth+1)
	default:
		fmt.Fprintf(b, "%s%T\n", indent, stmt)
	}
}

func exprStr(e ast.Expr) string {
	if e == nil {
		return ""
	}
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprStr(x.X) + "." + x.Sel.Name
	case *ast.StarExpr:
		return "*" + exprStr(x.X)
	case *ast.ArrayType:
		if x.Len == nil {
			return "[]" + exprStr(x.Elt)
		}
		return "[" + exprStr(x.Len) + "]" + exprStr(x.Elt)
	case *ast.MapType:
		return "map[" + exprStr(x.Key) + "]" + exprStr(x.Value)
	case *ast.ChanType:
		return "chan " + exprStr(x.Value)
	case *ast.FuncType:
		return "func(...)"
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.StructType:
		return "struct{...}"
	case *ast.Ellipsis:
		return "..." + exprStr(x.Elt)
	case *ast.CallExpr:
		return exprStr(x.Fun) + "(...)"
	case *ast.IndexExpr:
		return exprStr(x.X) + "[" + exprStr(x.Index) + "]"
	case *ast.BasicLit:
		return x.Value
	case *ast.UnaryExpr:
		return x.Op.String() + exprStr(x.X)
	case *ast.BinaryExpr:
		return exprStr(x.X) + x.Op.String() + exprStr(x.Y)
	case *ast.ParenExpr:
		return "(" + exprStr(x.X) + ")"
	case *ast.CompositeLit:
		if x.Type != nil {
			return exprStr(x.Type) + "{...}"
		}
		return "{...}"
	case *ast.TypeAssertExpr:
		if x.Type != nil {
			return exprStr(x.X) + ".(" + exprStr(x.Type) + ")"
		}
		return exprStr(x.X) + ".(type)"
	case *ast.SliceExpr:
		return exprStr(x.X) + "[:]"
	case *ast.KeyValueExpr:
		return exprStr(x.Key) + ":" + exprStr(x.Value)
	case *ast.FuncLit:
		return "func(){...}"
	case *ast.IndexListExpr:
		parts := make([]string, len(x.Indices))
		for i, idx := range x.Indices {
			parts[i] = exprStr(idx)
		}
		return exprStr(x.X) + "[" + strings.Join(parts, ",") + "]"
	}
	return fmt.Sprintf("%T", e)
}

var builtinTypes = map[string]bool{
	"bool": true, "byte": true, "int": true, "int8": true, "int16": true,
	"int32": true, "int64": true, "uint": true, "uint8": true, "uint16": true,
	"uint32": true, "uint64": true, "float32": true, "float64": true,
	"string": true, "rune": true, "error": true, "any": true,
	"true": true, "false": true, "nil": true,
	"len": true, "cap": true, "append": true, "copy": true, "delete": true,
	"close": true, "panic": true, "recover": true, "print": true, "println": true,
	"make": true, "new": true,
}

func collectRefs(exprs ...ast.Expr) []string {
	seen := map[string]bool{}
	var refs []string
	for _, e := range exprs {
		walkExpr(e, func(id string) {
			if !seen[id] && !builtinTypes[id] {
				seen[id] = true
				refs = append(refs, id)
			}
		})
	}
	return refs
}

func walkExpr(e ast.Expr, visit func(string)) {
	if e == nil {
		return
	}
	switch x := e.(type) {
	case *ast.Ident:
		visit(x.Name)
	case *ast.SelectorExpr:
		walkExpr(x.X, visit)
		visit(x.Sel.Name)
	case *ast.CallExpr:
		walkExpr(x.Fun, visit)
		for _, a := range x.Args {
			walkExpr(a, visit)
		}
	case *ast.BinaryExpr:
		walkExpr(x.X, visit)
		walkExpr(x.Y, visit)
	case *ast.UnaryExpr:
		walkExpr(x.X, visit)
	case *ast.StarExpr:
		walkExpr(x.X, visit)
	case *ast.ParenExpr:
		walkExpr(x.X, visit)
	case *ast.IndexExpr:
		walkExpr(x.X, visit)
		walkExpr(x.Index, visit)
	case *ast.SliceExpr:
		walkExpr(x.X, visit)
		walkExpr(x.Low, visit)
		walkExpr(x.High, visit)
		walkExpr(x.Max, visit)
	case *ast.CompositeLit:
		walkExpr(x.Type, visit)
		for _, elt := range x.Elts {
			walkExpr(elt, visit)
		}
	case *ast.KeyValueExpr:
		walkExpr(x.Key, visit)
		walkExpr(x.Value, visit)
	case *ast.TypeAssertExpr:
		walkExpr(x.X, visit)
		walkExpr(x.Type, visit)
	case *ast.FuncLit:
		if x.Body != nil {
			for _, stmt := range x.Body.List {
				walkStmtRefs(stmt, visit)
			}
		}
	case *ast.ArrayType:
		walkExpr(x.Elt, visit)
		walkExpr(x.Len, visit)
	case *ast.MapType:
		walkExpr(x.Key, visit)
		walkExpr(x.Value, visit)
	case *ast.IndexListExpr:
		walkExpr(x.X, visit)
		for _, idx := range x.Indices {
			walkExpr(idx, visit)
		}
	}
}

func walkStmtRefs(stmt ast.Stmt, visit func(string)) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		for _, e := range s.Lhs {
			walkExpr(e, visit)
		}
		for _, e := range s.Rhs {
			walkExpr(e, visit)
		}
	case *ast.ReturnStmt:
		for _, e := range s.Results {
			walkExpr(e, visit)
		}
	case *ast.ExprStmt:
		walkExpr(s.X, visit)
	case *ast.IfStmt:
		if s.Init != nil {
			walkStmtRefs(s.Init, visit)
		}
		walkExpr(s.Cond, visit)
		if s.Body != nil {
			for _, st := range s.Body.List {
				walkStmtRefs(st, visit)
			}
		}
		if s.Else != nil {
			walkStmtRefs(s.Else, visit)
		}
	case *ast.ForStmt:
		if s.Init != nil {
			walkStmtRefs(s.Init, visit)
		}
		walkExpr(s.Cond, visit)
		if s.Post != nil {
			walkStmtRefs(s.Post, visit)
		}
		if s.Body != nil {
			for _, st := range s.Body.List {
				walkStmtRefs(st, visit)
			}
		}
	case *ast.RangeStmt:
		walkExpr(s.Key, visit)
		walkExpr(s.Value, visit)
		walkExpr(s.X, visit)
		if s.Body != nil {
			for _, st := range s.Body.List {
				walkStmtRefs(st, visit)
			}
		}
	case *ast.BlockStmt:
		for _, st := range s.List {
			walkStmtRefs(st, visit)
		}
	case *ast.SwitchStmt:
		if s.Init != nil {
			walkStmtRefs(s.Init, visit)
		}
		walkExpr(s.Tag, visit)
		if s.Body != nil {
			for _, st := range s.Body.List {
				walkStmtRefs(st, visit)
			}
		}
	case *ast.TypeSwitchStmt:
		if s.Init != nil {
			walkStmtRefs(s.Init, visit)
		}
		walkStmtRefs(s.Assign, visit)
		if s.Body != nil {
			for _, st := range s.Body.List {
				walkStmtRefs(st, visit)
			}
		}
	case *ast.CaseClause:
		for _, e := range s.List {
			walkExpr(e, visit)
		}
		for _, st := range s.Body {
			walkStmtRefs(st, visit)
		}
	case *ast.CommClause:
		walkStmtRefs(s.Comm, visit)
		for _, st := range s.Body {
			walkStmtRefs(st, visit)
		}
	case *ast.SelectStmt:
		if s.Body != nil {
			for _, st := range s.Body.List {
				walkStmtRefs(st, visit)
			}
		}
	case *ast.DeferStmt:
		walkExpr(s.Call.Fun, visit)
		for _, a := range s.Call.Args {
			walkExpr(a, visit)
		}
	case *ast.GoStmt:
		walkExpr(s.Call.Fun, visit)
		for _, a := range s.Call.Args {
			walkExpr(a, visit)
		}
	case *ast.SendStmt:
		walkExpr(s.Chan, visit)
		walkExpr(s.Value, visit)
	case *ast.IncDecStmt:
		walkExpr(s.X, visit)
	case *ast.DeclStmt:
		if gd, ok := s.Decl.(*ast.GenDecl); ok {
			for _, sp := range gd.Specs {
				if vs, ok := sp.(*ast.ValueSpec); ok {
					for _, n := range vs.Names {
						visit(n.Name)
					}
					for _, v := range vs.Values {
						walkExpr(v, visit)
					}
				}
			}
		}
	case *ast.LabeledStmt:
		walkStmtRefs(s.Stmt, visit)
	}
}

func exprListStr(exprs []ast.Expr) string {
	parts := make([]string, len(exprs))
	for i, e := range exprs {
		parts[i] = exprStr(e)
	}
	return strings.Join(parts, ",")
}
