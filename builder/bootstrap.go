package builder

import (
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"moxie/loader"
	"tinygo.org/x/go-llvm"
)

var (
	bootstrapOnce        sync.Once
	bootstrapLib         uintptr
	bootstrapCompile     func(uintptr, int32, uintptr, int32, uintptr, int32) int32
	bootstrapIRLen       func(int32) int32
	bootstrapIRCopy      func(int32, uintptr, int32) int32
	bootstrapIRFree      func(int32)
	bootstrapRegPkg      func(uintptr, int32, uintptr, int32)
	bootstrapRegFunc     func(uintptr, int32, uintptr, int32, uintptr, int32)
	bootstrapRegVar      func(uintptr, int32, uintptr, int32, uintptr, int32)
	bootstrapClearImport func()
	bootstrapErr         error
)

func initBootstrap() {
	bootstrapOnce.Do(func() {
		soPath := os.Getenv("MOXIE_COMPILE_SO")
		if soPath == "" {
			root := os.Getenv("MOXIEROOT")
			if root == "" {
				root, _ = os.Executable()
				root = filepath.Dir(root)
			}
			soPath = filepath.Join(root, "bootstrap", "compile.so")
		}
		if _, err := os.Stat(soPath); err != nil {
			bootstrapErr = err
			return
		}
		lib, err := purego.Dlopen(soPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			bootstrapErr = fmt.Errorf("dlopen %s: %w", soPath, err)
			return
		}
		bootstrapLib = lib
		purego.RegisterLibFunc(&bootstrapCompile, lib, "moxie_compile_to_ir")
		purego.RegisterLibFunc(&bootstrapIRLen, lib, "moxie_compile_ir_len")
		purego.RegisterLibFunc(&bootstrapIRCopy, lib, "moxie_compile_ir_copy")
		purego.RegisterLibFunc(&bootstrapIRFree, lib, "moxie_compile_ir_free")
		purego.RegisterLibFunc(&bootstrapRegPkg, lib, "moxie_register_package")
		purego.RegisterLibFunc(&bootstrapRegFunc, lib, "moxie_register_func")
		purego.RegisterLibFunc(&bootstrapRegVar, lib, "moxie_register_var")
		purego.RegisterLibFunc(&bootstrapClearImport, lib, "moxie_clear_imports")
	})
}

func bootstrapAvailable() bool {
	initBootstrap()
	return bootstrapErr == nil && bootstrapLib != 0
}

func bootstrapRegisterStr(fn func(uintptr, int32, uintptr, int32), a, b string) {
	ab := []byte(a)
	bb := []byte(b)
	fn(uintptr(unsafe.Pointer(&ab[0])), int32(len(ab)), uintptr(unsafe.Pointer(&bb[0])), int32(len(bb)))
}

func bootstrapRegisterStr3(fn func(uintptr, int32, uintptr, int32, uintptr, int32), a, b, c string) {
	ab := []byte(a)
	bb := []byte(b)
	cb := []byte(c)
	fn(uintptr(unsafe.Pointer(&ab[0])), int32(len(ab)),
		uintptr(unsafe.Pointer(&bb[0])), int32(len(bb)),
		uintptr(unsafe.Pointer(&cb[0])), int32(len(cb)))
}

func bootstrapTypeDesc(t types.Type) string {
	switch t := t.Underlying().(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.Bool:
			return "bool"
		case types.Int8:
			return "int8"
		case types.Int16:
			return "int16"
		case types.Int32, types.Int:
			return "int32"
		case types.Int64:
			return "int64"
		case types.Uint8:
			return "uint8"
		case types.Uint16:
			return "uint16"
		case types.Uint32, types.Uint:
			return "uint32"
		case types.Uint64:
			return "uint64"
		case types.Float32:
			return "float32"
		case types.Float64:
			return "float64"
		case types.String:
			return "string"
		case types.UnsafePointer, types.Uintptr:
			return "ptr"
		}
	case *types.Slice:
		return "[]" + bootstrapTypeDesc(t.Elem())
	case *types.Pointer:
		return "*" + bootstrapTypeDesc(t.Elem())
	}
	return "int32"
}

func bootstrapSigDesc(sig *types.Signature) string {
	var parts []string
	params := sig.Params()
	for i := 0; i < params.Len(); i++ {
		parts = append(parts, bootstrapTypeDesc(params.At(i).Type()))
	}
	paramStr := strings.Join(parts, ",")

	results := sig.Results()
	if results.Len() == 0 {
		return paramStr
	}
	var rParts []string
	for i := 0; i < results.Len(); i++ {
		rParts = append(rParts, bootstrapTypeDesc(results.At(i).Type()))
	}
	resultStr := strings.Join(rParts, ",")
	if paramStr == "" {
		return "->" + resultStr
	}
	return paramStr + "->" + resultStr
}

