package main

import (
	"fmt"
	"go/types"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"moxie/builder"
	"moxie/compileopts"
	"moxie/loader"
	"moxie/parse"
)

// Header scans a package for codec types and emits a .mxh protocol header.
// outdir is the directory to write <pkgname>.mxh into; "-" writes to stdout.
// prevPath is an optional path to an old .mxh for backward-compat checking.
func Header(pkgName, outdir, prevPath string, options *compileopts.Options) error {
	config, err := builder.NewConfig(options)
	if err != nil {
		return err
	}
	prog, err := loader.Load(config, pkgName, types.Config{})
	if err != nil {
		return err
	}
	prog.SkipMainNameCheck = true
	if err := prog.Parse(); err != nil {
		return err
	}

	// Find the target package by import path or by resolving a local dir.
	// Do not use MainPkg() — that returns the last dependency-ordered package,
	// which may not be the requested package when the input is a stdlib package.
	targetPkg := findTargetPkg(prog, pkgName)
	if targetPkg == nil {
		return fmt.Errorf("header: package %q not found in program", pkgName)
	}

	types_, err := collectCodecTypes(targetPkg, prog)
	if err != nil {
		return err
	}

	// Build body.
	var body strings.Builder
	// Wire protocol declaration.
	body.WriteString("// wire: uint32-length-prefixed EncodeTo/DecodeFrom at cross-binary spawn boundaries\n")
	for _, td := range types_ {
		body.WriteString("type ")
		body.WriteString(td.name)
		switch {
		case len(td.fields) > 0 && td.fields[0].name == "_":
			// Named basic type: "type Bool bool"
			body.WriteString(" ")
			body.WriteString(td.fields[0].typ)
			body.WriteString("\n")
		case len(td.fields) > 0:
			// Struct type with exported fields.
			body.WriteString(" struct {\n")
			for _, f := range td.fields {
				body.WriteString("\t")
				body.WriteString(f.name)
				body.WriteString(" ")
				body.WriteString(f.typ)
				body.WriteString("\n")
			}
			body.WriteString("}\n")
		default:
			// Opaque type (e.g. type Bytes []byte): wire format via methods only.
			body.WriteString(" opaque\n")
		}
		// *T's method set always includes T's value-receiver methods, so the
		// pointer spelling is valid whether EncodeTo was declared on T or *T.
		body.WriteString("func (*")
		body.WriteString(td.name)
		body.WriteString(") EncodeTo(w io.Writer) error\n")
		body.WriteString("func (*")
		body.WriteString(td.name)
		body.WriteString(") DecodeFrom(r io.Reader) error\n")
	}

	// Hash the body with FNV-1a.
	h := fnv.New64a()
	io.WriteString(h, body.String())
	hash := fmt.Sprintf("%016x", h.Sum64())

	// Include module version in .mxh header if available.
	wd, _ := os.Getwd()
	modVersion := loader.ParseMoxieModVersion(wd)
	headerLine := "// mxh v1 " + hash
	if modVersion != "" {
		headerLine += " " + modVersion
	}
	full := headerLine + "\n" + body.String()

	// Compat check against previous version.
	if prevPath != "" {
		f, err := os.Open(prevPath)
		if err != nil {
			return fmt.Errorf("header: opening -prev: %w", err)
		}
		defer f.Close()
		prev, err := parse.ParseMXH(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("header: parsing -prev: %w", err)
		}
		if err := compatCheck(prev, types_); err != nil {
			return err
		}
	}

	if outdir == "-" {
		fmt.Print(full)
		return nil
	}

	pkgBase := filepath.Base(targetPkg.Dir)
	outPath := filepath.Join(outdir, pkgBase+".mxh")
	return os.WriteFile(outPath, []byte(full), 0644)
}

type codecTypeDef struct {
	name   string
	fields []codecField
}

type codecField struct {
	name string
	typ  string
}

func collectCodecTypes(pkg *loader.Package, prog *loader.Program) ([]codecTypeDef, error) {
	scope := pkg.Pkg.Scope()
	names := scope.Names()
	sort.Strings(names)

	var out []codecTypeDef
	for _, name := range names {
		obj := scope.Lookup(name)
		tn, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}
		t := tn.Type()
		if !loader.ImplementsCodec(t, prog) {
			continue
		}

		// Skip interface types — they can't cross spawn boundaries as data.
		if _, ok := t.Underlying().(*types.Interface); ok {
			continue
		}

		var fields []codecField
		if st, ok := t.Underlying().(*types.Struct); ok {
			// Struct type: emit exported fixed-width fields for wire-layout compat.
			for i := 0; i < st.NumFields(); i++ {
				f := st.Field(i)
				if !f.Exported() {
					continue
				}
				ft := f.Type()
				if isVariableLength(ft) {
					return nil, fmt.Errorf("header: variable-length field %s.%s cannot appear in .mxh struct layout; encode it in EncodeTo/DecodeFrom instead", name, f.Name())
				}
				fields = append(fields, codecField{name: f.Name(), typ: types.TypeString(ft, nil)})
			}
		} else if !isVariableLength(t.Underlying()) {
			// Non-struct fixed-width named type (e.g. type Bool bool, type Int32 int32):
			// emit the underlying type string for compat detection.
			underlyingStr := types.TypeString(t.Underlying(), nil)
			fields = append(fields, codecField{name: "_", typ: underlyingStr})
		}
		// else: variable-length named type (e.g. type Bytes []byte): emit as opaque.
		// Wire format is defined by EncodeTo/DecodeFrom, not struct layout.
		out = append(out, codecTypeDef{name: name, fields: fields})
	}
	return out, nil
}

// findTargetPkg finds the package the user requested for header extraction.
// It checks by exact import path, then by the working directory.
func findTargetPkg(prog *loader.Program, pkgName string) *loader.Package {
	// Direct import path lookup.
	if pkg, ok := prog.Packages[pkgName]; ok {
		return pkg
	}
	// Relative or absolute directory: find by resolved directory.
	abs, err := filepath.Abs(pkgName)
	if err == nil {
		for _, pkg := range prog.Sorted() {
			if pkg.Dir == abs || filepath.Base(pkg.Dir) == pkgName {
				return pkg
			}
		}
	}
	return nil
}

// isVariableLength returns true for slice and map types.
func isVariableLength(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Slice, *types.Map:
		return true
	}
	return false
}

// compatCheck verifies backward-compat: fields can only be appended, never
// removed, reordered, or changed in type.
func compatCheck(prev *parse.MXH, cur []codecTypeDef) error {
	curByName := make(map[string]*codecTypeDef, len(cur))
	for i := range cur {
		curByName[cur[i].name] = &cur[i]
	}

	for _, pt := range prev.Types {
		ct, ok := curByName[pt.Name]
		if !ok {
			return fmt.Errorf("compat: type %s was removed (breaking change)", pt.Name)
		}
		for i, pf := range pt.Fields {
			if i >= len(ct.fields) {
				return fmt.Errorf("compat: %s.%s (field %d) was removed (breaking change)", pt.Name, pf.Name, i)
			}
			cf := ct.fields[i]
			if cf.name != pf.Name {
				return fmt.Errorf("compat: %s field %d: was %q, now %q (reorder is breaking)", pt.Name, i, pf.Name, cf.name)
			}
			if cf.typ != pf.Type {
				return fmt.Errorf("compat: %s.%s type changed from %q to %q (breaking)", pt.Name, pf.Name, pf.Type, cf.typ)
			}
		}
	}
	return nil
}
