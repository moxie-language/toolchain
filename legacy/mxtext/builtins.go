package mxtext

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// HasMoxieSecallocRefs reports whether any file in the package contains a
// reference to __moxie_secalloc, the stub the slice-literal rewriter emits
// for the `[]byte{:n, secure}` form. The loader uses this as an injection
// trigger so packages that ONLY use the secure-allocator literal (and none
// of the AST-level rewrites: pipe concat, byte compare, byte switch,
// add-assign) still get the builtin declarations injected.
func HasMoxieSecallocRefs(files []*ast.File) bool {
	for _, f := range files {
		found := false
		ast.Inspect(f, func(n ast.Node) bool {
			if found {
				return false
			}
			if id, ok := n.(*ast.Ident); ok && id.Name == "__moxie_secalloc" {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// injectMoxieByteBuiltins parses __moxie_concat, __moxie_eq, __moxie_lt,
// and __moxie_secalloc declarations for the given package name. Called
// during the rewrite phase to make these functions available in any
// package that uses []byte ops or the secure-allocator literal syntax.
func InjectMoxieByteBuiltins(fset *token.FileSet, pkgName string) *ast.File {
	src := fmt.Sprintf(`package %s

// Parameter names are prefixed because this file is parsed into the user's
// package and the shadowing check runs over it: a package-level n in the
// fixture made the injected __moxie_secalloc(n int32) fail with
// 'n shadows declaration in enclosing scope'.
func __moxie_concat(__a, __b []byte) (__out []byte) {
	panic("__moxie_concat: compiler failed to intercept")
}

func __moxie_concat_move(__a, __b []byte) (__out []byte) {
	panic("__moxie_concat_move: compiler failed to intercept")
}

func __moxie_eq(__a, __b []byte) (__eq bool) {
	panic("__moxie_eq: compiler failed to intercept")
}

func __moxie_lt(__a, __b []byte) (__lt bool) {
	panic("__moxie_lt: compiler failed to intercept")
}

func __moxie_secalloc(__n int32) (__out []byte) {
	panic("__moxie_secalloc: compiler failed to intercept")
}
`, pkgName)
	f, err := parser.ParseFile(fset, "<moxie:builtins>", src, 0)
	if err != nil {
		return nil
	}
	return f
}
