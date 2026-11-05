package loader

import (
	"go/ast"
	"go/token"
)

// rewritePopStatements turns a statement-position pop into an explicit
// resize assignment:
//
//	pop(s)        ->  s = resize(s, len(s)-1)
//	pop(s, xs...) ->  s = resize(s, len(s)-len(xs))
//
// pop returns an element, so only the statement form means "shorten the
// slice"; the second form is the symmetric counterpart of push(s, xs...) and
// removes as many elements as xs holds. The rewrite runs before the first
// type-check, so the checker and the SSA builder only ever see the resize
// builtin, which already has the statement store-back. Stage4's front end
// implements the same shrink directly (emitPopShrink).
func rewritePopStatements(files []*ast.File) (n int) {
	for _, f := range files {
		ast.Inspect(f, func(node ast.Node) bool {
			blk, ok := node.(*ast.BlockStmt)
			if !ok {
				return true
			}
			for i, st := range blk.List {
				es, ok := st.(*ast.ExprStmt)
				if !ok {
					continue
				}
				call, ok := es.X.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 || len(call.Args) > 2 {
					continue
				}
				fun, ok := call.Fun.(*ast.Ident)
				if !ok || fun.Name != "pop" {
					continue
				}
				if len(call.Args) == 2 && !call.Ellipsis.IsValid() {
					continue
				}
				target := call.Args[0]
				if !assignableExpr(target) {
					continue
				}
				var count ast.Expr
				lenPos := call.Lparen
				if len(call.Args) == 2 {
					count = &ast.CallExpr{Fun: ast.NewIdent("len"), Lparen: lenPos, Args: []ast.Expr{call.Args[1]}}
				} else {
					one := &ast.BasicLit{Kind: token.INT, Value: "1", ValuePos: lenPos}
					count = one
				}
				lenSelf := &ast.CallExpr{Fun: ast.NewIdent("len"), Lparen: lenPos, Args: []ast.Expr{target}}
				sub := &ast.BinaryExpr{X: lenSelf, Op: token.SUB, Y: count}
				resize := &ast.CallExpr{Fun: ast.NewIdent("resize"), Lparen: lenPos, Args: []ast.Expr{target, sub}}
				blk.List[i] = &ast.AssignStmt{Lhs: []ast.Expr{target}, Tok: token.ASSIGN, TokPos: call.Pos(), Rhs: []ast.Expr{resize}}
				n++
			}
			return true
		})
	}
	return n
}

// assignableExpr reports whether e can appear on the left of an assignment,
// which is exactly what pop's slice argument must be for the statement form to
// have anywhere to store the shortened header.
func assignableExpr(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name != "_"
	case *ast.SelectorExpr:
		return true
	case *ast.IndexExpr:
		return true
	case *ast.StarExpr:
		return true
	}
	return false
}
