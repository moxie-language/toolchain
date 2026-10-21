package loader

// Synthetic String() methods for structs and named func types.
//
// fmt takes ...Stringer, so a type with no declared String() has no printable
// form: stage4 used to print '?' and legacy refused the package. The compiler
// declares one for every such type (a declared String always wins), and the
// body calls runtime.RenderValue, which walks the type descriptor. The method
// is injected as ordinary source and parsed, so the type checker and the code
// generator see a normal method.
//
// This file is the legacy side of the same rule; _mxc_stage4/ir_typecheck.mx
// injectBuiltinString is the stage4 side. Wood law: both or neither.

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// renderSkipPkg reports whether the package is inside runtime's own import
// closure: the synthesized method imports runtime, and runtime already imports
// these, so the import would be a cycle. The list is runtime's measured
// transitive import closure (11 packages) and must be recomputed if runtime's
// imports change.
func renderSkipPkg(pkgPath string) bool {
	switch pkgPath {
	case "array", "errors", "git.smesh.lol/moxie/pkg/mxregexp",
		"git.smesh.lol/moxie/pkg/mxutil", "internal/binary", "internal/task", "io",
		"math/bits", "moxie", "runtime", "runtime/interrupt", "unsafe":
		return true
	}
	return false
}

// renderUserPkg reports whether the package belongs to the program being built
// rather than to the toolchain's own stdlib. Synthesizing String() for every
// stdlib struct was measured to be unshippable: the method lands in each type's
// export data and in the Stringer dispatch table, and a ~250-package fixture
// grew past the compiler's type-check arena before it linked. User code is
// where the request came from and its type set is small; a stdlib type that is
// printed needs a declared String() until the builtin method is served from
// the dispatch default instead of a per-type declaration.
func renderUserPkg(dir, goroot string) bool {
	if dir == "" || goroot == "" {
		return false
	}
	if dir == goroot || strings.HasPrefix(dir, goroot+string(filepath.Separator)) {
		return false
	}
	return true
}

// recvBaseName returns the base type name of a method receiver expression.
func recvBaseName(e ast.Expr) string {
	switch r := e.(type) {
	case *ast.Ident:
		return r.Name
	case *ast.StarExpr:
		return recvBaseName(r.X)
	case *ast.ParenExpr:
		return recvBaseName(r.X)
	case *ast.IndexExpr:
		return recvBaseName(r.X)
	}
	return ""
}

// injectBuiltinStrings appends one synthetic file declaring String() for every
// struct and named func type in the package that does not already declare one,
// and returns the extended file list.
func (p *Package) injectBuiltinStrings(files []*ast.File) ([]*ast.File, error) {
	if renderSkipPkg(p.ImportPath) || !renderUserPkg(p.Dir, p.program.goroot) {
		return files, nil
	}
	declared := make(map[string]bool)
	var specs []*ast.TypeSpec
	for _, f := range files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv != nil && d.Name != nil && d.Name.Name == "String" && len(d.Recv.List) == 1 {
					if n := recvBaseName(d.Recv.List[0].Type); n != "" {
						declared[n] = true
					}
				}
			case *ast.GenDecl:
				if d.Tok != token.TYPE {
					continue
				}
				for _, s := range d.Specs {
					ts, ok := s.(*ast.TypeSpec)
					if !ok {
						continue
					}
					specs = append(specs, ts)
				}
			}
		}
	}

	var b strings.Builder
	needRuntime := false
	for _, ts := range specs {
		if ts.Name == nil || ts.Assign.IsValid() || ts.TypeParams != nil || declared[ts.Name.Name] {
			continue
		}
		switch t := ts.Type.(type) {
		case *ast.StructType:
			// A struct with an embedded field may already promote a String();
			// synthesizing one would replace that promotion.
			embedded := false
			if t.Fields != nil {
				for _, f := range t.Fields.List {
					if len(f.Names) == 0 {
						embedded = true
					}
				}
			}
			if embedded {
				continue
			}
			needRuntime = true
			fmt.Fprintf(&b, "func (p *%s) String() (s string) {\n", ts.Name.Name)
			b.WriteString("\treturn runtime.RenderValue((*runtime.RawType)(unsafe.TypeOf(*p)), unsafe.Pointer(p))\n}\n\n")
		case *ast.FuncType:
			fmt.Fprintf(&b, "func (p *%s) String() (s string) { return %s }\n\n",
				ts.Name.Name, strconv.Quote(types.ExprString(t)))
		}
	}
	if b.Len() == 0 {
		return files, nil
	}
	var src strings.Builder
	fmt.Fprintf(&src, "package %s\n\nimport (\n", p.Name)
	if needRuntime {
		src.WriteString("\t\"runtime\"\n\t\"unsafe\"\n")
	}
	src.WriteString(")\n\n")
	src.WriteString(b.String())
	f, err := parser.ParseFile(p.program.fset, "<moxie:stringers>", src.String(), 0)
	if err != nil {
		return nil, fmt.Errorf("synthetic String methods for %s: %w", p.ImportPath, err)
	}
	if !needRuntime {
		// The file only declares constant-returning methods; the empty import
		// block above is still valid, but keep the file only if it parsed.
		return append(files, f), nil
	}
	return append(files, f), nil
}
