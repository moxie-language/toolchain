package builder

import (
	"fmt"
	"go/constant"
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
	bootstrapOnce           sync.Once
	bootstrapLib            uintptr
	bootstrapCompile        func(uintptr, int32, uintptr, int32, uintptr, int32) int32
	bootstrapIRLen          func(int32) int32
	bootstrapIRCopy         func(int32, uintptr, int32) int32
	bootstrapIRFree         func(int32)
	bootstrapRegPkg         func(uintptr, int32, uintptr, int32)
	bootstrapRegFunc        func(uintptr, int32, uintptr, int32, uintptr, int32)
	bootstrapRegVar         func(uintptr, int32, uintptr, int32, uintptr, int32)
	bootstrapRegConst       func(uintptr, int32, uintptr, int32, uintptr, int32, int64)
	bootstrapRegConstStr    func(uintptr, int32, uintptr, int32, uintptr, int32)
	bootstrapRegIface       func(uintptr, int32, uintptr, int32, uintptr, int32)
	bootstrapRegType        func(uintptr, int32, uintptr, int32, uintptr, int32)
	bootstrapRegMethod      func(uintptr, int32, uintptr, int32, uintptr, int32, uintptr, int32, int32)
	bootstrapClearImport    func()
	bootstrapErr            error
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
		purego.RegisterLibFunc(&bootstrapRegConst, lib, "moxie_register_const")
		purego.RegisterLibFunc(&bootstrapRegConstStr, lib, "moxie_register_const_string")
		purego.RegisterLibFunc(&bootstrapRegIface, lib, "moxie_register_iface")
		purego.RegisterLibFunc(&bootstrapRegType, lib, "moxie_register_type")
		purego.RegisterLibFunc(&bootstrapRegMethod, lib, "moxie_register_method")
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

func bootstrapRegisterStr4(fn func(uintptr, int32, uintptr, int32, uintptr, int32, uintptr, int32, int32), a, b, c, d string, flag int32) {
	ab := []byte(a)
	bb := []byte(b)
	cb := []byte(c)
	db := []byte(d)
	fn(uintptr(unsafe.Pointer(&ab[0])), int32(len(ab)),
		uintptr(unsafe.Pointer(&bb[0])), int32(len(bb)),
		uintptr(unsafe.Pointer(&cb[0])), int32(len(cb)),
		uintptr(unsafe.Pointer(&db[0])), int32(len(db)),
		flag)
}

func bootstrapRegisterConst(pkgPath, name, typDesc string, val int64) {
	ab := []byte(pkgPath)
	bb := []byte(name)
	cb := []byte(typDesc)
	bootstrapRegConst(
		uintptr(unsafe.Pointer(&ab[0])), int32(len(ab)),
		uintptr(unsafe.Pointer(&bb[0])), int32(len(bb)),
		uintptr(unsafe.Pointer(&cb[0])), int32(len(cb)),
		val)
}

func bootstrapIfaceMethodsDesc(iface *types.Interface) string {
	var parts []string
	for i := 0; i < iface.NumMethods(); i++ {
		m := iface.Method(i)
		sig := m.Type().(*types.Signature)
		parts = append(parts, m.Name()+"="+bootstrapSigDesc(sig))
	}
	return strings.Join(parts, ";")
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
	case *types.Interface:
		if t.NumMethods() == 1 && t.Method(0).Name() == "Error" {
			return "error"
		}
		return "interface{}"
	case *types.Signature:
		return "interface{}"
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
			case *types.Const:
				val := obj.Val()
				switch val.Kind() {
				case constant.String:
					s := constant.StringVal(val)
					bootstrapRegisterStr3(bootstrapRegConstStr, imp.Path(), name, s)
				case constant.Int:
					if v, ok := constant.Int64Val(val); ok {
						bootstrapRegisterConst(imp.Path(), name, bootstrapTypeDesc(obj.Type()), v)
					}
				case constant.Float:
					if v, ok := constant.Float64Val(val); ok {
						bootstrapRegisterConst(imp.Path(), name, bootstrapTypeDesc(obj.Type()), int64(v))
					}
				case constant.Bool:
					v := int64(0)
					if constant.BoolVal(val) {
						v = 1
					}
					bootstrapRegisterConst(imp.Path(), name, "bool", v)
				}
			case *types.TypeName:
				underlying := obj.Type().Underlying()
				if iface, ok := underlying.(*types.Interface); ok {
					bootstrapRegisterStr3(bootstrapRegIface, imp.Path(), name, bootstrapIfaceMethodsDesc(iface))
				} else {
					bootstrapRegisterStr3(bootstrapRegType, imp.Path(), name, bootstrapTypeDesc(obj.Type()))
					named, ok := obj.Type().(*types.Named)
					if ok {
						for i := 0; i < named.NumMethods(); i++ {
							m := named.Method(i)
							msig := m.Type().(*types.Signature)
							ptrRecv := int32(0)
							if msig.Recv() != nil {
								if _, isPtr := msig.Recv().Type().(*types.Pointer); isPtr {
									ptrRecv = 1
								}
							}
							bootstrapRegisterStr4(bootstrapRegMethod, imp.Path(), name, m.Name(), bootstrapSigDesc(msig), ptrRecv)
						}
					}
				}
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