func bootstrapRegisterImports(pkg *loader.Package) {
	bootstrapClearImport()
	if pkg.Pkg == nil {
		return
	}
	for _, imp := range pkg.Pkg.Imports() {
		bootstrapRegisterStr(bootstrapRegPkg, imp.Path(), imp.Name())
		scope := imp.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if !obj.Exported() {
				continue
			}
			switch obj := obj.(type) {
			case *types.Func:
				sig := obj.Type().(*types.Signature)
				bootstrapRegisterStr3(bootstrapRegFunc, imp.Path(), name, bootstrapSigDesc(sig))
			case *types.Var:
				bootstrapRegisterStr3(bootstrapRegVar, imp.Path(), name, bootstrapTypeDesc(obj.Type()))
			}
		}
	}
}

func bootstrapConcatSources(pkg *loader.Package) ([]byte, error) {
	imports := map[string]bool{}
	var bodies [][]byte
	for _, f := range pkg.GoFiles {
		path := filepath.Join(pkg.Dir, f)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: read %s: %w", path, err)
		}
		lines := strings.Split(string(data), "\n")
		var body []string
		i := 0
		for i < len(lines) {
			line := strings.TrimSpace(lines[i])
			if strings.HasPrefix(line, "package ") {
				i++
				continue
			}
			if line == "import (" {
				i++
				for i < len(lines) {
					imp := strings.TrimSpace(lines[i])
					i++
					if imp == ")" {
						break
					}
					if imp != "" {
						imports[imp] = true
					}
				}
				continue
			}
			if strings.HasPrefix(line, "import ") && !strings.HasPrefix(line, "import (") {
				imp := strings.TrimPrefix(line, "import ")
				imports[imp] = true
				i++
				continue
			}
			body = append(body, lines[i])
			i++
		}
		bodies = append(bodies, []byte(strings.Join(body, "\n")))
	}
	var out []byte
	out = append(out, []byte("package "+pkg.Pkg.Name()+"\n")...)
	if len(imports) > 0 {
		out = append(out, []byte("import (\n")...)
		for imp := range imports {
			out = append(out, '\t')
			out = append(out, []byte(imp)...)
			out = append(out, '\n')
		}
		out = append(out, []byte(")\n")...)
	}
	for _, b := range bodies {
		out = append(out, b...)
		out = append(out, '\n')
	}
	return out, nil
}

func bootstrapCompilePackage(pkg *loader.Package, triple string) (llvm.Module, error) {
	initBootstrap()
	if bootstrapErr != nil {
		return llvm.Module{}, bootstrapErr
	}

	bootstrapRegisterImports(pkg)

	src, err := bootstrapConcatSources(pkg)
	if err != nil {
		return llvm.Module{}, err
	}
	if len(src) == 0 {
		return llvm.Module{}, fmt.Errorf("bootstrap: no source files for %s", pkg.ImportPath)
	}

	name := []byte(pkg.Pkg.Path())
	tripleBytes := []byte(triple)

	h := bootstrapCompile(
		uintptr(unsafe.Pointer(&src[0])), int32(len(src)),
		uintptr(unsafe.Pointer(&name[0])), int32(len(name)),
		uintptr(unsafe.Pointer(&tripleBytes[0])), int32(len(tripleBytes)),
	)
	if h < 0 {
		return llvm.Module{}, fmt.Errorf("bootstrap: compile failed for %s", pkg.ImportPath)
	}

	n := bootstrapIRLen(h)
	if n <= 0 {
		bootstrapIRFree(h)
		return llvm.Module{}, fmt.Errorf("bootstrap: empty IR for %s", pkg.ImportPath)
	}
	irBuf := make([]byte, n)
	bootstrapIRCopy(h, uintptr(unsafe.Pointer(&irBuf[0])), n)
	bootstrapIRFree(h)

	if dp := os.Getenv("MOXIE_BOOTSTRAP_DUMP_IR"); dp != "" {
		os.WriteFile(dp, irBuf, 0644)
	}

	ctx := llvm.NewContext()
	membuf := llvm.NewMemoryBufferFromBytes(irBuf, pkg.ImportPath+".ll")
	mod, err := ctx.ParseIR(membuf)
	if err != nil {
		ctx.Dispose()
		return llvm.Module{}, fmt.Errorf("bootstrap: parse IR for %s: %w", pkg.ImportPath, err)
	}
	return mod, nil
}
