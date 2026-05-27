//go:build purego

package loader

import (
	"fmt"
	"go/ast"
	"go/token"
)

func processCgoFiles(files []*ast.File, workingDir, importPath string, fset *token.FileSet, initialCFlags []string, goos string) (generated []*ast.File, headerCode []string, cflags, ldflags []string, accessedFiles map[string][]byte, errs []error) {
	return nil, nil, nil, nil, nil, []error{fmt.Errorf("cgo not supported in purego builds (package %s has cgo files)", importPath)}
}
