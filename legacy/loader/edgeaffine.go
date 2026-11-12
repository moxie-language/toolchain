package loader

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
)

// checkEdgeAffinity enforces the declared pair's affineness (docs/memory-model.md
// section 4, D19 rule 4): an Edge owns one pair of rings and one pair of
// handles, so it has one owner and moves ONCE. It may be
//
//   - passed to a spawn (the move),
//   - returned from a function (the move out of the frame),
//   - read through its fields (e.Send, e.Recv),
//   - addressed by the declaration's __edgealloc call.
//
// Everything else is a copy: `e2 := e1`, `f(e1)` for a non-spawn f, storing it
// in a struct, slice or channel. Two spawns of one Edge are the same fault in
// the other direction: the pair's handles would have two children.
//
// It runs after the final type-check, on the lowered AST (the declaration is
// already `var p Edge[...]; ... __edgealloc(&p, &c, ...)`), so the vars that
// hold a pair are exactly the Edge-typed definitions.
func checkEdgeAffinity(files []*ast.File, info *types.Info, fset *token.FileSet) []error {
	edges := map[types.Object]bool{}
	for _, obj := range info.Defs {
		if obj == nil {
			continue
		}
		if !isEdgeObjectType(obj.Type()) {
			continue
		}
		if v, ok := obj.(*types.Var); ok && v.IsField() {
			continue
		}
		edges[obj] = true
	}
	if len(edges) == 0 {
		return nil
	}

	// Parent links let the use-site judge reach the enclosing expression
	// without threading context through every walk.
	parents := map[ast.Node]ast.Node{}
	for _, f := range files {
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if len(stack) > 0 {
				parents[n] = stack[len(stack)-1]
			}
			stack = append(stack, n)
			return true
		})
	}

	moved := map[types.Object]ast.Node{}
	var errs []error
	fail := func(pos token.Pos, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", fset.Position(pos), fmt.Sprintf(format, args...)))
	}

	for id, obj := range info.Uses {
		if !edges[obj] {
			continue
		}
		switch p := parents[id].(type) {
		case *ast.SelectorExpr:
			// e.Send / e.Recv: the pair is read, not copied.
			if p.X == id {
				continue
			}
			fail(id.Pos(), "an Edge cannot be copied; pass it to one spawn or return it")
		case *ast.UnaryExpr:
			// &p in the declaration's __edgealloc call.
			if p.Op == token.AND && p.X == id {
				continue
			}
			fail(id.Pos(), "an Edge cannot be copied; pass it to one spawn or return it")
		case *ast.ReturnStmt:
			// The move out of the frame.
			continue
		case *ast.AssignStmt:
			// The LHS of an assignment reuses the name; the RHS would be a copy
			// and falls here too, so only the LHS is permitted.
			isLHS := false
			for _, l := range p.Lhs {
				if l == id {
					isLHS = true
					break
				}
			}
			if isLHS {
				continue
			}
			// `_ = e` discards the value: no second owner is created.
			if len(p.Lhs) == 1 {
				if blank, ok := p.Lhs[0].(*ast.Ident); ok && blank.Name == "_" {
					continue
				}
			}
			fail(id.Pos(), "an Edge cannot be copied; pass it to one spawn or return it")
		case *ast.CallExpr:
			if isSpawnCall(p, info) && len(p.Args) > 1 && containsExpr(p.Args[1:], id) {
				if prev, ok := moved[obj]; ok {
					fail(id.Pos(), "this Edge was already passed to spawn at %s; a pair has one child", fset.Position(prev.Pos()))
					continue
				}
				moved[obj] = id
				continue
			}
			fail(id.Pos(), "an Edge cannot be copied; pass it to one spawn or return it")
		case *ast.DeclStmt, *ast.ValueSpec, *ast.Field:
			// Declaration, not a use.
			continue
		default:
			fail(id.Pos(), "an Edge cannot be copied; pass it to one spawn or return it")
		}
	}

	sort.Slice(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
	return errs
}

// isEdgeObjectType reports whether t is the builtin Edge instantiation.
func isEdgeObjectType(t types.Type) bool {
	if t == nil {
		return false
	}
	n, ok := t.(*types.Named)
	if !ok || n.Obj() == nil || n.Obj().Pkg() != nil {
		return false
	}
	return n.Obj().Name() == "Edge"
}

// isSpawnCall reports whether call invokes the predeclared spawn builtin.
func isSpawnCall(call *ast.CallExpr, info *types.Info) bool {
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	obj := info.Uses[id]
	if obj == nil {
		obj = info.Defs[id]
	}
	if b, ok := obj.(*types.Builtin); ok {
		return b.Name() == "spawn"
	}
	if f, ok := obj.(*types.Func); ok {
		return f.Name() == "spawn" && f.Parent() == types.Universe
	}
	return false
}

func containsExpr(list []ast.Expr, n ast.Node) bool {
	for _, e := range list {
		if e == n {
			return true
		}
	}
	return false
}
