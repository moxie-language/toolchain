package mxtext

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// injectMoxieByteBuiltins parses __moxie_concat, __moxie_eq, __moxie_lt
// declarations for the given package name. Called during the rewrite phase
// to make these functions available in any package that uses []byte ops.
func InjectMoxieByteBuiltins(fset *token.FileSet, pkgName string) *ast.File {
	src := fmt.Sprintf(`package %s

func __moxie_concat(a, b []byte) []byte {
	panic("__moxie_concat: compiler failed to intercept")
}

func __moxie_eq(a, b []byte) bool {
	panic("__moxie_eq: compiler failed to intercept")
}

func __moxie_lt(a, b []byte) bool {
	panic("__moxie_lt: compiler failed to intercept")
}
`, pkgName)
	f, err := parser.ParseFile(fset, "<moxie:builtins>", src, 0)
	if err != nil {
		return nil
	}
	return f
}
