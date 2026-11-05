package loader

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ast/astutil"
)

// rewriteValueReceiverRefs makes a value receiver over a NAMED slice or map
// behave as a reference to the caller's variable, matching stage4's front end.
//
// The legacy compiler gets there through pointer receivers: the receiver type
// becomes *T and every USE of the receiver name in the body becomes (*name).
// That is the same code pointer-receiver methods already generate, so the
// sovereign-arena prologue, the receiver store-back and the method-set rules
// need no separate path. Go's addressability requirement is already dropped by
// the patched type checker (a pointer-receiver method is callable on a value
// expression and belongs to the value type's method set), so a call site keeps
// working unchanged.
//
// The rewrite is driven by the FIRST type-check pass: info.Uses tells us
// exactly which identifiers refer to the receiver variable, so a declaration
// that shadows the name inside the body is left alone. The caller must
// re-typecheck afterwards; Check() already has that path for the pipe/byte
// rewrites.
func rewriteValueReceiverRefs(files []*ast.File, info *types.Info) (n int) {
	if info == nil || len(info.Uses) == 0 {
		return 0
	}
	aggs := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, sp := range gd.Specs {
				ts, ok := sp.(*ast.TypeSpec)
				if !ok || ts.Name == nil || ts.Assign.IsValid() {
					continue
				}
				switch t := ts.Type.(type) {
				case *ast.ArrayType:
					if t.Len == nil {
						aggs[ts.Name.Name] = true
					}
				case *ast.MapType:
					aggs[ts.Name.Name] = true
				}
			}
		}
	}
	if len(aggs) == 0 {
		return 0
	}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 || fd.Body == nil {
				continue
			}
			rf := fd.Recv.List[0]
			if len(rf.Names) == 0 {
				continue
			}
			name := rf.Names[0].Name
			if name == "" || name == "_" {
				continue
			}
			if _, isStar := rf.Type.(*ast.StarExpr); isStar {
				continue
			}
			if !aggs[recvTypeIdent(rf.Type)] {
				continue
			}
			obj := info.Defs[rf.Names[0]]
			if obj == nil {
				continue
			}
			rf.Type = &ast.StarExpr{X: rf.Type}
			fd.Body = rewriteRecvUses(fd.Body, name, obj, info)
			n++
		}
	}
	return n
}

// recvTypeIdent returns the named type a receiver's type expression names:
// T, T[P], or T[P, Q].
func recvTypeIdent(t ast.Expr) (s string) {
	switch x := t.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexListExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// rewriteRecvUses replaces every use of the receiver variable in body with
// (*name). Uses are matched by types.Object identity, so a shadowing
// declaration of the same name is not touched.
func rewriteRecvUses(body *ast.BlockStmt, name string, obj types.Object, info *types.Info) *ast.BlockStmt {
	out := astutil.Apply(body, func(c *astutil.Cursor) bool {
		id, ok := c.Node().(*ast.Ident)
		if !ok || id.Name != name || info.Uses[id] != obj {
			return true
		}
		c.Replace(&ast.ParenExpr{X: &ast.StarExpr{X: ast.NewIdent(name)}})
		return false
	}, nil)
	blk, _ := out.(*ast.BlockStmt)
	return blk
}
