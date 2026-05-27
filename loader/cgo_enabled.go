//go:build !purego

package loader

import (
	"go/ast"
	"go/token"

	"moxie/cgo"
)

func processCgoFiles(files []*ast.File, workingDir, importPath string, fset *token.FileSet, initialCFlags []string, goos string) (generated []*ast.File, headerCode []string, cflags, ldflags []string, accessedFiles map[string][]byte, errs []error) {
	return cgo.Process(files, workingDir, importPath, fset, initialCFlags, goos)
}
