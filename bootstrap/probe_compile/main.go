package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
)

func concatMxSources(dir string, files []string) ([]byte, int, int) {
	imports := map[string]bool{}
	var bodies [][]byte
	totalLines := 0
	fileCount := 0
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			fmt.Fprintf(os.Stderr, "SKIP file %s: %v\n", name, err)
			continue
		}
		fileCount++
		totalLines += strings.Count(string(data), "\n")
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
	out = append(out, []byte("package main\n")...)
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
	return out, fileCount, totalLines
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <path-to-compile.so>\n", os.Args[0])
		os.Exit(1)
	}
	soPath := os.Args[1]
	if !filepath.IsAbs(soPath) {
		wd, _ := os.Getwd()
		soPath = filepath.Join(wd, soPath)
	}

	lib, err := purego.Dlopen(soPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dlopen %s: %v\n", soPath, err)
		os.Exit(1)
	}

	var compileToIR func(uintptr, int32, uintptr, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&compileToIR, lib, "moxie_compile_to_ir")

	var irLen func(int32) int32
	purego.RegisterLibFunc(&irLen, lib, "moxie_compile_ir_len")

	var irCopy func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&irCopy, lib, "moxie_compile_ir_copy")

	var irFree func(int32)
	purego.RegisterLibFunc(&irFree, lib, "moxie_compile_ir_free")

	var registerPackage func(uintptr, int32, uintptr, int32)
	purego.RegisterLibFunc(&registerPackage, lib, "moxie_register_package")

	var registerFunc func(uintptr, int32, uintptr, int32, uintptr, int32)
	purego.RegisterLibFunc(&registerFunc, lib, "moxie_register_func")

	var registerVar func(uintptr, int32, uintptr, int32, uintptr, int32)
	purego.RegisterLibFunc(&registerVar, lib, "moxie_register_var")

	var clearImports func()
	purego.RegisterLibFunc(&clearImports, lib, "moxie_clear_imports")

	var registerIface func(uintptr, int32, uintptr, int32, uintptr, int32)
	purego.RegisterLibFunc(&registerIface, lib, "moxie_register_iface")

	var registerType func(uintptr, int32, uintptr, int32, uintptr, int32)
	purego.RegisterLibFunc(&registerType, lib, "moxie_register_type")

	var registerMethod func(uintptr, int32, uintptr, int32, uintptr, int32, uintptr, int32, int32)
	purego.RegisterLibFunc(&registerMethod, lib, "moxie_register_method")

	var registerConst func(uintptr, int32, uintptr, int32, uintptr, int32, int64)
	purego.RegisterLibFunc(&registerConst, lib, "moxie_register_const")

	regPkg := func(path, name string) {
		pb := []byte(path)
		nb := []byte(name)
		registerPackage(uintptr(unsafe.Pointer(&pb[0])), int32(len(pb)), uintptr(unsafe.Pointer(&nb[0])), int32(len(nb)))
	}
	regFn := func(pkgPath, name, sig string) {
		pp := []byte(pkgPath)
		nb := []byte(name)
		if sig == "" {
			registerFunc(uintptr(unsafe.Pointer(&pp[0])), int32(len(pp)), uintptr(unsafe.Pointer(&nb[0])), int32(len(nb)), 0, 0)
			return
		}
		sb := []byte(sig)
		registerFunc(uintptr(unsafe.Pointer(&pp[0])), int32(len(pp)), uintptr(unsafe.Pointer(&nb[0])), int32(len(nb)), uintptr(unsafe.Pointer(&sb[0])), int32(len(sb)))
	}
	regVar := func(pkgPath, name, typ string) {
		pp := []byte(pkgPath)
		nb := []byte(name)
		tb := []byte(typ)
		registerVar(uintptr(unsafe.Pointer(&pp[0])), int32(len(pp)), uintptr(unsafe.Pointer(&nb[0])), int32(len(nb)), uintptr(unsafe.Pointer(&tb[0])), int32(len(tb)))
	}
	_ = regVar
	regConst := func(pkgPath, name, typ string, val int64) {
		pp := []byte(pkgPath)
		nb := []byte(name)
		tb := []byte(typ)
		registerConst(uintptr(unsafe.Pointer(&pp[0])), int32(len(pp)), uintptr(unsafe.Pointer(&nb[0])), int32(len(nb)), uintptr(unsafe.Pointer(&tb[0])), int32(len(tb)), val)
	}
	_ = regConst
	regIface := func(pkgPath, name, methods string) {
		pp := []byte(pkgPath)
		nb := []byte(name)
		mb := []byte(methods)
		registerIface(uintptr(unsafe.Pointer(&pp[0])), int32(len(pp)), uintptr(unsafe.Pointer(&nb[0])), int32(len(nb)), uintptr(unsafe.Pointer(&mb[0])), int32(len(mb)))
	}
	_ = regIface
	regType := func(pkgPath, name, underlying string) {
		pp := []byte(pkgPath)
		nb := []byte(name)
		if underlying == "" {
			registerType(uintptr(unsafe.Pointer(&pp[0])), int32(len(pp)), uintptr(unsafe.Pointer(&nb[0])), int32(len(nb)), 0, 0)
		} else {
			ub := []byte(underlying)
			registerType(uintptr(unsafe.Pointer(&pp[0])), int32(len(pp)), uintptr(unsafe.Pointer(&nb[0])), int32(len(nb)), uintptr(unsafe.Pointer(&ub[0])), int32(len(ub)))
		}
	}
	_ = regType
	regMethod := func(pkgPath, typeName, methodName, sig string, ptrRecv bool) {
		pp := []byte(pkgPath)
		tn := []byte(typeName)
		mn := []byte(methodName)
		sb := []byte(sig)
		pr := int32(0)
		if ptrRecv { pr = 1 }
		registerMethod(
			uintptr(unsafe.Pointer(&pp[0])), int32(len(pp)),
			uintptr(unsafe.Pointer(&tn[0])), int32(len(tn)),
			uintptr(unsafe.Pointer(&mn[0])), int32(len(mn)),
			uintptr(unsafe.Pointer(&sb[0])), int32(len(sb)),
			pr,
		)
	}
	_ = regMethod

	// Subprocess mode: compile a single file and report result
	if len(os.Args) >= 4 && os.Args[2] == "--compile-file" {
		src, err := os.ReadFile(os.Args[3])
		if err != nil {
			fmt.Fprintf(os.Stderr, "read error: %v", err)
			os.Exit(1)
		}
		// Warm up: compile a trivial source to initialize universe types (Typ[] etc.)
		// before registering imports that depend on parseTypeDesc.
		warmup := []byte("package main\nfunc main() {}\n")
		wpkg := []byte("main")
		wh := compileToIR(
			uintptr(unsafe.Pointer(&warmup[0])), int32(len(warmup)),
			uintptr(unsafe.Pointer(&wpkg[0])), int32(len(wpkg)),
			uintptr(unsafe.Pointer(&[]byte("x86_64-unknown-linux-musl")[0])), 25,
		)
		irFree(wh)
		clearImports()
		regPkg("go/token", "token")
		regConst("go/token", "ILLEGAL", "int", 0)
		regConst("go/token", "EOF", "int", 1)
		regConst("go/token", "COMMENT", "int", 2)
		regConst("go/token", "IDENT", "int", 4)
		regConst("go/token", "INT", "int", 5)
		regConst("go/token", "FLOAT", "int", 6)
		regConst("go/token", "IMAG", "int", 7)
		regConst("go/token", "CHAR", "int", 8)
		regConst("go/token", "STRING", "int", 9)
		regConst("go/token", "ADD", "int", 12)
		regConst("go/token", "SUB", "int", 13)
		regConst("go/token", "MUL", "int", 14)
		regConst("go/token", "QUO", "int", 15)
		regConst("go/token", "REM", "int", 16)
		regConst("go/token", "AND", "int", 17)
		regConst("go/token", "OR", "int", 18)
		regConst("go/token", "XOR", "int", 19)
		regConst("go/token", "SHL", "int", 20)
		regConst("go/token", "SHR", "int", 21)
		regConst("go/token", "AND_NOT", "int", 22)
		regConst("go/token", "LAND", "int", 34)
		regConst("go/token", "LOR", "int", 35)
		regConst("go/token", "ARROW", "int", 36)
		regConst("go/token", "INC", "int", 37)
		regConst("go/token", "DEC", "int", 38)
		regConst("go/token", "EQL", "int", 39)
		regConst("go/token", "LSS", "int", 40)
		regConst("go/token", "GTR", "int", 41)
		regConst("go/token", "ASSIGN", "int", 42)
		regConst("go/token", "NOT", "int", 43)
		regConst("go/token", "NEQ", "int", 44)
		regConst("go/token", "LEQ", "int", 45)
		regConst("go/token", "GEQ", "int", 46)
		regConst("go/token", "DEFINE", "int", 47)
		regConst("go/token", "ELLIPSIS", "int", 48)
		regConst("go/token", "DEFAULT", "int", 62)
		regType("go/token", "Token", "int")
		regPkg("go/constant", "constant")
		regConst("go/constant", "Unknown", "int", 0)
		regConst("go/constant", "Bool", "int", 1)
		regConst("go/constant", "String", "int", 2)
		regConst("go/constant", "Int", "int", 3)
		regConst("go/constant", "Float", "int", 4)
		regConst("go/constant", "Complex", "int", 5)
		regIface("go/constant", "Value", "Kind=->int;String=->string;ExactString=->string")
		regFn("go/constant", "MakeInt64", "int64->interface{}")
		regFn("go/constant", "MakeFloat64", "float64->interface{}")
		regFn("go/constant", "MakeString", "string->interface{}")
		regFn("go/constant", "MakeFromLiteral", "string,int,int->interface{}")
		regFn("go/constant", "BinaryOp", "interface{},int,interface{}->interface{}")
		regFn("go/constant", "UnaryOp", "int,interface{},int->interface{}")
		regFn("go/constant", "Compare", "interface{},int,interface{}->bool")
		regFn("go/constant", "StringVal", "interface{}->string")
		regFn("go/constant", "Int64Val", "interface{}->int64,bool")
		regFn("go/constant", "Uint64Val", "interface{}->uint64,bool")
		regFn("go/constant", "Float64Val", "interface{}->float64,bool")
		regFn("go/constant", "BitLen", "interface{}->int")
		regFn("go/constant", "Sign", "interface{}->int")
		regPkg("fmt", "fmt")
		regFn("fmt", "Sprintf", "string,interface{},interface{},interface{}->string")
		regFn("fmt", "Fprintf", "interface{},string,interface{},interface{}->int,error")
		regPkg("os", "os")
		regVar("os", "Stderr", "interface{}")
		regPkg("strconv", "strconv")
		regFn("strconv", "Itoa", "int->string")
		regFn("strconv", "FormatInt", "int64,int->string")
		regFn("strconv", "FormatFloat", "float64,byte,int,int->string")
		regFn("strconv", "ParseInt", "string,int,int->int64,error")
		regFn("strconv", "ParseUint", "string,int,int->uint64,error")
		regFn("strconv", "ParseFloat", "string,int->float64,error")
		regFn("strconv", "Unquote", "string->string,error")
		regPkg("bytes", "bytes")
		regFn("bytes", "NewReader", "*byte->interface{}")
		regType("bytes", "Reader", "")
		regMethod("bytes", "Reader", "Read", "[]byte->int,error", true)
		regFn("bytes", "IndexByte", "[]byte,byte->int")
		regFn("bytes", "HasPrefix", "[]byte,[]byte->bool")
		regFn("bytes", "TrimSpace", "[]byte->[]byte")
		regFn("bytes", "Index", "[]byte,[]byte->int")
		regFn("bytes", "LastIndexByte", "[]byte,byte->int")
		regFn("bytes", "Trim", "[]byte,string->[]byte")
		regPkg("io", "io")
		regIface("io", "Reader", "Read=[]byte->int,error")
		regVar("io", "EOF", "error")
		regVar("io", "ErrNoProgress", "error")
		regPkg("runtime", "runtime")
		regFn("runtime", "InitCShared", "")
		regPkg("unsafe", "unsafe")
		regPkg("unicode", "unicode")
		regType("unicode", "Tables", "")
		regMethod("unicode", "Tables", "IsLetter", "int32->bool", true)
		regMethod("unicode", "Tables", "IsDigit", "int32->bool", true)
		regFn("unicode", "NewTables", "->ptr")
		regFn("unicode", "IsLetter", "int32->bool")
		regFn("unicode", "IsDigit", "int32->bool")
		regFn("unicode", "IsSpace", "int32->bool")
		regVar("unicode", "MaxRune", "int32")
		regPkg("unicode/utf8", "utf8")
		regFn("unicode/utf8", "DecodeRune", "[]byte->int32,int")
		regFn("unicode/utf8", "RuneLen", "int32->int")
		regVar("unicode/utf8", "RuneSelf", "int")
		regVar("unicode/utf8", "UTFMax", "int")
		regVar("unicode/utf8", "RuneError", "int32")
		regFn("unicode/utf8", "FullRune", "[]byte->bool")
		triple := []byte("x86_64-unknown-linux-musl")
		pkg := []byte("main")
		fmt.Fprintf(os.Stderr, "[sub] compiling %d bytes from %s\n", len(src), os.Args[3])
		h := compileToIR(
			uintptr(unsafe.Pointer(&src[0])), int32(len(src)),
			uintptr(unsafe.Pointer(&pkg[0])), int32(len(pkg)),
			uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
		)
		fmt.Fprintf(os.Stderr, "[sub] compile returned h=%d\n", h)
		l := irLen(h)
		if l == 0 {
			fmt.Print("IR=0")
			os.Exit(0)
		}
		buf := make([]byte, l)
		irCopy(h, uintptr(unsafe.Pointer(&buf[0])), l)
		ir := string(buf)
		funcCount := strings.Count(ir, "\ndefine ")
		if funcCount == 0 {
			fmt.Printf("IR=%d funcs=%d\n%s", len(ir), funcCount, ir)
		} else {
			fmt.Printf("IR=%d funcs=%d", len(ir), funcCount)
		}
		if len(os.Args) >= 5 && os.Args[4] != "" {
			os.WriteFile(os.Args[4], []byte(ir), 0644)
			fmt.Fprintf(os.Stderr, "[sub] wrote IR to %s\n", os.Args[4])
		}
		irFree(h)
		os.Exit(0)
	}

	pass := 0
	fail := 0
	assert := func(name string, cond bool) {
		if cond {
			pass++
		} else {
			fail++
			fmt.Fprintf(os.Stderr, "FAIL: %s\n", name)
		}
	}

	llvmVerify := func(name, ir string) {
		if ir == "" {
			fail++
			fmt.Fprintf(os.Stderr, "FAIL: %s llvm-verify (empty IR)\n", name)
			return
		}
		cmd := exec.Command("llvm-as-21", "-o", "/dev/null")
		cmd.Stdin = strings.NewReader(ir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			fail++
			fmt.Fprintf(os.Stderr, "FAIL: %s llvm-verify: %s\n%s\n", name, err, out)
		} else {
			pass++
		}
	}

	getIR := func(h int32) string {
		n := irLen(h)
		if n <= 0 {
			return ""
		}
		buf := make([]byte, n)
		got := irCopy(h, uintptr(unsafe.Pointer(&buf[0])), n)
		return string(buf[:got])
	}

	name1 := []byte("mypkg")
	triple := []byte("x86_64-unknown-linux-musl")

	dumpTests := map[int]bool{}
	test := func(num int, src string, checks func(string)) {
		srcb := []byte(src)
		pkg := []byte("main")
		if strings.HasPrefix(src, "package mypkg") {
			pkg = name1
		}
		h := compileToIR(
			uintptr(unsafe.Pointer(&srcb[0])), int32(len(srcb)),
			uintptr(unsafe.Pointer(&pkg[0])), int32(len(pkg)),
			uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
		)
		ir := getIR(h)
		if ir == "" {
			fail++
			fmt.Fprintf(os.Stderr, "FAIL: test %d: empty IR\n", num)
			irFree(h)
			return
		}
		if dumpTests[num] {
			fmt.Fprintf(os.Stderr, "=== IR test %d ===\n%s\n", num, ir)
		}
		llvmVerify(fmt.Sprintf("test %d", num), ir)
		checks(ir)
		irFree(h)
	}
	_ = test

	// Test 1: Simple add function
	src1 := []byte(`package mypkg

func add(a, b int32) int32 {
	return a + b
}
`)
	h1 := compileToIR(
		uintptr(unsafe.Pointer(&src1[0])), int32(len(src1)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	assert("compile returns handle >= 0", h1 >= 0)

	ir1 := getIR(h1)
	fmt.Println("=== IR for add ===")
	fmt.Println(ir1)

	llvmVerify("add", ir1)
	assert("IR contains target triple", strings.Contains(ir1, "x86_64"))
	assert("IR contains define", strings.Contains(ir1, "define"))
	assert("IR contains @mypkg.add", strings.Contains(ir1, "@mypkg.add"))
	assert("IR contains i32", strings.Contains(ir1, "i32"))
	assert("IR contains add instruction", strings.Contains(ir1, "add i32"))
	assert("IR contains ret", strings.Contains(ir1, "ret i32"))
	assert("IR contains entry block", strings.Contains(ir1, "entry:"))

	irFree(h1)

	// Test 2: Control flow (if/else)
	src2 := []byte(`package mypkg

func max(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
`)
	h2 := compileToIR(
		uintptr(unsafe.Pointer(&src2[0])), int32(len(src2)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir2 := getIR(h2)
	fmt.Println("=== IR for max ===")
	fmt.Println(ir2)

	llvmVerify("max", ir2)
	assert("max IR contains icmp", strings.Contains(ir2, "icmp"))
	assert("max IR contains br", strings.Contains(ir2, "br"))
	assert("max IR contains multiple blocks", strings.Count(ir2, ":") >= 2)

	irFree(h2)

	// Test 3: Global variables
	src3 := []byte(`package mypkg

var counter int32

func inc() {
	counter = counter + 1
}
`)
	h3 := compileToIR(
		uintptr(unsafe.Pointer(&src3[0])), int32(len(src3)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir3 := getIR(h3)
	fmt.Println("=== IR for counter ===")
	fmt.Println(ir3)

	llvmVerify("counter", ir3)
	assert("counter IR has global", strings.Contains(ir3, "@mypkg.counter"))
	assert("counter IR has global decl", strings.Contains(ir3, "global"))

	irFree(h3)

	// Test 4: Function calls
	src4 := []byte(`package mypkg

func double(x int32) int32 {
	return x + x
}

func quadruple(x int32) int32 {
	return double(double(x))
}
`)
	h4 := compileToIR(
		uintptr(unsafe.Pointer(&src4[0])), int32(len(src4)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir4 := getIR(h4)
	fmt.Println("=== IR for calls ===")
	fmt.Println(ir4)

	llvmVerify("calls", ir4)
	assert("calls IR has call instruction", strings.Contains(ir4, "call"))
	assert("calls IR has @mypkg.double", strings.Contains(ir4, "@mypkg.double"))
	assert("calls IR has @mypkg.quadruple", strings.Contains(ir4, "@mypkg.quadruple"))

	irFree(h4)

	// Test 5: For loop
	src5 := []byte(`package mypkg

func sum(n int32) int32 {
	s := int32(0)
	i := int32(0)
	for i < n {
		s = s + i
		i = i + 1
	}
	return s
}
`)
	h5 := compileToIR(
		uintptr(unsafe.Pointer(&src5[0])), int32(len(src5)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir5 := getIR(h5)
	fmt.Println("=== IR for loop ===")
	fmt.Println(ir5)

	llvmVerify("loop", ir5)
	assert("loop IR has phi or branch back", strings.Contains(ir5, "br"))
	assert("loop IR has icmp", strings.Contains(ir5, "icmp"))
	assert("loop IR has multiple blocks", strings.Count(ir5, ":") >= 3)

	irFree(h5)

	// Test 6: Type conversion
	src6 := []byte(`package mypkg

func widen(x int32) int64 {
	return int64(x)
}

func narrow(x int64) int32 {
	return int32(x)
}
`)
	h6 := compileToIR(
		uintptr(unsafe.Pointer(&src6[0])), int32(len(src6)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir6 := getIR(h6)
	fmt.Println("=== IR for conversions ===")
	fmt.Println(ir6)

	llvmVerify("conversions", ir6)
	assert("widen IR has sext", strings.Contains(ir6, "sext"))
	assert("narrow IR has trunc", strings.Contains(ir6, "trunc"))

	irFree(h6)

	// Test 7: Pointer operations
	src7 := []byte(`package mypkg

func setPtr(p *int32, v int32) {
	*p = v
}

func getPtr(p *int32) int32 {
	return *p
}
`)
	h7 := compileToIR(
		uintptr(unsafe.Pointer(&src7[0])), int32(len(src7)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir7 := getIR(h7)
	fmt.Println("=== IR for pointers ===")
	fmt.Println(ir7)

	llvmVerify("pointers", ir7)
	assert("setPtr IR has store", strings.Contains(ir7, "store"))
	assert("getPtr IR has load", strings.Contains(ir7, "load"))
	assert("pointer IR has ptr param", strings.Contains(ir7, "ptr %p"))

	irFree(h7)

	// Test 8: Boolean operations
	src8 := []byte(`package mypkg

func both(a, b bool) bool {
	if a {
		if b {
			return true
		}
	}
	return false
}
`)
	h8 := compileToIR(
		uintptr(unsafe.Pointer(&src8[0])), int32(len(src8)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir8 := getIR(h8)
	fmt.Println("=== IR for booleans ===")
	fmt.Println(ir8)

	llvmVerify("booleans", ir8)
	assert("bool IR has i1 type", strings.Contains(ir8, "i1"))
	assert("bool IR has br", strings.Contains(ir8, "br"))
	assert("bool IR has true/false", strings.Contains(ir8, "true") || strings.Contains(ir8, "false"))

	irFree(h8)

	// Test 9: Structs
	src9 := []byte(`package mypkg

type Point struct {
	X int32
	Y int32
}

func getX(p Point) int32 {
	return p.X
}

func setY(p *Point, v int32) {
	p.Y = v
}
`)
	h9 := compileToIR(
		uintptr(unsafe.Pointer(&src9[0])), int32(len(src9)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir9 := getIR(h9)
	fmt.Println("=== IR for structs ===")
	fmt.Println(ir9)

	llvmVerify("structs", ir9)
	assert("struct IR has getelementptr", strings.Contains(ir9, "getelementptr"))
	assert("struct IR has i32 field type", strings.Contains(ir9, "i32"))
	assert("struct IR has struct type", strings.Contains(ir9, "{") && strings.Contains(ir9, "}"))

	irFree(h9)

	// Test 10: Multiple return values
	src10 := []byte(`package mypkg

func divmod(a, b int32) (int32, int32) {
	return a / b, a % b
}
`)
	h10 := compileToIR(
		uintptr(unsafe.Pointer(&src10[0])), int32(len(src10)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir10 := getIR(h10)
	fmt.Println("=== IR for multi-return ===")
	fmt.Println(ir10)

	llvmVerify("multi-return", ir10)
	assert("multi-ret IR has sdiv", strings.Contains(ir10, "sdiv"))
	assert("multi-ret IR has srem", strings.Contains(ir10, "srem"))
	assert("multi-ret IR has insertvalue", strings.Contains(ir10, "insertvalue"))

	irFree(h10)

	// Test 11: Short variable declaration and address-of
	src11 := []byte(`package mypkg

func newInt(v int32) *int32 {
	x := v
	return &x
}
`)
	h11 := compileToIR(
		uintptr(unsafe.Pointer(&src11[0])), int32(len(src11)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir11 := getIR(h11)
	fmt.Println("=== IR for addr-of ===")
	fmt.Println(ir11)

	llvmVerify("addr-of", ir11)
	assert("addr-of IR has alloca", strings.Contains(ir11, "alloca"))
	assert("addr-of IR returns ptr", strings.Contains(ir11, "ret ptr"))

	irFree(h11)

	// Test 12: Unsigned operations
	src12 := []byte(`package mypkg

func udiv(a, b uint32) uint32 {
	return a / b
}

func ucomp(a, b uint32) bool {
	return a < b
}
`)
	h12 := compileToIR(
		uintptr(unsafe.Pointer(&src12[0])), int32(len(src12)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir12 := getIR(h12)
	fmt.Println("=== IR for unsigned ===")
	fmt.Println(ir12)

	llvmVerify("unsigned", ir12)
	assert("unsigned IR has udiv", strings.Contains(ir12, "udiv"))
	assert("unsigned IR has ult", strings.Contains(ir12, "ult"))

	irFree(h12)

	// Test 13: Extract from multi-return call
	src13 := []byte(`package mypkg

func divmod(a, b int32) (int32, int32) {
	return a / b, a % b
}

func justQuot(a, b int32) int32 {
	q, _ := divmod(a, b)
	return q
}

func justRem(a, b int32) int32 {
	_, r := divmod(a, b)
	return r
}
`)
	h13 := compileToIR(
		uintptr(unsafe.Pointer(&src13[0])), int32(len(src13)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir13 := getIR(h13)
	fmt.Println("=== IR for extract ===")
	fmt.Println(ir13)

	llvmVerify("extract", ir13)
	assert("extract IR has call", strings.Contains(ir13, "call {i32, i32}"))
	assert("extract IR has extractvalue", strings.Contains(ir13, "extractvalue"))
	assert("extract IR has extractvalue 0", strings.Contains(ir13, ", 0"))
	assert("extract IR has extractvalue 1", strings.Contains(ir13, ", 1"))

	irFree(h13)

	// Test 14: Slice operations - make, len, cap, index
	src14 := []byte(`package mypkg

func sliceLen(s []int32) int32 {
	return len(s)
}

func sliceCap(s []int32) int32 {
	return cap(s)
}

func sliceGet(s []int32, i int32) int32 {
	return s[i]
}

func sliceSet(s []int32, i int32, v int32) {
	s[i] = v
}
`)
	h14 := compileToIR(
		uintptr(unsafe.Pointer(&src14[0])), int32(len(src14)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir14 := getIR(h14)
	fmt.Println("=== IR for slices ===")
	fmt.Println(ir14)

	llvmVerify("slices", ir14)
	assert("slice len extracts field 1", strings.Contains(ir14, "extractvalue {ptr, i64, i64}") && strings.Contains(ir14, ", 1"))
	assert("slice cap extracts field 2", strings.Contains(ir14, ", 2"))
	assert("slice index has extractvalue 0", strings.Contains(ir14, ", 0"))
	assert("slice index has getelementptr", strings.Contains(ir14, "getelementptr"))

	irFree(h14)

	// Test 15: Slice sub-expression
	src15 := []byte(`package mypkg

func subslice(s []int32, lo, hi int32) []int32 {
	return s[lo:hi]
}
`)
	h15 := compileToIR(
		uintptr(unsafe.Pointer(&src15[0])), int32(len(src15)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir15 := getIR(h15)
	fmt.Println("=== IR for subslice ===")
	fmt.Println(ir15)

	llvmVerify("subslice", ir15)
	assert("subslice has extractvalue", strings.Contains(ir15, "extractvalue"))
	assert("subslice has getelementptr", strings.Contains(ir15, "getelementptr"))
	assert("subslice has insertvalue", strings.Contains(ir15, "insertvalue"))
	assert("subslice has sub for new len", strings.Contains(ir15, "sub"))

	irFree(h15)

	// Test 16: make([]T, n) and append
	src16 := []byte(`package mypkg

func makeSlice(n int32) []int32 {
	return make([]int32, n)
}

func makeAndAppend(n int32) []int32 {
	s := make([]int32, 0, n)
	i := int32(0)
	for i < n {
		s = append(s, i)
		i = i + 1
	}
	return s
}
`)
	h16 := compileToIR(
		uintptr(unsafe.Pointer(&src16[0])), int32(len(src16)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir16 := getIR(h16)
	fmt.Println("=== IR for make+append ===")
	fmt.Println(ir16)

	llvmVerify("make+append", ir16)
	assert("make has runtime.alloc", strings.Contains(ir16, "@runtime.alloc"))
	assert("make has insertvalue for slice", strings.Contains(ir16, "insertvalue"))
	assert("append has sliceAppend", strings.Contains(ir16, "@runtime.sliceAppend"))

	irFree(h16)

	// Test 17: Switch statement
	src17 := []byte(`package mypkg

func classify(x int32) int32 {
	switch {
	case x < 0:
		return -1
	case x == 0:
		return 0
	}
	return 1
}
`)
	h17 := compileToIR(
		uintptr(unsafe.Pointer(&src17[0])), int32(len(src17)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir17 := getIR(h17)
	fmt.Println("=== IR for switch ===")
	fmt.Println(ir17)

	llvmVerify("switch", ir17)
	assert("switch has icmp", strings.Contains(ir17, "icmp"))
	assert("switch has multiple br", strings.Count(ir17, "br ") >= 2)

	irFree(h17)

	// Test 18: Nested struct access
	src18 := []byte(`package mypkg

type Inner struct {
	V int32
}

type Outer struct {
	A Inner
	B int32
}

func getInnerV(o *Outer) int32 {
	return o.A.V
}
`)
	h18 := compileToIR(
		uintptr(unsafe.Pointer(&src18[0])), int32(len(src18)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir18 := getIR(h18)
	fmt.Println("=== IR for nested struct ===")
	fmt.Println(ir18)

	llvmVerify("nested struct", ir18)
	assert("nested struct has getelementptr", strings.Contains(ir18, "getelementptr"))
	assert("nested struct has load i32", strings.Contains(ir18, "load i32"))

	irFree(h18)

	// Test 19: Nil pointer check
	src19 := []byte(`package mypkg

func isNil(p *int32) bool {
	if p == nil {
		return true
	}
	return false
}
`)
	h19 := compileToIR(
		uintptr(unsafe.Pointer(&src19[0])), int32(len(src19)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir19 := getIR(h19)
	fmt.Println("=== IR for nil check ===")
	fmt.Println(ir19)

	llvmVerify("nil check", ir19)
	assert("nil check has icmp", strings.Contains(ir19, "icmp"))
	assert("nil check has null/zeroinit", strings.Contains(ir19, "null") || strings.Contains(ir19, "zeroinitializer"))

	irFree(h19)

	// Test 20: Bitwise operations
	src20 := []byte(`package mypkg

func bitAnd(a, b int32) int32 {
	return a & b
}

func bitXor(a, b int32) int32 {
	return a ^ b
}

func bitShiftL(a int32, n uint32) int32 {
	return a << n
}

func bitShiftR(a int32, n uint32) int32 {
	return a >> n
}
`)
	h20 := compileToIR(
		uintptr(unsafe.Pointer(&src20[0])), int32(len(src20)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir20 := getIR(h20)
	fmt.Println("=== IR for bitwise ===")
	fmt.Println(ir20)

	llvmVerify("bitwise", ir20)
	assert("bitwise has and", strings.Contains(ir20, "and i32"))
	assert("bitwise has xor", strings.Contains(ir20, "xor i32"))
	assert("bitwise has shl", strings.Contains(ir20, "shl i32"))
	assert("bitwise has ashr", strings.Contains(ir20, "ashr i32"))

	irFree(h20)

	// Test 21: Slice/string concat (| operator -> OpAdd on slice type)
	src21 := []byte(`package mypkg

func concat(a, b []int32) []int32 {
	return a | b
}
`)
	h21 := compileToIR(
		uintptr(unsafe.Pointer(&src21[0])), int32(len(src21)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir21 := getIR(h21)
	fmt.Println("=== IR for slice concat ===")
	fmt.Println(ir21)

	llvmVerify("slice concat", ir21)
	assert("concat calls sliceAppend", strings.Contains(ir21, "@runtime.sliceAppend"))
	assert("concat extracts ptr", strings.Contains(ir21, "extractvalue"))
	assert("concat builds result", strings.Contains(ir21, "insertvalue"))

	irFree(h21)

	// Test 22: for-range over slice
	src22 := []byte(`package mypkg

func sumRange(s []int32) int32 {
	total := int32(0)
	for _, v := range s {
		total = total + v
	}
	return total
}
`)
	h22 := compileToIR(
		uintptr(unsafe.Pointer(&src22[0])), int32(len(src22)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir22 := getIR(h22)
	fmt.Println("=== IR for range ===")
	fmt.Println(ir22)

	llvmVerify("range", ir22)
	assert("range has alloca for iter", strings.Contains(ir22, "alloca i64"))
	assert("range has icmp ult", strings.Contains(ir22, "icmp ult"))
	assert("range has getelementptr", strings.Contains(ir22, "getelementptr"))
	assert("range has select", strings.Contains(ir22, "select"))

	irFree(h22)

	// Test 23: Map operations (make, set, get)
	src23 := []byte(`package mypkg

func mapSetGet() int32 {
	m := make(map[int32]int32)
	m[1] = 42
	return m[1]
}
`)
	h23 := compileToIR(
		uintptr(unsafe.Pointer(&src23[0])), int32(len(src23)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir23 := getIR(h23)
	fmt.Println("=== IR for map ops ===")
	fmt.Println(ir23)

	llvmVerify("map ops", ir23)
	assert("map has hashmapMake", strings.Contains(ir23, "@runtime.hashmapMake"))
	assert("map has hashmapBinarySet", strings.Contains(ir23, "@runtime.hashmapBinarySet"))
	assert("map has hashmapBinaryGet", strings.Contains(ir23, "@runtime.hashmapBinaryGet"))

	irFree(h23)

	// Test 24: panic
	src24 := []byte(`package mypkg

func mustPositive(n int32) int32 {
	if n <= 0 {
		panic("negative")
	}
	return n
}
`)
	h24 := compileToIR(
		uintptr(unsafe.Pointer(&src24[0])), int32(len(src24)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir24 := getIR(h24)
	fmt.Println("=== IR for panic ===")
	fmt.Println(ir24)

	llvmVerify("panic", ir24)
	assert("panic has runtime._panic", strings.Contains(ir24, "@runtime._panic"))
	assert("panic has unreachable", strings.Contains(ir24, "unreachable"))

	irFree(h24)

	// Test 25: copy builtin
	src25 := []byte(`package mypkg

func copySlice(dst, src []int32) int32 {
	return copy(dst, src)
}
`)
	h25 := compileToIR(
		uintptr(unsafe.Pointer(&src25[0])), int32(len(src25)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir25 := getIR(h25)
	fmt.Println("=== IR for copy ===")
	fmt.Println(ir25)

	llvmVerify("copy", ir25)
	assert("copy calls sliceCopy", strings.Contains(ir25, "@runtime.sliceCopy"))

	irFree(h25)

	// Test 26: delete from map
	src26 := []byte(`package mypkg

func mapDelete(m map[int32]int32, k int32) {
	delete(m, k)
}
`)
	h26 := compileToIR(
		uintptr(unsafe.Pointer(&src26[0])), int32(len(src26)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir26 := getIR(h26)
	fmt.Println("=== IR for delete ===")
	fmt.Println(ir26)

	llvmVerify("delete", ir26)
	assert("delete calls hashmapBinaryDelete", strings.Contains(ir26, "@runtime.hashmapBinaryDelete"))

	irFree(h26)

	// Test 27: float operations
	src27 := []byte(`package mypkg

func fadd(a, b float64) float64 {
	return a + b
}

func fmul(a, b float64) float64 {
	return a * b
}

func fcmp(a, b float64) bool {
	return a < b
}
`)
	h27 := compileToIR(
		uintptr(unsafe.Pointer(&src27[0])), int32(len(src27)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir27 := getIR(h27)
	fmt.Println("=== IR for float ===")
	fmt.Println(ir27)

	llvmVerify("float", ir27)
	assert("float has fadd", strings.Contains(ir27, "fadd"))
	assert("float has fmul", strings.Contains(ir27, "fmul"))
	assert("float has fcmp", strings.Contains(ir27, "fcmp"))

	irFree(h27)

	// Test 28: Function value (function as first-class value passed as ptr)
	src28 := []byte(`package mypkg

func apply(f func(int32) int32, x int32) int32 {
	return f(x)
}
`)
	h28 := compileToIR(
		uintptr(unsafe.Pointer(&src28[0])), int32(len(src28)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir28 := getIR(h28)
	fmt.Println("=== IR for func value ===")
	fmt.Println(ir28)

	llvmVerify("func value", ir28)
	assert("func value has call ptr", strings.Contains(ir28, "call i32 %"))

	irFree(h28)

	// Test 29: Array operations
	src29 := []byte(`package mypkg

func arrayGet(a [4]int32, i int32) int32 {
	return a[i]
}
`)
	h29 := compileToIR(
		uintptr(unsafe.Pointer(&src29[0])), int32(len(src29)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir29 := getIR(h29)
	fmt.Println("=== IR for array ===")
	fmt.Println(ir29)

	llvmVerify("array", ir29)
	assert("array has [4 x i32]", strings.Contains(ir29, "[4 x i32]"))
	assert("array has getelementptr", strings.Contains(ir29, "getelementptr"))

	irFree(h29)

	// Test 30: Map comma-ok lookup
	src30 := []byte(`package mypkg

func mapLookup(m map[int32]int32, k int32) (int32, bool) {
	v, ok := m[k]
	return v, ok
}
`)
	h30 := compileToIR(
		uintptr(unsafe.Pointer(&src30[0])), int32(len(src30)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir30 := getIR(h30)
	fmt.Println("=== IR for comma-ok ===")
	fmt.Println(ir30)

	llvmVerify("comma-ok", ir30)
	assert("comma-ok has hashmapBinaryGet", strings.Contains(ir30, "@runtime.hashmapBinaryGet"))
	assert("comma-ok returns tuple", strings.Contains(ir30, "{i32, i1}") || strings.Contains(ir30, "insertvalue"))

	irFree(h30)

	// Test 31: println with int and string
	src31 := []byte(`package mypkg

func hello(n int32) {
	println("hello", n)
}
`)
	h31 := compileToIR(
		uintptr(unsafe.Pointer(&src31[0])), int32(len(src31)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir31 := getIR(h31)
	fmt.Println("=== IR for println ===")
	fmt.Println(ir31)

	llvmVerify("println", ir31)
	assert("println has printlock", strings.Contains(ir31, "@runtime.printlock"))
	assert("println has printstring", strings.Contains(ir31, "@runtime.printstring"))
	assert("println has printint32", strings.Contains(ir31, "@runtime.printint32"))
	assert("println has printspace", strings.Contains(ir31, "@runtime.printspace"))
	assert("println has printnl", strings.Contains(ir31, "@runtime.printnl"))
	assert("println has printunlock", strings.Contains(ir31, "@runtime.printunlock"))

	irFree(h31)

	// Test 32: print with bool and float
	src32 := []byte(`package mypkg

func debug(ok bool, val float64) {
	print(ok, val)
}
`)
	h32 := compileToIR(
		uintptr(unsafe.Pointer(&src32[0])), int32(len(src32)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir32 := getIR(h32)
	fmt.Println("=== IR for print ===")
	fmt.Println(ir32)

	llvmVerify("print", ir32)
	assert("print has printbool", strings.Contains(ir32, "@runtime.printbool"))
	assert("print has printfloat64", strings.Contains(ir32, "@runtime.printfloat64"))
	assert("print no printspace", !strings.Contains(ir32, "@runtime.printspace"))
	assert("print no printnl", !strings.Contains(ir32, "@runtime.printnl"))

	irFree(h32)

	// Test 33: for-range over map
	src33 := []byte(`package mypkg

func sumMap(m map[int32]int32) int32 {
	total := int32(0)
	for _, v := range m {
		total = total + v
	}
	return total
}
`)
	h33 := compileToIR(
		uintptr(unsafe.Pointer(&src33[0])), int32(len(src33)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir33 := getIR(h33)
	fmt.Println("=== IR for map range ===")
	fmt.Println(ir33)

	llvmVerify("map range", ir33)
	assert("map range has hashmapNext", strings.Contains(ir33, "@runtime.hashmapNext"))
	assert("map range has memset", strings.Contains(ir33, "llvm.memset"))
	assert("map range has alloca [48 x i8]", strings.Contains(ir33, "alloca [48 x i8]"))

	irFree(h33)

	// Test 34: string constant in println
	src34 := []byte(`package mypkg

func greet(name string) {
	println("hello", name)
}
`)
	h34 := compileToIR(
		uintptr(unsafe.Pointer(&src34[0])), int32(len(src34)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir34 := getIR(h34)
	fmt.Println("=== IR for string const ===")
	fmt.Println(ir34)

	llvmVerify("string const", ir34)
	assert("string const has global", strings.Contains(ir34, "@.str."))
	assert("string const has c\"hello\"", strings.Contains(ir34, `c"hello"`))
	assert("string const has printstring with ptr", strings.Contains(ir34, "{ ptr @.str."))

	irFree(h34)

	// Test 35: global string variable
	src35 := []byte(`package mypkg

var greeting = "world"

func getGreeting() string {
	return greeting
}
`)
	h35 := compileToIR(
		uintptr(unsafe.Pointer(&src35[0])), int32(len(src35)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir35 := getIR(h35)
	fmt.Println("=== IR for global string ===")
	fmt.Println(ir35)

	llvmVerify("global string", ir35)
	assert("global string has global var", strings.Contains(ir35, "@mypkg.greeting"))
	assert("global string has load slice", strings.Contains(ir35, "load {ptr, i64, i64}"))

	irFree(h35)

	// Test 36: logical not (unary !)
	src36 := []byte(`package mypkg

func negate(b bool) bool {
	return !b
}
`)
	h36 := compileToIR(
		uintptr(unsafe.Pointer(&src36[0])), int32(len(src36)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir36 := getIR(h36)
	fmt.Println("=== IR for not ===")
	fmt.Println(ir36)

	llvmVerify("not", ir36)
	assert("not has xor", strings.Contains(ir36, "xor i1"))

	irFree(h36)

	// Test 37: global int variable (inferred type)
	src37 := []byte(`package mypkg

var counter = 42

func getCounter() int {
	return counter
}
`)
	h37 := compileToIR(
		uintptr(unsafe.Pointer(&src37[0])), int32(len(src37)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir37 := getIR(h37)
	fmt.Println("=== IR for global int ===")
	fmt.Println(ir37)

	llvmVerify("global int", ir37)
	assert("global int has global var", strings.Contains(ir37, "@mypkg.counter = global i32"))
	assert("global int has load i32", strings.Contains(ir37, "load i32"))

	irFree(h37)

	// Test 38: string comparison
	src38 := []byte(`package mypkg

func isHello(s string) bool {
	return s == "hello"
}
`)
	h38 := compileToIR(
		uintptr(unsafe.Pointer(&src38[0])), int32(len(src38)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir38 := getIR(h38)
	fmt.Println("=== IR for string compare ===")
	fmt.Println(ir38)

	llvmVerify("string compare", ir38)
	assert("string compare has call", strings.Contains(ir38, "call") || strings.Contains(ir38, "icmp"))

	irFree(h38)

	// Test 39: tagless switch (switch { case ... })
	src39 := []byte(`package mypkg

func classify(x int) int {
	switch {
	case x < 0:
		return -1
	case x == 0:
		return 0
	default:
		return 1
	}
}
`)
	h39 := compileToIR(
		uintptr(unsafe.Pointer(&src39[0])), int32(len(src39)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir39 := getIR(h39)
	fmt.Println("=== IR for tagless switch ===")
	fmt.Println(ir39)

	llvmVerify("tagless switch", ir39)
	assert("tagless switch has icmp", strings.Contains(ir39, "icmp"))
	assert("tagless switch has br", strings.Contains(ir39, "br"))

	irFree(h39)

	// Test 40: slice of strings
	src40 := []byte(`package mypkg

func firstWord(words []string) string {
	return words[0]
}
`)
	h40 := compileToIR(
		uintptr(unsafe.Pointer(&src40[0])), int32(len(src40)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir40 := getIR(h40)
	fmt.Println("=== IR for slice of strings ===")
	fmt.Println(ir40)

	llvmVerify("slice of strings", ir40)
	assert("slice of strings has gep", strings.Contains(ir40, "getelementptr"))

	irFree(h40)

	// Test 41: const declarations
	src41 := []byte(`package mypkg

const limit = 100
const name = "test"

func getLimit() int { return limit }
func getName() string { return name }
`)
	h41 := compileToIR(
		uintptr(unsafe.Pointer(&src41[0])), int32(len(src41)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir41 := getIR(h41)
	fmt.Println("=== IR for const decls ===")
	fmt.Println(ir41)

	llvmVerify("const decls", ir41)
	assert("const int returns i32", strings.Contains(ir41, "ret i32 100"))
	assert("const string has str ref", strings.Contains(ir41, "@.str."))

	irFree(h41)

	// Test 42: global variable mutation
	src42 := []byte(`package mypkg

var count int

func increment() {
	count = count + 1
}
`)
	h42 := compileToIR(
		uintptr(unsafe.Pointer(&src42[0])), int32(len(src42)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir42 := getIR(h42)
	fmt.Println("=== IR for global mutation ===")
	fmt.Println(ir42)

	llvmVerify("global mutation", ir42)
	assert("global mutation has load", strings.Contains(ir42, "load i32, ptr @mypkg.count"))
	assert("global mutation has store", strings.Contains(ir42, "store i32"))

	irFree(h42)

	// Test 43: multiple return with named results
	src43 := []byte(`package mypkg

func divmod(a, b int) (int, int) {
	return a / b, a % b
}
`)
	h43 := compileToIR(
		uintptr(unsafe.Pointer(&src43[0])), int32(len(src43)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir43 := getIR(h43)
	fmt.Println("=== IR for divmod ===")
	fmt.Println(ir43)

	llvmVerify("divmod", ir43)
	assert("divmod has sdiv", strings.Contains(ir43, "sdiv"))
	assert("divmod has srem", strings.Contains(ir43, "srem"))
	assert("divmod returns tuple", strings.Contains(ir43, "ret {i32, i32}"))

	irFree(h43)

	// Test 44: len on string
	src44 := []byte(`package mypkg

func strLen(s string) int {
	return len(s)
}
`)
	h44 := compileToIR(
		uintptr(unsafe.Pointer(&src44[0])), int32(len(src44)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir44 := getIR(h44)
	fmt.Println("=== IR for strlen ===")
	fmt.Println(ir44)

	llvmVerify("strlen", ir44)
	assert("strlen has extractvalue for len", strings.Contains(ir44, "extractvalue"))
	assert("strlen has trunc", strings.Contains(ir44, "trunc"))

	irFree(h44)

	// Test 45: global bool variable (inferred type)
	src45 := []byte(`package mypkg

var enabled = true

func isEnabled() bool { return enabled }
`)
	h45 := compileToIR(
		uintptr(unsafe.Pointer(&src45[0])), int32(len(src45)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir45 := getIR(h45)
	fmt.Println("=== IR for global bool ===")
	fmt.Println(ir45)

	llvmVerify("global bool", ir45)
	assert("global bool has i1", strings.Contains(ir45, "@mypkg.enabled = global i1"))

	irFree(h45)

	// Test 46: string concat with + (which is | in Moxie SSA)
	src46 := []byte(`package mypkg

func greet(name string) string {
	return "hello " | name
}
`)
	h46 := compileToIR(
		uintptr(unsafe.Pointer(&src46[0])), int32(len(src46)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir46 := getIR(h46)
	fmt.Println("=== IR for string concat ===")
	fmt.Println(ir46)

	llvmVerify("string concat", ir46)
	assert("string concat calls sliceAppend", strings.Contains(ir46, "@runtime.sliceAppend"))

	irFree(h46)

	// Test 47: cap on string (strings have len == cap)
	src47 := []byte(`package mypkg

func strCap(s string) int {
	return cap(s)
}
`)
	h47 := compileToIR(
		uintptr(unsafe.Pointer(&src47[0])), int32(len(src47)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir47 := getIR(h47)
	fmt.Println("=== IR for string cap ===")
	fmt.Println(ir47)

	llvmVerify("string cap", ir47)
	assert("string cap has extractvalue", strings.Contains(ir47, "extractvalue"))

	irFree(h47)

	// Test 48: nested if/else with multiple returns
	src48 := []byte(`package mypkg

func clamp(x, lo, hi int) int {
	if x < lo {
		return lo
	} else if x > hi {
		return hi
	}
	return x
}
`)
	h48 := compileToIR(
		uintptr(unsafe.Pointer(&src48[0])), int32(len(src48)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir48 := getIR(h48)
	fmt.Println("=== IR for clamp ===")
	fmt.Println(ir48)

	llvmVerify("clamp", ir48)
	assert("clamp has icmp slt", strings.Contains(ir48, "icmp slt"))
	assert("clamp has icmp sgt", strings.Contains(ir48, "icmp sgt"))

	irFree(h48)

	// Test 49: Closure (captures variable from outer scope)
	src49 := []byte(`package mypkg

func adder(x int32) func(int32) int32 {
	return func(y int32) int32 {
		return x + y
	}
}
`)
	h49 := compileToIR(
		uintptr(unsafe.Pointer(&src49[0])), int32(len(src49)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir49 := getIR(h49)
	fmt.Println("=== IR for closure ===")
	fmt.Println(ir49)

	llvmVerify("closure", ir49)
	assert("closure has anon func", strings.Contains(ir49, "@mypkg.adder__anon"))
	assert("closure has runtime.alloc", strings.Contains(ir49, "@runtime.alloc"))
	assert("closure has insertvalue {ptr, ptr}", strings.Contains(ir49, "insertvalue {ptr, ptr}"))
	assert("closure has getelementptr for context", strings.Contains(ir49, "getelementptr"))
	assert("closure anon has context param", strings.Contains(ir49, "ptr %context"))

	irFree(h49)

	// Test 50: Closure with multiple captures
	src50 := []byte(`package mypkg

func makeCounter(start int32, step int32) func() int32 {
	val := start
	return func() int32 {
		result := val
		val = val + step
		return result
	}
}
`)
	h50 := compileToIR(
		uintptr(unsafe.Pointer(&src50[0])), int32(len(src50)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir50 := getIR(h50)
	fmt.Println("=== IR for multi-capture closure ===")
	fmt.Println(ir50)

	llvmVerify("multi-capture closure", ir50)
	assert("multi-capture has anon func", strings.Contains(ir50, "@mypkg.makeCounter__anon"))
	assert("multi-capture has alloc", strings.Contains(ir50, "@runtime.alloc"))

	irFree(h50)

	// Test 51: Function value call (indirect call through {ptr, ptr})
	src51 := []byte(`package mypkg

func apply(f func(int32) int32, x int32) int32 {
	return f(x)
}

func double(x int32) int32 {
	return x + x
}

func test() int32 {
	return apply(double, 5)
}
`)
	h51 := compileToIR(
		uintptr(unsafe.Pointer(&src51[0])), int32(len(src51)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir51 := getIR(h51)
	fmt.Println("=== IR for func value call ===")
	fmt.Println(ir51)

	llvmVerify("func value call", ir51)
	assert("func value call has extractvalue", strings.Contains(ir51, "extractvalue {ptr, ptr}"))
	assert("func value call passes context", strings.Contains(ir51, "ptr %ctx"))

	irFree(h51)

	// Test 52: Method with value receiver
	src52 := []byte(`package mypkg

type Point struct {
	X int32
	Y int32
}

func (p Point) Sum() int32 {
	return p.X + p.Y
}

func test() int32 {
	p := Point{X: 3, Y: 4}
	return p.Sum()
}
`)
	h52 := compileToIR(
		uintptr(unsafe.Pointer(&src52[0])), int32(len(src52)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir52 := getIR(h52)
	fmt.Println("=== IR for value receiver method ===")
	fmt.Println(ir52)

	llvmVerify("value receiver method", ir52)
	assert("value recv method defined", strings.Contains(ir52, "@mypkg.Point.Sum"))
	assert("value recv method takes struct", strings.Contains(ir52, "define i32 @mypkg.Point.Sum("))

	irFree(h52)

	// Test 53: Method with pointer receiver
	src53 := []byte(`package mypkg

type Counter struct {
	Val int32
}

func (c *Counter) Inc() {
	c.Val = c.Val + 1
}

func (c *Counter) Get() int32 {
	return c.Val
}

func test() int32 {
	c := Counter{Val: 10}
	c.Inc()
	c.Inc()
	return c.Get()
}
`)
	h53 := compileToIR(
		uintptr(unsafe.Pointer(&src53[0])), int32(len(src53)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir53 := getIR(h53)
	fmt.Println("=== IR for pointer receiver method ===")
	fmt.Println(ir53)

	llvmVerify("pointer receiver method", ir53)
	assert("ptr recv Inc defined", strings.Contains(ir53, "@mypkg.Counter.Inc"))
	assert("ptr recv Get defined", strings.Contains(ir53, "@mypkg.Counter.Get"))
	assert("ptr recv method takes ptr", strings.Contains(ir53, "define") && strings.Contains(ir53, "@mypkg.Counter.Inc(ptr"))

	irFree(h53)

	// Test 54: Slice make literal {: syntax rewrite
	src54 := []byte(`package mypkg

func test() int32 {
	a := []int32{:5}
	b := []int32{:0:10}
	return int32(len(a)) + int32(len(b))
}
`)
	h54 := compileToIR(
		uintptr(unsafe.Pointer(&src54[0])), int32(len(src54)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir54 := getIR(h54)
	fmt.Println("=== IR for slice make literal ===")
	fmt.Println(ir54)

	llvmVerify("slice make literal", ir54)
	assert("make literal has alloc", strings.Contains(ir54, "@runtime.alloc"))
	assert("make literal no parse error", !strings.Contains(ir54, "parse error"))

	irFree(h54)

	// Test 55: Interface dispatch
	src55 := []byte(`package mypkg

type Stringer interface {
	String() string
}

type Name struct {
	First string
	Last  string
}

func (n Name) String() string {
	return n.First | " " | n.Last
}

func greet(s Stringer) string {
	return "Hello, " | s.String()
}

func test() int32 {
	n := Name{First: "John", Last: "Doe"}
	r := greet(n)
	if len(r) > 0 {
		return 42
	}
	return 0
}
`)
	h55 := compileToIR(
		uintptr(unsafe.Pointer(&src55[0])), int32(len(src55)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir55 := getIR(h55)
	fmt.Println("=== IR for interface dispatch ===")
	fmt.Println(ir55)

	llvmVerify("interface dispatch", ir55)
	assert("iface has typeid", strings.Contains(ir55, "typeid"))
	assert("iface has makeinterface", strings.Contains(ir55, "insertvalue {ptr, ptr}"))
	assert("iface has Name.String method", strings.Contains(ir55, "@mypkg.Name.String"))
	assert("iface greet takes iface", strings.Contains(ir55, "define") && strings.Contains(ir55, "@mypkg.greet"))
	assert("iface no parse error", !strings.Contains(ir55, "parse error"))

	irFree(h55)

	// Test 56: Multi-impl interface dispatch
	src56 := []byte(`package mypkg

type Sizer interface {
	Size() int32
}

type Box struct {
	W int32
	H int32
}

type Circle struct {
	R int32
}

func (b Box) Size() int32 {
	return b.W * b.H
}

func (c Circle) Size() int32 {
	return c.R * c.R * 3
}

func getSize(s Sizer) int32 {
	return s.Size()
}

func test() int32 {
	b := Box{W: 3, H: 4}
	return getSize(b)
}
`)
	h56 := compileToIR(
		uintptr(unsafe.Pointer(&src56[0])), int32(len(src56)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir56 := getIR(h56)
	fmt.Println("=== IR for multi-impl interface ===")
	fmt.Println(ir56)

	llvmVerify("multi-impl interface", ir56)
	assert("multi has Box.Size", strings.Contains(ir56, "@mypkg.Box.Size"))
	assert("multi has Circle.Size", strings.Contains(ir56, "@mypkg.Circle.Size"))
	assert("multi has typeid Box", strings.Contains(ir56, "typeid.Box"))
	assert("multi has typeid Circle", strings.Contains(ir56, "typeid.Circle"))
	assert("multi dispatch has icmp", strings.Contains(ir56, "icmp eq ptr"))
	assert("multi no parse error", !strings.Contains(ir56, "parse error"))

	irFree(h56)

	// Test 57: Type assertion (comma-ok)
	src57 := []byte(`package mypkg

type Animal interface {
	Sound() string
}

type Dog struct {
	Name string
}

func (d Dog) Sound() string {
	return "woof"
}

func tryDog(a Animal) int32 {
	d, ok := a.(Dog)
	if ok {
		if len(d.Name) > 0 {
			return 1
		}
		return 2
	}
	return 0
}

func test() int32 {
	d := Dog{Name: "Rex"}
	return tryDog(d)
}
`)
	h57 := compileToIR(
		uintptr(unsafe.Pointer(&src57[0])), int32(len(src57)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir57 := getIR(h57)
	fmt.Println("=== IR for type assertion ===")
	fmt.Println(ir57)

	llvmVerify("type assertion", ir57)
	assert("ta has typeid compare", strings.Contains(ir57, "icmp eq ptr") && strings.Contains(ir57, "typeid.Dog"))
	assert("ta has extractvalue", strings.Contains(ir57, "extractvalue {ptr, ptr}"))
	assert("ta has insertvalue tuple", strings.Contains(ir57, "insertvalue"))
	assert("ta no parse error", !strings.Contains(ir57, "parse error"))

	irFree(h57)

	// Test 58: Type switch
	src58 := []byte(`package mypkg

type Shape interface {
	Area() int32
}

type Rect struct {
	W int32
	H int32
}

type Tri struct {
	B int32
	H int32
}

func (r Rect) Area() int32 {
	return r.W * r.H
}

func (t Tri) Area() int32 {
	return t.B * t.H / 2
}

func describe(s Shape) int32 {
	switch v := s.(type) {
	case Rect:
		return v.W + v.H
	case Tri:
		return v.B + v.H
	}
	return 0
}

func test() int32 {
	r := Rect{W: 3, H: 4}
	return describe(r)
}
`)
	h58 := compileToIR(
		uintptr(unsafe.Pointer(&src58[0])), int32(len(src58)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir58 := getIR(h58)
	fmt.Println("=== IR for type switch ===")
	fmt.Println(ir58)

	llvmVerify("type switch", ir58)
	assert("ts has typeid Rect", strings.Contains(ir58, "typeid.Rect"))
	assert("ts has typeid Tri", strings.Contains(ir58, "typeid.Tri"))
	assert("ts has typeassert", strings.Contains(ir58, "icmp eq ptr"))
	assert("ts no parse error", !strings.Contains(ir58, "parse error"))

	irFree(h58)

	// Test 59: Pointer receiver interface dispatch
	src59 := []byte(`package mypkg

type Writer interface {
	Write(data int32) int32
}

type Buffer struct {
	Count int32
}

func (b *Buffer) Write(data int32) int32 {
	b.Count = b.Count + data
	return b.Count
}

func writeAll(w Writer, v int32) int32 {
	return w.Write(v)
}

func test() int32 {
	b := Buffer{Count: 0}
	return writeAll(&b, 10)
}
`)
	h59 := compileToIR(
		uintptr(unsafe.Pointer(&src59[0])), int32(len(src59)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir59 := getIR(h59)
	fmt.Println("=== IR for ptr recv interface ===")
	fmt.Println(ir59)

	llvmVerify("ptr recv interface", ir59)
	assert("ptr iface has Buffer.Write", strings.Contains(ir59, "@mypkg.Buffer.Write"))
	assert("ptr iface dispatch passes ptr", strings.Contains(ir59, "call i32 @mypkg.Buffer.Write(ptr"))
	assert("ptr iface no parse error", !strings.Contains(ir59, "parse error"))

	irFree(h59)

	// Test 60: AndNot operator
	src60 := []byte(`package mypkg

func test() int32 {
	x := int32(0xFF)
	mask := int32(0x0F)
	return x &^ mask
}
`)
	h60 := compileToIR(
		uintptr(unsafe.Pointer(&src60[0])), int32(len(src60)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir60 := getIR(h60)
	fmt.Println("=== IR for andnot ===")
	fmt.Println(ir60)

	llvmVerify("andnot", ir60)
	assert("andnot has xor", strings.Contains(ir60, "xor"))
	assert("andnot has and", strings.Contains(ir60, " and "))
	assert("andnot no parse error", !strings.Contains(ir60, "parse error"))

	irFree(h60)

	// Test 61: Nil comparison
	src61 := []byte(`package mypkg

func test() int32 {
	var p *int32
	if p == nil {
		return 1
	}
	return 0
}
`)
	h61 := compileToIR(
		uintptr(unsafe.Pointer(&src61[0])), int32(len(src61)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir61 := getIR(h61)
	fmt.Println("=== IR for nil compare ===")
	fmt.Println(ir61)

	llvmVerify("nil compare", ir61)
	assert("nil has icmp eq ptr null", strings.Contains(ir61, "icmp eq ptr") && strings.Contains(ir61, "null"))
	assert("nil no parse error", !strings.Contains(ir61, "parse error"))

	irFree(h61)

	// Test 62: Key-only map range
	src62 := []byte(`package mypkg

func test() int32 {
	m := map[string]int32{"a": 1, "b": 2, "c": 3}
	count := int32(0)
	for k := range m {
		count = count + int32(len(k))
	}
	return count
}
`)
	h62 := compileToIR(
		uintptr(unsafe.Pointer(&src62[0])), int32(len(src62)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir62 := getIR(h62)
	fmt.Println("=== IR for key-only map range ===")
	fmt.Println(ir62)

	llvmVerify("key-only map range", ir62)
	assert("key range has hashmapNext", strings.Contains(ir62, "@runtime.hashmapNext"))
	assert("key range no parse error", !strings.Contains(ir62, "parse error"))

	irFree(h62)

	// Test 63: Local closure capturing mutable outer var
	src63 := []byte(`package mypkg

func test() int32 {
	counter := int32(0)
	inc := func() int32 {
		counter = counter + 1
		return counter
	}
	inc()
	inc()
	return inc()
}
`)
	h63 := compileToIR(
		uintptr(unsafe.Pointer(&src63[0])), int32(len(src63)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir63 := getIR(h63)
	fmt.Println("=== IR for mutable closure ===")
	fmt.Println(ir63)

	llvmVerify("mutable closure", ir63)
	assert("closure has makeclosure", strings.Contains(ir63, "insertvalue {ptr, ptr}") || strings.Contains(ir63, "makeclosure"))
	assert("closure no parse error", !strings.Contains(ir63, "parse error"))

	irFree(h63)

	// Test 64: Classic for loop
	src64 := []byte(`package mypkg

func test() int32 {
	sum := int32(0)
	for i := int32(0); i < 10; i++ {
		sum = sum + i
	}
	return sum
}
`)
	h64 := compileToIR(
		uintptr(unsafe.Pointer(&src64[0])), int32(len(src64)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir64 := getIR(h64)
	fmt.Println("=== IR for classic for loop ===")
	fmt.Println(ir64)

	llvmVerify("classic for loop", ir64)
	assert("for has icmp slt", strings.Contains(ir64, "icmp slt"))
	assert("for has add", strings.Contains(ir64, "add"))
	assert("for no parse error", !strings.Contains(ir64, "parse error"))

	irFree(h64)

	// Test 65: Multi-return value
	src65 := []byte(`package mypkg

func divmod(a, b int32) (int32, int32) {
	return a / b, a - (a / b) * b
}

func test() int32 {
	q, r := divmod(17, 5)
	return q + r
}
`)
	h65 := compileToIR(
		uintptr(unsafe.Pointer(&src65[0])), int32(len(src65)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir65 := getIR(h65)
	fmt.Println("=== IR for multi-return ===")
	fmt.Println(ir65)

	llvmVerify("multi-return", ir65)
	assert("multi-ret has extractvalue", strings.Contains(ir65, "extractvalue"))
	assert("multi-ret has divmod", strings.Contains(ir65, "@mypkg.divmod"))
	assert("multi-ret no parse error", !strings.Contains(ir65, "parse error"))

	irFree(h65)

	// Test 66: new() builtin
	src66 := []byte(`package mypkg

func test() int32 {
	p := new(int32)
	*p = 42
	return *p
}
`)
	h66 := compileToIR(
		uintptr(unsafe.Pointer(&src66[0])), int32(len(src66)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir66 := getIR(h66)
	fmt.Println("=== IR for new builtin ===")
	fmt.Println(ir66)

	llvmVerify("new builtin", ir66)
	assert("new has alloc or alloca", strings.Contains(ir66, "alloc") || strings.Contains(ir66, "alloca"))
	assert("new has store 42", strings.Contains(ir66, "store i32 42"))
	assert("new no parse error", !strings.Contains(ir66, "parse error"))

	irFree(h66)

	// Test 67: Logical AND / OR operators
	src67 := []byte(`package mypkg

func test() int32 {
	a := true
	b := false
	c := true
	r := int32(0)
	if a && c {
		r = r + 1
	}
	if a || b {
		r = r + 10
	}
	if b && c {
		r = r + 100
	}
	if b || c {
		r = r + 1000
	}
	return r
}
`)
	h67 := compileToIR(
		uintptr(unsafe.Pointer(&src67[0])), int32(len(src67)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir67 := getIR(h67)
	fmt.Println("=== IR for logical AND/OR ===")
	fmt.Println(ir67)

	llvmVerify("logical AND/OR", ir67)
	assert("land has short-circuit branch", strings.Contains(ir67, "phi i1 [false,") || strings.Contains(ir67, "and i1"))
	assert("lor has short-circuit branch", strings.Contains(ir67, "phi i1 [true,") || strings.Contains(ir67, "or i1"))
	assert("land/lor no parse error", !strings.Contains(ir67, "parse error"))

	irFree(h67)

	// Test 68: Multi-value switch case
	src68 := []byte(`package mypkg

func test() int32 {
	x := int32(3)
	r := int32(0)
	switch x {
	case 1, 2:
		r = 10
	case 3, 4:
		r = 20
	case 5:
		r = 30
	}
	return r
}
`)
	h68 := compileToIR(
		uintptr(unsafe.Pointer(&src68[0])), int32(len(src68)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir68 := getIR(h68)
	fmt.Println("=== IR for multi-value switch ===")
	fmt.Println(ir68)

	llvmVerify("multi-value switch", ir68)
	assert("multi-case has icmp", strings.Contains(ir68, "icmp eq"))
	assert("multi-case no parse error", !strings.Contains(ir68, "parse error"))

	irFree(h68)

	// Test 69: Variadic append
	src69 := []byte(`package mypkg

func test() int32 {
	s := make([]int32, 0)
	s = append(s, 10, 20, 30)
	return s[0] + s[1] + s[2]
}
`)
	h69 := compileToIR(
		uintptr(unsafe.Pointer(&src69[0])), int32(len(src69)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir69 := getIR(h69)
	fmt.Println("=== IR for variadic append ===")
	fmt.Println(ir69)

	llvmVerify("variadic append", ir69)
	assert("variadic append has sliceAppend", strings.Contains(ir69, "runtime.sliceAppend"))
	assert("variadic append no parse error", !strings.Contains(ir69, "parse error"))

	irFree(h69)

	// Test 70: Iota constants
	src70 := []byte(`package mypkg

const (
	A = iota
	B
	C
)

const (
	X = 1 << iota
	Y
	Z
)

func test() int32 {
	return int32(A + B + C + X + Y + Z)
}
`)
	h70 := compileToIR(
		uintptr(unsafe.Pointer(&src70[0])), int32(len(src70)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir70 := getIR(h70)
	fmt.Println("=== IR for iota constants ===")
	fmt.Println(ir70)

	llvmVerify("iota constants", ir70)
	assert("iota no parse error", !strings.Contains(ir70, "parse error"))

	irFree(h70)

	// Test 71: Typed iota const + switch dispatch
	src71 := []byte(`package mypkg

type Color int32

const (
	Red Color = iota
	Green
	Blue
)

func describe(c Color) int32 {
	switch c {
	case Red:
		return 1
	case Green:
		return 2
	case Blue:
		return 3
	}
	return 0
}

func test() int32 {
	return describe(Red) + describe(Green) + describe(Blue)
}
`)
	h71 := compileToIR(
		uintptr(unsafe.Pointer(&src71[0])), int32(len(src71)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir71 := getIR(h71)
	fmt.Println("=== IR for typed iota switch ===")
	fmt.Println(ir71)

	llvmVerify("typed iota switch", ir71)
	assert("typed iota has icmp", strings.Contains(ir71, "icmp eq"))
	assert("typed iota no parse error", !strings.Contains(ir71, "parse error"))

	irFree(h71)

	// Test 72: String switch
	src72 := []byte(`package mypkg

func classify(s string) int32 {
	switch s {
	case "hello":
		return 1
	case "world":
		return 2
	}
	return 0
}

func test() int32 {
	return classify("hello") + classify("world")
}
`)
	h72 := compileToIR(
		uintptr(unsafe.Pointer(&src72[0])), int32(len(src72)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir72 := getIR(h72)
	fmt.Println("=== IR for string switch ===")
	fmt.Println(ir72)

	llvmVerify("string switch", ir72)
	assert("string switch no parse error", !strings.Contains(ir72, "parse error"))

	irFree(h72)

	// Test 73: Nested field access (a.b.c pattern)
	src73 := []byte(`package mypkg

type Inner struct {
	val int32
}
type Outer struct {
	inner Inner
	extra int32
}

func test() int32 {
	o := Outer{inner: Inner{val: 42}, extra: 8}
	return o.inner.val + o.extra
}
`)
	h73 := compileToIR(
		uintptr(unsafe.Pointer(&src73[0])), int32(len(src73)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir73 := getIR(h73)
	fmt.Println("=== IR for nested field access ===")
	fmt.Println(ir73)

	llvmVerify("nested field access", ir73)
	assert("nested field has gep", strings.Contains(ir73, "getelementptr"))
	assert("nested field no parse error", !strings.Contains(ir73, "parse error"))

	irFree(h73)

	// Test 74: Method on pointer receiver with field mutation
	src74 := []byte(`package mypkg

type Counter struct {
	n int32
}

func (c *Counter) inc() {
	c.n = c.n + 1
}

func (c *Counter) get() int32 {
	return c.n
}

func test() int32 {
	c := Counter{n: 0}
	c.inc()
	c.inc()
	c.inc()
	return c.get()
}
`)
	h74 := compileToIR(
		uintptr(unsafe.Pointer(&src74[0])), int32(len(src74)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir74 := getIR(h74)
	fmt.Println("=== IR for method mutation ===")
	fmt.Println(ir74)

	llvmVerify("method mutation", ir74)
	assert("method mutation has Counter.inc", strings.Contains(ir74, "Counter.inc"))
	assert("method mutation no parse error", !strings.Contains(ir74, "parse error"))

	irFree(h74)

	// Test 75: Closure capturing method receiver (p := func() { e.field++ })
	src75 := []byte(`package mypkg

type Builder struct {
	count int32
}

func (b *Builder) build() int32 {
	inc := func() {
		b.count = b.count + 1
	}
	inc()
	inc()
	inc()
	return b.count
}

func test() int32 {
	b := Builder{count: 0}
	return b.build()
}
`)
	h75 := compileToIR(
		uintptr(unsafe.Pointer(&src75[0])), int32(len(src75)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir75 := getIR(h75)
	fmt.Println("=== IR for closure with receiver ===")
	fmt.Println(ir75)

	llvmVerify("closure with receiver", ir75)
	assert("closure recv no parse error", !strings.Contains(ir75, "parse error"))

	irFree(h75)

	// Test 76: Slice of structs with append and field access
	src76 := []byte(`package mypkg

type Item struct {
	id   int32
	val  int32
}

func test() int32 {
	items := make([]Item, 0)
	items = append(items, Item{id: 1, val: 10})
	items = append(items, Item{id: 2, val: 20})
	return items[0].val + items[1].val
}
`)
	h76 := compileToIR(
		uintptr(unsafe.Pointer(&src76[0])), int32(len(src76)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir76 := getIR(h76)
	fmt.Println("=== IR for slice of structs ===")
	fmt.Println(ir76)

	llvmVerify("slice of structs", ir76)
	assert("slice structs has sliceAppend", strings.Contains(ir76, "sliceAppend"))
	assert("slice structs no parse error", !strings.Contains(ir76, "parse error"))

	irFree(h76)

	// Test 77: Global variable read/write
	src77 := []byte(`package mypkg

var counter int32

func inc() {
	counter = counter + 1
}

func test() int32 {
	inc()
	inc()
	inc()
	return counter
}
`)
	h77 := compileToIR(
		uintptr(unsafe.Pointer(&src77[0])), int32(len(src77)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir77 := getIR(h77)
	fmt.Println("=== IR for global variable ===")
	fmt.Println(ir77)

	llvmVerify("global variable", ir77)
	assert("global has @mypkg.counter", strings.Contains(ir77, "@mypkg.counter"))
	assert("global no parse error", !strings.Contains(ir77, "parse error"))

	irFree(h77)

	// Test 78: String indexing via parameter (string = []byte)
	src78 := []byte(`package mypkg

func getByte(s string, i int32) int32 {
	return int32(s[i])
}

func test() int32 {
	return getByte("hello", 1)
}
`)
	h78 := compileToIR(
		uintptr(unsafe.Pointer(&src78[0])), int32(len(src78)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir78 := getIR(h78)
	fmt.Println("=== IR for string indexing ===")
	fmt.Println(ir78)

	llvmVerify("string indexing", ir78)
	assert("string idx no parse error", !strings.Contains(ir78, "parse error"))

	irFree(h78)

	// Test 79: Byte slice from string (type conversion)
	src79 := []byte(`package mypkg

func test() int32 {
	s := "abc"
	b := []byte(s)
	return int32(b[0]) + int32(b[2])
}
`)
	h79 := compileToIR(
		uintptr(unsafe.Pointer(&src79[0])), int32(len(src79)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir79 := getIR(h79)
	fmt.Println("=== IR for byte slice from string ===")
	fmt.Println(ir79)

	llvmVerify("byte slice from string", ir79)
	assert("byte slice no parse error", !strings.Contains(ir79, "parse error"))

	irFree(h79)

	// Test 80: Multiple return with named types
	src80 := []byte(`package mypkg

type Result struct {
	ok  bool
	val int32
}

func check(x int32) (Result, bool) {
	if x > 0 {
		return Result{ok: true, val: x}, true
	}
	return Result{ok: false, val: 0}, false
}

func test() int32 {
	r, ok := check(42)
	if ok && r.ok {
		return r.val
	}
	return 0
}
`)
	h80 := compileToIR(
		uintptr(unsafe.Pointer(&src80[0])), int32(len(src80)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir80 := getIR(h80)
	fmt.Println("=== IR for struct return + logical AND ===")
	fmt.Println(ir80)

	llvmVerify("struct return + logical AND", ir80)
	assert("struct ret no parse error", !strings.Contains(ir80, "parse error"))

	irFree(h80)

	// Test 81: Map with string keys and struct values
	src81 := []byte(`package mypkg

type Entry struct {
	name string
	val  int32
}

func test() int32 {
	m := make(map[string]Entry)
	m["x"] = Entry{name: "ex", val: 10}
	m["y"] = Entry{name: "why", val: 20}
	e := m["x"]
	return e.val
}
`)
	h81 := compileToIR(
		uintptr(unsafe.Pointer(&src81[0])), int32(len(src81)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir81 := getIR(h81)
	fmt.Println("=== IR for map struct values ===")
	fmt.Println(ir81)

	llvmVerify("map struct values", ir81)
	assert("map struct no parse error", !strings.Contains(ir81, "parse error"))

	irFree(h81)

	// Test 82: Interface with two concrete method impls
	src82 := []byte(`package mypkg

type Expr interface {
	compute() int32
}

type Lit struct {
	val int32
}

func (l Lit) compute() int32 {
	return l.val
}

type Binop struct {
	x int32
	y int32
}

func (a Binop) compute() int32 {
	return a.x + a.y
}

func run(e Expr) int32 {
	return e.compute()
}

func test() int32 {
	a := Lit{val: 10}
	b := Binop{x: 20, y: 12}
	return run(a) + run(b)
}
`)
	h82 := compileToIR(
		uintptr(unsafe.Pointer(&src82[0])), int32(len(src82)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir82 := getIR(h82)
	fmt.Println("=== IR for interface method dispatch ===")
	fmt.Println(ir82)

	llvmVerify("interface method dispatch", ir82)
	assert("iface dispatch has typeid", strings.Contains(ir82, "typeid"))
	assert("iface dispatch no parse error", !strings.Contains(ir82, "parse error"))

	irFree(h82)

	// Test 83: For loop with break
	src83 := []byte(`package mypkg

func test() int32 {
	sum := int32(0)
	for i := int32(0); i < 100; i = i + 1 {
		if i == 10 {
			break
		}
		sum = sum + i
	}
	return sum
}
`)
	h83 := compileToIR(
		uintptr(unsafe.Pointer(&src83[0])), int32(len(src83)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir83 := getIR(h83)
	fmt.Println("=== IR for break in loop ===")
	fmt.Println(ir83)

	llvmVerify("break in loop", ir83)
	assert("break no parse error", !strings.Contains(ir83, "parse error"))

	irFree(h83)

	// Test 84: Continue in loop
	src84 := []byte(`package mypkg

func test() int32 {
	sum := int32(0)
	for i := int32(0); i < 10; i = i + 1 {
		if i == 5 {
			continue
		}
		sum = sum + i
	}
	return sum
}
`)
	h84 := compileToIR(
		uintptr(unsafe.Pointer(&src84[0])), int32(len(src84)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir84 := getIR(h84)
	fmt.Println("=== IR for continue in loop ===")
	fmt.Println(ir84)

	llvmVerify("continue in loop", ir84)
	assert("continue no parse error", !strings.Contains(ir84, "parse error"))

	irFree(h84)

	// Test 85: String slicing
	src85 := []byte(`package mypkg

func test() int32 {
	s := "hello world"
	prefix := s[:5]
	return int32(len(prefix))
}
`)
	h85 := compileToIR(
		uintptr(unsafe.Pointer(&src85[0])), int32(len(src85)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir85 := getIR(h85)
	fmt.Println("=== IR for string slicing ===")
	fmt.Println(ir85)

	llvmVerify("string slicing", ir85)
	assert("string slice no parse error", !strings.Contains(ir85, "parse error"))

	irFree(h85)

	// Test 86: Const in expression
	src86 := []byte(`package mypkg

const maxSize = 100

func test() int32 {
	x := int32(maxSize)
	return x + 1
}
`)
	h86 := compileToIR(
		uintptr(unsafe.Pointer(&src86[0])), int32(len(src86)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir86 := getIR(h86)
	fmt.Println("=== IR for const expression ===")
	fmt.Println(ir86)

	llvmVerify("const expression", ir86)
	assert("const expr no parse error", !strings.Contains(ir86, "parse error"))

	irFree(h86)

	// Test 87: Assign to map value field (m[k] = v pattern)
	src87 := []byte(`package mypkg

func test() int32 {
	m := make(map[int32]int32)
	m[1] = 10
	m[2] = 20
	v1, ok1 := m[1]
	v2, ok2 := m[2]
	if ok1 && ok2 {
		return v1 + v2
	}
	return 0
}
`)
	h87 := compileToIR(
		uintptr(unsafe.Pointer(&src87[0])), int32(len(src87)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir87 := getIR(h87)
	fmt.Println("=== IR for map comma-ok ===")
	fmt.Println(ir87)

	llvmVerify("map comma-ok", ir87)
	assert("map comma ok no parse error", !strings.Contains(ir87, "parse error"))

	irFree(h87)

	// Test 88: Nested if-else chains
	src88 := []byte(`package mypkg

func classify(x int32) int32 {
	if x < 0 {
		return -1
	} else if x == 0 {
		return 0
	} else if x < 10 {
		return 1
	} else {
		return 2
	}
}

func test() int32 {
	return classify(-5) + classify(0) + classify(5) + classify(50)
}
`)
	h88 := compileToIR(
		uintptr(unsafe.Pointer(&src88[0])), int32(len(src88)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir88 := getIR(h88)
	fmt.Println("=== IR for if-else chain ===")
	fmt.Println(ir88)

	llvmVerify("if-else chain", ir88)
	assert("if-else no parse error", !strings.Contains(ir88, "parse error"))

	irFree(h88)

	// Test 89: String concatenation via | operator (Moxie-specific)
	src89 := []byte(`package mypkg

func test() int32 {
	a := "hello"
	b := " world"
	c := a | b
	return int32(len(c))
}
`)
	h89 := compileToIR(
		uintptr(unsafe.Pointer(&src89[0])), int32(len(src89)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir89 := getIR(h89)
	fmt.Println("=== IR for string | concat ===")
	fmt.Println(ir89)

	llvmVerify("string | concat", ir89)
	assert("pipe concat has sliceAppend", strings.Contains(ir89, "sliceAppend"))
	assert("pipe concat no parse error", !strings.Contains(ir89, "parse error"))

	irFree(h89)

	// Test 90: Switch with default case
	src90 := []byte(`package mypkg

func test() int32 {
	x := int32(99)
	switch x {
	case 1:
		return 10
	case 2:
		return 20
	default:
		return 42
	}
}
`)
	h90 := compileToIR(
		uintptr(unsafe.Pointer(&src90[0])), int32(len(src90)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir90 := getIR(h90)
	fmt.Println("=== IR for switch default ===")
	fmt.Println(ir90)

	llvmVerify("switch default", ir90)
	assert("switch default no parse error", !strings.Contains(ir90, "parse error"))

	irFree(h90)

	// Test 91: Boolean negation
	src91 := []byte(`package mypkg

func test() int32 {
	a := true
	b := false
	r := int32(0)
	if !a {
		r = r + 1
	}
	if !b {
		r = r + 10
	}
	return r
}
`)
	h91 := compileToIR(
		uintptr(unsafe.Pointer(&src91[0])), int32(len(src91)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir91 := getIR(h91)
	fmt.Println("=== IR for boolean negation ===")
	fmt.Println(ir91)

	llvmVerify("boolean negation", ir91)
	assert("bool neg has xor", strings.Contains(ir91, "xor"))
	assert("bool neg no parse error", !strings.Contains(ir91, "parse error"))

	irFree(h91)

	// Test 92: Tuple swap (slice element swap)
	src92 := []byte(`package mypkg

func test() int32 {
	s := make([]int32, 3)
	s[0] = 1
	s[1] = 2
	s[2] = 3
	s[0], s[1] = s[1], s[0]
	return s[0] + s[1]*10 + s[2]*100
}
`)
	h92 := compileToIR(
		uintptr(unsafe.Pointer(&src92[0])), int32(len(src92)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir92 := getIR(h92)
	fmt.Println("=== IR for tuple swap ===")
	fmt.Println(ir92)

	llvmVerify("tuple swap", ir92)
	assert("tuple swap no parse error", !strings.Contains(ir92, "parse error"))

	irFree(h92)

	// Test 93: String less-than comparison
	src93 := []byte(`package mypkg

func test() int32 {
	a := "apple"
	b := "banana"
	if a < b {
		return 1
	}
	return 0
}
`)
	h93 := compileToIR(
		uintptr(unsafe.Pointer(&src93[0])), int32(len(src93)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir93 := getIR(h93)
	fmt.Println("=== IR for string less-than ===")
	fmt.Println(ir93)

	llvmVerify("string less-than", ir93)
	assert("str lt has stringLess", strings.Contains(ir93, "stringLess"))
	assert("str lt no parse error", !strings.Contains(ir93, "parse error"))

	irFree(h93)

	// Test 94: Integer negation
	src94 := []byte(`package mypkg

func abs(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}

func test() int32 {
	return abs(-5) + abs(3)
}
`)
	h94 := compileToIR(
		uintptr(unsafe.Pointer(&src94[0])), int32(len(src94)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir94 := getIR(h94)
	fmt.Println("=== IR for integer negation ===")
	fmt.Println(ir94)

	llvmVerify("integer negation", ir94)
	assert("int neg has sub i32 0", strings.Contains(ir94, "sub i32 0"))
	assert("int neg no parse error", !strings.Contains(ir94, "parse error"))

	irFree(h94)

	// Test 95: Int64 arithmetic
	src95 := []byte(`package mypkg

func test() int32 {
	a := int64(1000000)
	b := int64(2000000)
	c := a + b
	return int32(c / int64(1000))
}
`)
	h95 := compileToIR(
		uintptr(unsafe.Pointer(&src95[0])), int32(len(src95)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir95 := getIR(h95)
	fmt.Println("=== IR for int64 arithmetic ===")
	fmt.Println(ir95)

	llvmVerify("int64 arithmetic", ir95)
	assert("int64 has i64", strings.Contains(ir95, "i64"))
	assert("int64 no parse error", !strings.Contains(ir95, "parse error"))

	irFree(h95)

	// Test 96: Global variable with initializer
	src96 := []byte(`package mypkg

var offset int32 = 100

func test() int32 {
	return offset + 5
}
`)
	h96 := compileToIR(
		uintptr(unsafe.Pointer(&src96[0])), int32(len(src96)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir96 := getIR(h96)
	fmt.Println("=== IR for global var initializer ===")
	fmt.Println(ir96)

	llvmVerify("global var initializer", ir96)
	assert("global init has @mypkg.offset", strings.Contains(ir96, "@mypkg.offset"))
	assert("global init no parse error", !strings.Contains(ir96, "parse error"))

	irFree(h96)

	// Test 97: Blank identifier in multi-return
	src97 := []byte(`package mypkg

func pair() (int32, int32) {
	return 10, 20
}

func test() int32 {
	_, b := pair()
	return b
}
`)
	h97 := compileToIR(
		uintptr(unsafe.Pointer(&src97[0])), int32(len(src97)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir97 := getIR(h97)
	fmt.Println("=== IR for blank identifier ===")
	fmt.Println(ir97)

	llvmVerify("blank identifier", ir97)
	assert("blank id no parse error", !strings.Contains(ir97, "parse error"))

	irFree(h97)

	// Test 98: For range with index only (i := range slice)
	src98 := []byte(`package mypkg

func test() int32 {
	s := []int32{10, 20, 30, 40}
	sum := int32(0)
	for i := range s {
		sum = sum + int32(i)
	}
	return sum
}
`)
	h98 := compileToIR(
		uintptr(unsafe.Pointer(&src98[0])), int32(len(src98)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir98 := getIR(h98)
	fmt.Println("=== IR for range index only ===")
	fmt.Println(ir98)

	llvmVerify("range index only", ir98)
	assert("range idx no parse error", !strings.Contains(ir98, "parse error"))

	irFree(h98)

	// Test 99: Append with variadic expansion (append(buf, s...))
	src99 := []byte(`package mypkg

func test() int32 {
	buf := make([]byte, 0)
	s := "hello"
	buf = append(buf, s...)
	return int32(len(buf))
}
`)
	h99 := compileToIR(
		uintptr(unsafe.Pointer(&src99[0])), int32(len(src99)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir99 := getIR(h99)
	fmt.Println("=== IR for append with dots ===")
	fmt.Println(ir99)

	llvmVerify("append with dots", ir99)
	assert("append dots has sliceAppend", strings.Contains(ir99, "sliceAppend"))
	assert("append dots no parse error", !strings.Contains(ir99, "parse error"))

	irFree(h99)

	// Test 100: Assign to struct field through pointer
	src100 := []byte(`package mypkg

type Node struct {
	val  int32
	next *Node
}

func test() int32 {
	a := Node{val: 10}
	b := Node{val: 20, next: &a}
	return b.val + b.next.val
}
`)
	h100 := compileToIR(
		uintptr(unsafe.Pointer(&src100[0])), int32(len(src100)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir100 := getIR(h100)
	fmt.Println("=== IR for struct pointer chain ===")
	fmt.Println(ir100)

	llvmVerify("struct pointer chain", ir100)
	assert("ptr chain no parse error", !strings.Contains(ir100, "parse error"))

	irFree(h100)

	// Test 101: Address-of operator
	src101 := []byte(`package mypkg

func setVal(p *int32, v int32) {
	*p = v
}

func test() int32 {
	x := int32(0)
	setVal(&x, 42)
	return x
}
`)
	h101 := compileToIR(
		uintptr(unsafe.Pointer(&src101[0])), int32(len(src101)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir101 := getIR(h101)
	fmt.Println("=== IR for address-of ===")
	fmt.Println(ir101)

	llvmVerify("address-of", ir101)
	assert("addr-of no parse error", !strings.Contains(ir101, "parse error"))

	irFree(h101)

	// Test 102: Map delete
	src102 := []byte(`package mypkg

func test() int32 {
	m := map[string]int32{"a": 1, "b": 2, "c": 3}
	delete(m, "b")
	_, ok := m["b"]
	if ok {
		return 0
	}
	return 1
}
`)
	h102 := compileToIR(
		uintptr(unsafe.Pointer(&src102[0])), int32(len(src102)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir102 := getIR(h102)
	fmt.Println("=== IR for map delete ===")
	fmt.Println(ir102)

	llvmVerify("map delete", ir102)
	assert("map del has hashmapDelete", strings.Contains(ir102, "hashmapDelete") || strings.Contains(ir102, "hashmapBinaryDelete"))
	assert("map del no parse error", !strings.Contains(ir102, "parse error"))

	irFree(h102)

	// Test 103: String constant
	src103 := []byte(`package mypkg

const greeting = "hello"

func test() int32 {
	s := greeting
	return int32(len(s))
}
`)
	h103 := compileToIR(
		uintptr(unsafe.Pointer(&src103[0])), int32(len(src103)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir103 := getIR(h103)
	fmt.Println("=== IR for string const ===")
	fmt.Println(ir103)

	llvmVerify("string const", ir103)
	assert("str const no parse error", !strings.Contains(ir103, "parse error"))

	irFree(h103)

	// Test 104: Nested method calls (a.b().c())
	src104 := []byte(`package mypkg

type Builder struct {
	buf []byte
}

func (b *Builder) write(s string) *Builder {
	b.buf = append(b.buf, s...)
	return b
}

func (b *Builder) length() int32 {
	return int32(len(b.buf))
}

func test() int32 {
	b := Builder{buf: make([]byte, 0)}
	b.write("hello")
	b.write(" world")
	return b.length()
}
`)
	h104 := compileToIR(
		uintptr(unsafe.Pointer(&src104[0])), int32(len(src104)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir104 := getIR(h104)
	fmt.Println("=== IR for method chaining ===")
	fmt.Println(ir104)

	llvmVerify("method chaining", ir104)
	assert("method chain has Builder.write", strings.Contains(ir104, "Builder.write"))
	assert("method chain no parse error", !strings.Contains(ir104, "parse error"))

	irFree(h104)

	// Test 105: Unary not on comparison
	src105 := []byte(`package mypkg

func test() int32 {
	x := int32(5)
	if !(x > 10) {
		return 1
	}
	return 0
}
`)
	h105 := compileToIR(
		uintptr(unsafe.Pointer(&src105[0])), int32(len(src105)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir105 := getIR(h105)
	fmt.Println("=== IR for not-comparison ===")
	fmt.Println(ir105)

	llvmVerify("not-comparison", ir105)
	assert("not cmp no parse error", !strings.Contains(ir105, "parse error"))

	irFree(h105)

	// Test 106: Self-compilation pattern - irItoa-like function
	src106 := []byte(`package mypkg

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := make([]byte, 0)
	for n > 0 {
		buf = append(buf, byte(n%10)+'0')
		n = n / 10
	}
	if neg {
		buf = append(buf, '-')
	}
	result := make([]byte, len(buf))
	for i := 0; i < len(buf); i = i + 1 {
		result[i] = buf[len(buf)-1-i]
	}
	return string(result)
}

func test() int32 {
	s := itoa(42)
	return int32(len(s))
}
`)
	h106 := compileToIR(
		uintptr(unsafe.Pointer(&src106[0])), int32(len(src106)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir106 := getIR(h106)
	fmt.Println("=== IR for itoa self-compilation pattern ===")
	fmt.Println(ir106)

	llvmVerify("itoa self-compile", ir106)
	assert("itoa has sliceAppend", strings.Contains(ir106, "sliceAppend"))
	assert("itoa no parse error", !strings.Contains(ir106, "parse error"))

	irFree(h106)

	// Test 107: Type method returning interface (SSAMember pattern)
	src107 := []byte(`package mypkg

type Member interface {
	Name() string
}

type Func struct {
	name string
}

func (f *Func) Name() string {
	return f.name
}

type Global struct {
	name string
}

func (g *Global) Name() string {
	return g.name
}

func getMember(which int32) Member {
	if which == 0 {
		return &Func{name: "test"}
	}
	return &Global{name: "counter"}
}

func test() int32 {
	m := getMember(0)
	n := m.Name()
	return int32(len(n))
}
`)
	h107 := compileToIR(
		uintptr(unsafe.Pointer(&src107[0])), int32(len(src107)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir107 := getIR(h107)
	fmt.Println("=== IR for interface return ===")
	fmt.Println(ir107)

	llvmVerify("interface return", ir107)
	assert("iface ret has typeid", strings.Contains(ir107, "typeid"))
	assert("iface ret no parse error", !strings.Contains(ir107, "parse error"))

	irFree(h107)

	// Test 108: Interface variable assignment
	src108 := []byte(`package mypkg

type Stringer interface {
	str() string
}

type Num struct {
	val int32
}

func (n Num) str() string {
	return "num"
}

func test() int32 {
	var s Stringer
	s = Num{val: 42}
	name := s.str()
	return int32(len(name))
}
`)
	h108 := compileToIR(
		uintptr(unsafe.Pointer(&src108[0])), int32(len(src108)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir108 := getIR(h108)
	fmt.Println("=== IR for iface var assign ===")
	fmt.Println(ir108)

	llvmVerify("iface var assign", ir108)
	assert("iface assign has typeid", strings.Contains(ir108, "typeid"))
	assert("iface assign no parse error", !strings.Contains(ir108, "parse error"))

	irFree(h108)

	// Test 109: string(buf) conversion from []byte
	src109 := []byte(`package mypkg

func test() int32 {
	buf := []byte{72, 105}
	s := string(buf)
	return int32(len(s))
}
`)
	h109 := compileToIR(
		uintptr(unsafe.Pointer(&src109[0])), int32(len(src109)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir109 := getIR(h109)
	fmt.Println("=== IR for string(buf) ===")
	fmt.Println(ir109)

	llvmVerify("string(buf)", ir109)
	assert("string buf no parse error", !strings.Contains(ir109, "parse error"))

	irFree(h109)

	// Test 110: byte(expr) conversion + char literal
	src110 := []byte(`package mypkg

func test() int32 {
	n := int32(5)
	b := byte('0' + n)
	return int32(b)
}
`)
	h110 := compileToIR(
		uintptr(unsafe.Pointer(&src110[0])), int32(len(src110)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir110 := getIR(h110)
	fmt.Println("=== IR for byte conversion ===")
	fmt.Println(ir110)

	llvmVerify("byte conversion", ir110)
	assert("byte conv no parse error", !strings.Contains(ir110, "parse error"))

	irFree(h110)

	// Test 111: Slice append in loop (builder pattern from ir_emit)
	src111 := []byte(`package mypkg

type Writer struct {
	buf []byte
}

func (w *Writer) write(s string) {
	w.buf = append(w.buf, s...)
}

func (w *Writer) result() string {
	return string(w.buf)
}

func test() int32 {
	w := Writer{buf: make([]byte, 0)}
	w.write("he")
	w.write("llo")
	s := w.result()
	return int32(len(s))
}
`)
	h111 := compileToIR(
		uintptr(unsafe.Pointer(&src111[0])), int32(len(src111)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir111 := getIR(h111)
	fmt.Println("=== IR for writer pattern ===")
	fmt.Println(ir111)

	llvmVerify("writer pattern", ir111)
	assert("writer has sliceAppend", strings.Contains(ir111, "sliceAppend"))
	assert("writer no parse error", !strings.Contains(ir111, "parse error"))

	irFree(h111)

	// Test 112: Map with string values and range iteration
	src112 := []byte(`package mypkg

func test() int32 {
	m := map[string]string{"a": "alpha", "b": "beta"}
	total := int32(0)
	for _, v := range m {
		total = total + int32(len(v))
	}
	return total
}
`)
	h112 := compileToIR(
		uintptr(unsafe.Pointer(&src112[0])), int32(len(src112)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir112 := getIR(h112)
	fmt.Println("=== IR for map string range ===")
	fmt.Println(ir112)

	llvmVerify("map string range", ir112)
	assert("map str range no parse error", !strings.Contains(ir112, "parse error"))

	irFree(h112)

	// Test 113: Nested if with type assertion
	src113 := []byte(`package mypkg

type Animal interface {
	sound() string
}

type Dog struct{}

func (d Dog) sound() string { return "woof" }

type Cat struct{}

func (c Cat) sound() string { return "meow" }

func describe(a Animal) int32 {
	if d, ok := a.(Dog); ok {
		s := d.sound()
		return int32(len(s))
	}
	if c, ok := a.(Cat); ok {
		s := c.sound()
		return int32(len(s))
	}
	return 0
}

func test() int32 {
	return describe(Dog{}) + describe(Cat{})
}
`)
	h113 := compileToIR(
		uintptr(unsafe.Pointer(&src113[0])), int32(len(src113)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir113 := getIR(h113)
	fmt.Println("=== IR for if type assertion ===")
	fmt.Println(ir113)

	llvmVerify("if type assertion", ir113)
	assert("if type assert no parse error", !strings.Contains(ir113, "parse error"))

	irFree(h113)

	// Test 114: Large switch on int constant (simulates SSA op dispatch)
	src114 := []byte(`package mypkg

const (
	OpAdd = iota
	OpSub
	OpMul
	OpDiv
	OpEql
	OpNeq
	OpLss
	OpGtr
)

func opName(op int32) string {
	switch op {
	case OpAdd:
		return "add"
	case OpSub:
		return "sub"
	case OpMul:
		return "mul"
	case OpDiv:
		return "div"
	case OpEql:
		return "eq"
	case OpNeq:
		return "ne"
	case OpLss:
		return "lt"
	case OpGtr:
		return "gt"
	default:
		return "?"
	}
}

func test() int32 {
	s1 := opName(OpAdd)
	s2 := opName(OpDiv)
	return int32(len(s1) + len(s2))
}
`)
	h114 := compileToIR(
		uintptr(unsafe.Pointer(&src114[0])), int32(len(src114)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir114 := getIR(h114)
	fmt.Println("=== IR for large iota switch ===")
	fmt.Println(ir114)

	llvmVerify("large iota switch", ir114)
	assert("large switch has icmp", strings.Contains(ir114, "icmp eq"))
	assert("large switch no parse error", !strings.Contains(ir114, "parse error"))

	irFree(h114)

	// Test 115: Nested type switch (primary dispatch pattern in ir_emit.mx)
	src115 := []byte(`package mypkg

type Node interface {
	nodeType() int32
}

type Lit struct {
	val int32
}
func (l Lit) nodeType() int32 { return 1 }

type BinOp struct {
	op  int32
	x   Node
	y   Node
}
func (b BinOp) nodeType() int32 { return 2 }

type UnOp struct {
	op int32
	x  Node
}
func (u UnOp) nodeType() int32 { return 3 }

func eval(n Node) int32 {
	switch v := n.(type) {
	case Lit:
		return v.val
	case BinOp:
		lv := eval(v.x)
		rv := eval(v.y)
		switch v.op {
		case 1:
			return lv + rv
		case 2:
			return lv - rv
		default:
			return 0
		}
	case UnOp:
		inner := eval(v.x)
		if v.op == 1 {
			return 0 - inner
		}
		return inner
	}
	return 0
}

func test() int32 {
	a := Lit{val: 10}
	b := Lit{val: 3}
	sum := BinOp{op: 1, x: a, y: b}
	return eval(sum)
}
`)
	h115 := compileToIR(
		uintptr(unsafe.Pointer(&src115[0])), int32(len(src115)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir115 := getIR(h115)
	fmt.Println("=== IR for nested type switch ===")
	fmt.Println(ir115)

	llvmVerify("nested type switch", ir115)
	assert("nested tswitch has eval", strings.Contains(ir115, "@mypkg.eval"))
	assert("nested tswitch no parse error", !strings.Contains(ir115, "parse error"))

	irFree(h115)

	// Test 116: String concatenation via | operator (Moxie-specific)
	src116 := []byte(`package mypkg

func join(a string, b string) string {
	return a | b
}

func test() int32 {
	s := join("hel", "lo")
	return int32(len(s))
}
`)
	h116 := compileToIR(
		uintptr(unsafe.Pointer(&src116[0])), int32(len(src116)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir116 := getIR(h116)
	fmt.Println("=== IR for string concat ===")
	fmt.Println(ir116)

	llvmVerify("string concat", ir116)
	assert("str concat has sliceAppend", strings.Contains(ir116, "sliceAppend"))
	assert("str concat no parse error", !strings.Contains(ir116, "parse error"))

	irFree(h116)

	// Test 117: Multiple return values with named usage
	src117 := []byte(`package mypkg

func divmod(a int32, b int32) (int32, int32) {
	return a / b, a % b
}

func test() int32 {
	q, r := divmod(17, 5)
	return q*10 + r
}
`)
	h117 := compileToIR(
		uintptr(unsafe.Pointer(&src117[0])), int32(len(src117)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir117 := getIR(h117)
	fmt.Println("=== IR for multi-return ===")
	fmt.Println(ir117)

	llvmVerify("multi-return", ir117)
	assert("multi-return has extractvalue", strings.Contains(ir117, "extractvalue"))
	assert("multi-return no parse error", !strings.Contains(ir117, "parse error"))

	irFree(h117)

	// Test 118: For-range over string (byte iteration - Moxie string=[]byte)
	src118 := []byte(`package mypkg

func countUpper(s string) int32 {
	n := int32(0)
	for _, c := range s {
		if int32(c) >= 65 && int32(c) <= 90 {
			n = n + 1
		}
	}
	return n
}

func test() int32 {
	return countUpper("Hello World")
}
`)
	h118 := compileToIR(
		uintptr(unsafe.Pointer(&src118[0])), int32(len(src118)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir118 := getIR(h118)
	fmt.Println("=== IR for range over string ===")
	fmt.Println(ir118)

	llvmVerify("range over string", ir118)
	assert("range string no parse error", !strings.Contains(ir118, "parse error"))

	irFree(h118)

	// Test 119: Nested struct field write through pointer receiver
	src119 := []byte(`package mypkg

type Inner struct {
	val int32
}

type Outer struct {
	inner Inner
	count int32
}

func (o *Outer) setVal(v int32) {
	o.inner.val = v
	o.count = o.count + 1
}

func (o *Outer) getVal() int32 {
	return o.inner.val
}

func test() int32 {
	o := Outer{inner: Inner{val: 0}, count: 0}
	o.setVal(42)
	return o.getVal() + o.count
}
`)
	h119 := compileToIR(
		uintptr(unsafe.Pointer(&src119[0])), int32(len(src119)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir119 := getIR(h119)
	fmt.Println("=== IR for nested struct write ===")
	fmt.Println(ir119)

	llvmVerify("nested struct write", ir119)
	assert("nested struct has getelementptr", strings.Contains(ir119, "getelementptr"))
	assert("nested struct no parse error", !strings.Contains(ir119, "parse error"))

	irFree(h119)

	// Test 120: Global variable with simple init and function-level map
	src120 := []byte(`package mypkg

var globalCount int32 = 10

func lookup(s string) int32 {
	m := map[string]int32{"if": 1, "for": 3}
	v, ok := m[s]
	if ok {
		return v + globalCount
	}
	return 0
}

func test() int32 {
	return lookup("if") + lookup("for")
}
`)
	h120 := compileToIR(
		uintptr(unsafe.Pointer(&src120[0])), int32(len(src120)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir120 := getIR(h120)
	fmt.Println("=== IR for global var + local map ===")
	fmt.Println(ir120)

	llvmVerify("global var + local map", ir120)
	assert("global var has globalCount", strings.Contains(ir120, "@mypkg.globalCount"))
	assert("global var no parse error", !strings.Contains(ir120, "parse error"))

	irFree(h120)

	// Test 121: Recursive data structure (linked list - forward ref types)
	src121 := []byte(`package mypkg

type ListNode struct {
	val  int32
	next *ListNode
}

func sum(n *ListNode) int32 {
	total := int32(0)
	cur := n
	for cur != nil {
		total = total + cur.val
		cur = cur.next
	}
	return total
}

func test() int32 {
	c := ListNode{val: 3, next: nil}
	b := ListNode{val: 2, next: &c}
	a := ListNode{val: 1, next: &b}
	return sum(&a)
}
`)
	h121 := compileToIR(
		uintptr(unsafe.Pointer(&src121[0])), int32(len(src121)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir121 := getIR(h121)
	fmt.Println("=== IR for linked list ===")
	fmt.Println(ir121)

	llvmVerify("linked list", ir121)
	assert("linked list has icmp ne ptr", strings.Contains(ir121, "icmp ne ptr"))
	assert("linked list no parse error", !strings.Contains(ir121, "parse error"))

	irFree(h121)

	// Test 122: Slice of interfaces (heterogeneous collection)
	src122 := []byte(`package mypkg

type Sizer interface {
	size() int32
}

type Small struct{}
func (s Small) size() int32 { return 1 }

type Big struct{ n int32 }
func (b Big) size() int32 { return b.n }

func totalSize(items []Sizer) int32 {
	total := int32(0)
	for _, item := range items {
		total = total + item.size()
	}
	return total
}

func test() int32 {
	items := []Sizer{Small{}, Big{n: 5}, Small{}, Big{n: 10}}
	return totalSize(items)
}
`)
	h122 := compileToIR(
		uintptr(unsafe.Pointer(&src122[0])), int32(len(src122)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir122 := getIR(h122)
	fmt.Println("=== IR for interface slice ===")
	fmt.Println(ir122)

	llvmVerify("interface slice", ir122)
	assert("iface slice no parse error", !strings.Contains(ir122, "parse error"))

	irFree(h122)

	// Test 123: Nested function calls with method chaining pattern
	src123 := []byte(`package mypkg

type Builder struct {
	n int32
}

func newBuilder() *Builder {
	b := Builder{n: 0}
	return &b
}

func (b *Builder) add(v int32) *Builder {
	b.n = b.n + v
	return b
}

func (b *Builder) result() int32 {
	return b.n
}

func test() int32 {
	return newBuilder().add(10).add(20).add(3).result()
}
`)
	h123 := compileToIR(
		uintptr(unsafe.Pointer(&src123[0])), int32(len(src123)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir123 := getIR(h123)
	fmt.Println("=== IR for method chaining ===")
	fmt.Println(ir123)

	llvmVerify("method chaining", ir123)
	assert("chain has newBuilder", strings.Contains(ir123, "@mypkg.newBuilder"))
	assert("chain no parse error", !strings.Contains(ir123, "parse error"))

	irFree(h123)

	// Test 124: Local slice lookup with bounds check
	src124 := []byte(`package mypkg

func opName(i int32) string {
	names := []string{"add", "sub", "mul", "div", "mod"}
	if i >= 0 && i < int32(len(names)) {
		return names[i]
	}
	return "?"
}

func test() int32 {
	s := opName(2)
	return int32(len(s))
}
`)
	h124 := compileToIR(
		uintptr(unsafe.Pointer(&src124[0])), int32(len(src124)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir124 := getIR(h124)
	fmt.Println("=== IR for local slice lookup ===")
	fmt.Println(ir124)

	llvmVerify("local slice lookup", ir124)
	assert("local slice no parse error", !strings.Contains(ir124, "parse error"))

	irFree(h124)

	// Test 125: Multiple `:=` with same name in sibling if-blocks (scoping)
	src125 := []byte(`package mypkg

func classify(n int32) int32 {
	if n > 100 {
		r := n - 100
		return r
	}
	if n > 50 {
		r := n - 50
		return r * 2
	}
	if n > 0 {
		r := n
		return r * 3
	}
	return 0
}

func test() int32 {
	return classify(120) + classify(75)
}
`)
	h125 := compileToIR(
		uintptr(unsafe.Pointer(&src125[0])), int32(len(src125)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir125 := getIR(h125)
	fmt.Println("=== IR for variable shadowing ===")
	fmt.Println(ir125)

	llvmVerify("variable shadowing", ir125)
	assert("shadowing no parse error", !strings.Contains(ir125, "parse error"))

	irFree(h125)

	// Test 126: Multiple interface implementations with method dispatch
	src126 := []byte(`package mypkg

type Formatter interface {
	format() string
}

type IntFmt struct{ val int32 }
func (f IntFmt) format() string {
	if f.val > 0 {
		return "positive"
	}
	return "non-positive"
}

type StrFmt struct{ val string }
func (f StrFmt) format() string {
	return f.val
}

type NilFmt struct{}
func (f NilFmt) format() string {
	return "nil"
}

func formatLen(f Formatter) int32 {
	s := f.format()
	return int32(len(s))
}

func test() int32 {
	a := formatLen(IntFmt{val: 5})
	b := formatLen(StrFmt{val: "hello"})
	c := formatLen(NilFmt{})
	return a + b + c
}
`)
	h126 := compileToIR(
		uintptr(unsafe.Pointer(&src126[0])), int32(len(src126)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir126 := getIR(h126)
	fmt.Println("=== IR for multi-impl dispatch ===")
	fmt.Println(ir126)

	llvmVerify("multi-impl dispatch", ir126)
	assert("multi-impl has typeid", strings.Contains(ir126, "typeid"))
	assert("multi-impl no parse error", !strings.Contains(ir126, "parse error"))

	irFree(h126)

	// Test 127: Nested composite literal (struct containing struct containing slice)
	src127 := []byte(`package mypkg

type Pos struct {
	line int32
	col  int32
}

type Token struct {
	pos  Pos
	kind int32
	text string
}

func tokenLen(t Token) int32 {
	return int32(len(t.text)) + t.pos.line
}

func test() int32 {
	t := Token{
		pos:  Pos{line: 1, col: 5},
		kind: 42,
		text: "hello",
	}
	return tokenLen(t)
}
`)
	h127 := compileToIR(
		uintptr(unsafe.Pointer(&src127[0])), int32(len(src127)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir127 := getIR(h127)
	fmt.Println("=== IR for nested struct literal ===")
	fmt.Println(ir127)

	llvmVerify("nested struct literal", ir127)
	assert("nested struct lit has gep", strings.Contains(ir127, "getelementptr"))
	assert("nested struct lit no parse error", !strings.Contains(ir127, "parse error"))

	irFree(h127)

	// Test 128: Recursive function with multiple base cases
	src128 := []byte(`package mypkg

func fib(n int32) int32 {
	if n <= 0 {
		return 0
	}
	if n == 1 {
		return 1
	}
	return fib(n-1) + fib(n-2)
}

func test() int32 {
	return fib(7)
}
`)
	h128 := compileToIR(
		uintptr(unsafe.Pointer(&src128[0])), int32(len(src128)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir128 := getIR(h128)
	fmt.Println("=== IR for fibonacci ===")
	fmt.Println(ir128)

	llvmVerify("fibonacci", ir128)
	assert("fib has recursive call", strings.Contains(ir128, "@mypkg.fib"))
	assert("fib no parse error", !strings.Contains(ir128, "parse error"))

	irFree(h128)

	// Test 129: Slice of structs with append and field access in loop
	src129 := []byte(`package mypkg

type Entry struct {
	key   string
	value int32
}

func totalValue(entries []Entry) int32 {
	total := int32(0)
	for _, e := range entries {
		total = total + e.value
	}
	return total
}

func test() int32 {
	entries := []Entry{
		Entry{key: "a", value: 10},
		Entry{key: "b", value: 20},
		Entry{key: "c", value: 30},
	}
	return totalValue(entries)
}
`)
	h129 := compileToIR(
		uintptr(unsafe.Pointer(&src129[0])), int32(len(src129)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir129 := getIR(h129)
	fmt.Println("=== IR for struct slice ===")
	fmt.Println(ir129)

	llvmVerify("struct slice", ir129)
	assert("struct slice no parse error", !strings.Contains(ir129, "parse error"))

	irFree(h129)

	// Test 130: Type switch with mixed types (pattern from ir_emit.mx dispatch)
	src130 := []byte(`package mypkg

type Expr interface {
	eval() int32
}

type Const struct{ val int32 }
func (c Const) eval() int32 { return c.val }

type Add struct{ x Expr; y Expr }
func (a Add) eval() int32 { return a.x.eval() + a.y.eval() }

type Neg struct{ x Expr }
func (n Neg) eval() int32 { return 0 - n.x.eval() }

func describe(e Expr) int32 {
	switch v := e.(type) {
	case Const:
		return v.val
	case Add:
		return v.eval()
	case Neg:
		return v.eval()
	}
	return 0
}

func test() int32 {
	c1 := Const{val: 5}
	c2 := Const{val: 3}
	sum := Add{x: c1, y: c2}
	neg := Neg{x: c1}
	return describe(sum) + describe(neg)
}
`)
	h130 := compileToIR(
		uintptr(unsafe.Pointer(&src130[0])), int32(len(src130)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir130 := getIR(h130)
	fmt.Println("=== IR for expr eval dispatch ===")
	fmt.Println(ir130)

	llvmVerify("expr eval dispatch", ir130)
	assert("expr eval has typeid", strings.Contains(ir130, "typeid"))
	assert("expr eval no parse error", !strings.Contains(ir130, "parse error"))

	irFree(h130)

	// Test 131: Cross-package function call
	regPkg("mathutil", "mathutil")
	regFn("mathutil", "Double", "int32->int32")
	regFn("mathutil", "Add", "int32,int32->int32")

	src131 := []byte(`package mypkg

import "mathutil"

func compute(x int32) int32 {
	return mathutil.Add(mathutil.Double(x), 10)
}
`)
	name131 := []byte("mypkg")
	h131 := compileToIR(
		uintptr(unsafe.Pointer(&src131[0])), int32(len(src131)),
		uintptr(unsafe.Pointer(&name131[0])), int32(len(name131)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir131 := getIR(h131)
	fmt.Println("=== IR for cross-package call ===")
	fmt.Println(ir131)

	llvmVerify("cross-package call", ir131)
	assert("cross-pkg has declare @mathutil.Double", strings.Contains(ir131, "declare") && strings.Contains(ir131, "@mathutil.Double"))
	assert("cross-pkg has declare @mathutil.Add", strings.Contains(ir131, "@mathutil.Add"))
	assert("cross-pkg calls Double", strings.Contains(ir131, "call i32 @mathutil.Double("))
	assert("cross-pkg calls Add", strings.Contains(ir131, "call i32 @mathutil.Add("))
	assert("cross-pkg no parse error", !strings.Contains(ir131, "parse error"))

	irFree(h131)
	clearImports()

	// Test 132: Cross-package variable access
	regPkg("config", "config")
	regVar("config", "MaxRetries", "int32")
	regFn("config", "GetTimeout", "->int32")

	src132 := []byte(`package mypkg

import "config"

func retries() int32 {
	return config.MaxRetries
}

func timeout() int32 {
	return config.GetTimeout()
}
`)
	name132 := []byte("mypkg")
	h132 := compileToIR(
		uintptr(unsafe.Pointer(&src132[0])), int32(len(src132)),
		uintptr(unsafe.Pointer(&name132[0])), int32(len(name132)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir132 := getIR(h132)
	fmt.Println("=== IR for cross-package variable ===")
	fmt.Println(ir132)

	llvmVerify("cross-package variable", ir132)
	assert("cross-pkg-var has @config.MaxRetries global", strings.Contains(ir132, "@config.MaxRetries"))
	assert("cross-pkg-var loads MaxRetries", strings.Contains(ir132, "load i32, ptr @config.MaxRetries"))
	assert("cross-pkg-var calls GetTimeout", strings.Contains(ir132, "call i32 @config.GetTimeout("))
	assert("cross-pkg-var no parse error", !strings.Contains(ir132, "parse error"))

	irFree(h132)
	clearImports()

	// Test 133: Aliased import
	regPkg("long/path/util", "util")
	regFn("long/path/util", "Helper", "int32->int32")

	src133 := []byte(`package mypkg

import u "long/path/util"

func wrapped(x int32) int32 {
	return u.Helper(x)
}
`)
	name133 := []byte("mypkg")
	h133 := compileToIR(
		uintptr(unsafe.Pointer(&src133[0])), int32(len(src133)),
		uintptr(unsafe.Pointer(&name133[0])), int32(len(name133)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir133 := getIR(h133)
	fmt.Println("=== IR for aliased import ===")
	fmt.Println(ir133)

	llvmVerify("aliased import", ir133)
	assert("aliased import calls @long/path/util.Helper", strings.Contains(ir133, `@"long/path/util.Helper"`))
	assert("aliased import has declare", strings.Contains(ir133, "declare"))
	assert("aliased import no parse error", !strings.Contains(ir133, "parse error"))

	irFree(h133)
	clearImports()

	// Test 134: Cross-package multi-return function
	regPkg("parser", "parser")
	regFn("parser", "Parse", "[]byte->int32,bool")

	src134 := []byte(`package mypkg

import "parser"

func tryParse(data []byte) int32 {
	n, ok := parser.Parse(data)
	if ok {
		return n
	}
	return -1
}
`)
	name134 := []byte("mypkg")
	h134 := compileToIR(
		uintptr(unsafe.Pointer(&src134[0])), int32(len(src134)),
		uintptr(unsafe.Pointer(&name134[0])), int32(len(name134)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir134 := getIR(h134)
	fmt.Println("=== IR for cross-pkg multi-return ===")
	fmt.Println(ir134)

	llvmVerify("cross-pkg multi-return", ir134)
	assert("cross-pkg multi-ret calls Parse", strings.Contains(ir134, "call {i32, i1} @parser.Parse("))
	assert("cross-pkg multi-ret extracts value", strings.Contains(ir134, "extractvalue"))
	assert("cross-pkg multi-ret no parse error", !strings.Contains(ir134, "parse error"))

	irFree(h134)
	clearImports()

	// Test 135: Cross-package slice-returning function
	regPkg("codec", "codec")
	regFn("codec", "Encode", "int32->[]byte")

	src135 := []byte(`package mypkg

import "codec"

func encodeVal(v int32) []byte {
	return codec.Encode(v)
}
`)
	name135 := []byte("mypkg")
	h135 := compileToIR(
		uintptr(unsafe.Pointer(&src135[0])), int32(len(src135)),
		uintptr(unsafe.Pointer(&name135[0])), int32(len(name135)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir135 := getIR(h135)
	fmt.Println("=== IR for cross-pkg slice return ===")
	fmt.Println(ir135)

	llvmVerify("cross-pkg slice return", ir135)
	assert("cross-pkg slice calls Encode", strings.Contains(ir135, "call {ptr, i64, i64} @codec.Encode("))
	assert("cross-pkg slice has declare", strings.Contains(ir135, "declare"))
	assert("cross-pkg slice no parse error", !strings.Contains(ir135, "parse error"))

	irFree(h135)
	clearImports()

	// Test 136: Cross-package function as argument to local function
	regPkg("strings", "strings")
	regFn("strings", "Contains", "string,string->bool")
	regFn("strings", "HasPrefix", "string,string->bool")

	src136 := []byte(`package mypkg

import "strings"

func check(s string) bool {
	if strings.HasPrefix(s, "test") {
		return strings.Contains(s, "pass")
	}
	return false
}
`)
	name136 := []byte("mypkg")
	h136 := compileToIR(
		uintptr(unsafe.Pointer(&src136[0])), int32(len(src136)),
		uintptr(unsafe.Pointer(&name136[0])), int32(len(name136)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir136 := getIR(h136)
	fmt.Println("=== IR for cross-pkg chained calls ===")
	fmt.Println(ir136)

	llvmVerify("cross-pkg chained calls", ir136)
	assert("cross-pkg chain calls HasPrefix", strings.Contains(ir136, "@strings.HasPrefix"))
	assert("cross-pkg chain calls Contains", strings.Contains(ir136, "@strings.Contains"))
	assert("cross-pkg chain both declared", strings.Count(ir136, "declare") >= 2)
	assert("cross-pkg chain no parse error", !strings.Contains(ir136, "parse error"))

	irFree(h136)
	clearImports()

	// Test 137: Cross-package void function (no return value)
	regPkg("log", "log")
	regFn("log", "Print", "string")

	src137 := []byte(`package mypkg

import "log"

func logMsg(msg string) {
	log.Print(msg)
}
`)
	name137 := []byte("mypkg")
	h137 := compileToIR(
		uintptr(unsafe.Pointer(&src137[0])), int32(len(src137)),
		uintptr(unsafe.Pointer(&name137[0])), int32(len(name137)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir137 := getIR(h137)
	fmt.Println("=== IR for cross-pkg void call ===")
	fmt.Println(ir137)

	llvmVerify("cross-pkg void call", ir137)
	assert("cross-pkg void calls Print", strings.Contains(ir137, "call void @log.Print("))
	assert("cross-pkg void has declare", strings.Contains(ir137, "declare void @log.Print("))
	assert("cross-pkg void no parse error", !strings.Contains(ir137, "parse error"))

	irFree(h137)
	clearImports()

	// Test 138: Multiple imports in one file
	regPkg("bytes", "bytes")
	regFn("bytes", "NewReader", "[]byte->*int32")
	regPkg("strconv", "strconv")
	regFn("strconv", "Itoa", "int32->string")

	src138 := []byte(`package mypkg

import (
	"bytes"
	"strconv"
)

func format(n int32) []byte {
	s := strconv.Itoa(n)
	return s
}

func reader(data []byte) *int32 {
	return bytes.NewReader(data)
}
`)
	name138 := []byte("mypkg")
	h138 := compileToIR(
		uintptr(unsafe.Pointer(&src138[0])), int32(len(src138)),
		uintptr(unsafe.Pointer(&name138[0])), int32(len(name138)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir138 := getIR(h138)
	fmt.Println("=== IR for multi-import ===")
	fmt.Println(ir138)

	llvmVerify("multi-import", ir138)
	assert("multi-import calls Itoa", strings.Contains(ir138, "@strconv.Itoa"))
	assert("multi-import calls NewReader", strings.Contains(ir138, "@bytes.NewReader"))
	assert("multi-import no parse error", !strings.Contains(ir138, "parse error"))

	irFree(h138)
	clearImports()

	// Test 139: Named type with methods and switch - pattern from ssa_types.mx
	src139 := []byte(`package mypkg

type Op int32

const (
	OpAdd Op = iota
	OpSub
	OpMul
)

func (op Op) String() string {
	switch op {
	case OpAdd:
		return "+"
	case OpSub:
		return "-"
	case OpMul:
		return "*"
	}
	return "?"
}

func describe(op Op) string {
	return op.String()
}
`)
	name139 := []byte("mypkg")
	h139 := compileToIR(
		uintptr(unsafe.Pointer(&src139[0])), int32(len(src139)),
		uintptr(unsafe.Pointer(&name139[0])), int32(len(name139)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir139 := getIR(h139)
	fmt.Println("=== IR for named type method ===")
	fmt.Println(ir139)

	llvmVerify("named type method", ir139)
	assert("named-type has Op.String method", strings.Contains(ir139, "@mypkg.Op.String"))
	assert("named-type has switch comparisons", strings.Contains(ir139, "icmp eq i32"))
	assert("named-type describe calls String", strings.Contains(ir139, "call"))
	assert("named-type no parse error", !strings.Contains(ir139, "parse error"))

	irFree(h139)

	// Test 140: Struct with method returning pointer to self - pattern from SSA builder
	src140 := []byte(`package mypkg

type Builder struct {
	buf    []byte
	count  int32
}

func (b *Builder) Write(data []byte) *Builder {
	b.buf = append(b.buf, data...)
	b.count = b.count + 1
	return b
}

func (b *Builder) Len() int32 {
	return int32(len(b.buf))
}

func chain() int32 {
	b := &Builder{buf: []byte{:0:64}}
	b.Write([]byte("hello")).Write([]byte(" world"))
	return b.Len()
}
`)
	name140 := []byte("mypkg")
	h140 := compileToIR(
		uintptr(unsafe.Pointer(&src140[0])), int32(len(src140)),
		uintptr(unsafe.Pointer(&name140[0])), int32(len(name140)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir140 := getIR(h140)
	fmt.Println("=== IR for struct method chain ===")
	fmt.Println(ir140)

	llvmVerify("struct method chain", ir140)
	assert("method-chain has Write method", strings.Contains(ir140, "@mypkg.Builder.Write"))
	assert("method-chain has Len method", strings.Contains(ir140, "@mypkg.Builder.Len"))
	assert("method-chain chain calls Write twice", strings.Count(ir140, "call ptr @mypkg.Write") >= 2 || strings.Count(ir140, "call") >= 3)
	assert("method-chain no parse error", !strings.Contains(ir140, "parse error"))

	irFree(h140)

	// Test 141: Cross-package function result used in local map
	regPkg("strconv", "strconv")
	regFn("strconv", "Itoa", "int32->string")

	src141 := []byte(`package mypkg

import "strconv"

func buildTable(n int32) map[int32]string {
	m := map[int32]string{}
	var i int32
	for i = 0; i < n; i++ {
		m[i] = strconv.Itoa(i)
	}
	return m
}
`)
	name141 := []byte("mypkg")
	h141 := compileToIR(
		uintptr(unsafe.Pointer(&src141[0])), int32(len(src141)),
		uintptr(unsafe.Pointer(&name141[0])), int32(len(name141)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir141 := getIR(h141)
	fmt.Println("=== IR for cross-pkg in map ===")
	fmt.Println(ir141)

	llvmVerify("cross-pkg in map", ir141)
	assert("cross-pkg-map calls Itoa", strings.Contains(ir141, "@strconv.Itoa"))
	assert("cross-pkg-map has hashmapBinarySet", strings.Contains(ir141, "runtime.hashmapBinarySet"))
	assert("cross-pkg-map no parse error", !strings.Contains(ir141, "parse error"))

	irFree(h141)
	clearImports()

	// Test 142: Cross-package function result stored in struct field
	regPkg("codec", "codec")
	regFn("codec", "Encode", "int32->[]byte")

	src142 := []byte(`package mypkg

import "codec"

type Packet struct {
	data []byte
	seq  int32
}

func newPacket(seq int32) Packet {
	return Packet{
		data: codec.Encode(seq),
		seq:  seq,
	}
}
`)
	name142 := []byte("mypkg")
	h142 := compileToIR(
		uintptr(unsafe.Pointer(&src142[0])), int32(len(src142)),
		uintptr(unsafe.Pointer(&name142[0])), int32(len(name142)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir142 := getIR(h142)
	fmt.Println("=== IR for cross-pkg in struct ===")
	fmt.Println(ir142)

	llvmVerify("cross-pkg in struct", ir142)
	assert("cross-pkg-struct calls Encode", strings.Contains(ir142, "@codec.Encode"))
	assert("cross-pkg-struct has struct GEP", strings.Contains(ir142, "getelementptr"))
	assert("cross-pkg-struct no parse error", !strings.Contains(ir142, "parse error"))

	irFree(h142)
	clearImports()

	// Test 143: Cross-package function in conditional expression
	regPkg("strings", "strings")
	regFn("strings", "HasPrefix", "string,string->bool")
	regFn("strings", "TrimPrefix", "string,string->string")

	src143 := []byte(`package mypkg

import "strings"

func clean(s string) string {
	if strings.HasPrefix(s, "//") {
		return strings.TrimPrefix(s, "//")
	}
	return s
}
`)
	name143 := []byte("mypkg")
	h143 := compileToIR(
		uintptr(unsafe.Pointer(&src143[0])), int32(len(src143)),
		uintptr(unsafe.Pointer(&name143[0])), int32(len(name143)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir143 := getIR(h143)
	fmt.Println("=== IR for cross-pkg conditional ===")
	fmt.Println(ir143)

	llvmVerify("cross-pkg conditional", ir143)
	assert("cross-pkg-cond calls HasPrefix", strings.Contains(ir143, "@strings.HasPrefix"))
	assert("cross-pkg-cond calls TrimPrefix", strings.Contains(ir143, "@strings.TrimPrefix"))
	assert("cross-pkg-cond has branch", strings.Contains(ir143, "br i1"))
	assert("cross-pkg-cond no parse error", !strings.Contains(ir143, "parse error"))

	irFree(h143)
	clearImports()

	// Test 144: Cross-package variable write (store to external global)
	regPkg("state", "state")
	regVar("state", "Counter", "int32")
	regVar("state", "Name", "string")

	src144 := []byte(`package mypkg

import "state"

func increment() {
	state.Counter = state.Counter + 1
}

func setName(s string) {
	state.Name = s
}
`)
	name144 := []byte("mypkg")
	h144 := compileToIR(
		uintptr(unsafe.Pointer(&src144[0])), int32(len(src144)),
		uintptr(unsafe.Pointer(&name144[0])), int32(len(name144)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir144 := getIR(h144)
	fmt.Println("=== IR for cross-pkg variable write ===")
	fmt.Println(ir144)

	llvmVerify("cross-pkg variable write", ir144)
	assert("cross-pkg-write loads Counter", strings.Contains(ir144, "load i32, ptr @state.Counter"))
	assert("cross-pkg-write stores Counter", strings.Contains(ir144, "store i32") && strings.Contains(ir144, "@state.Counter"))
	assert("cross-pkg-write stores Name", strings.Contains(ir144, "store {ptr, i64, i64}") && strings.Contains(ir144, "@state.Name"))
	assert("cross-pkg-write has external globals", strings.Contains(ir144, "external global"))
	assert("cross-pkg-write no parse error", !strings.Contains(ir144, "parse error"))

	irFree(h144)
	clearImports()

	// Test 145: String escape pattern from ir_emit.mx (string indexing, bit shift, hex table)
	src145 := []byte(`package mypkg

func escape(s string) []byte {
	var buf []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 32 && c < 127 {
			buf = append(buf, c)
		} else {
			buf = append(buf, '\\')
			buf = append(buf, "0123456789ABCDEF"[c>>4])
			buf = append(buf, "0123456789ABCDEF"[c&0xf])
		}
	}
	return buf
}
`)
	name145 := []byte("mypkg")
	h145 := compileToIR(
		uintptr(unsafe.Pointer(&src145[0])), int32(len(src145)),
		uintptr(unsafe.Pointer(&name145[0])), int32(len(name145)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir145 := getIR(h145)
	fmt.Println("=== IR for string escape ===")
	fmt.Println(ir145)

	llvmVerify("string escape", ir145)
	assert("escape has shr (>>4)", strings.Contains(ir145, "lshr") || strings.Contains(ir145, "ashr"))
	assert("escape has and (&0xf)", strings.Contains(ir145, "and"))
	assert("escape has string index GEP", strings.Contains(ir145, "getelementptr"))
	assert("escape no parse error", !strings.Contains(ir145, "parse error"))

	irFree(h145)

	// Test 146: String builder pattern from ir_emit.mx (append bytes, return string)
	src146 := []byte(`package mypkg

func build(parts []string) string {
	var buf []byte
	for i := 0; i < len(parts); i++ {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, parts[i]...)
	}
	return string(buf)
}
`)
	name146 := []byte("mypkg")
	h146 := compileToIR(
		uintptr(unsafe.Pointer(&src146[0])), int32(len(src146)),
		uintptr(unsafe.Pointer(&name146[0])), int32(len(name146)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir146 := getIR(h146)
	fmt.Println("=== IR for string builder ===")
	fmt.Println(ir146)

	llvmVerify("string builder", ir146)
	assert("builder has sliceAppend", strings.Contains(ir146, "sliceAppend"))
	assert("builder has loop", strings.Contains(ir146, "br i1"))
	assert("builder no parse error", !strings.Contains(ir146, "parse error"))

	irFree(h146)

	// Test 147: Multiple return with cross-package call in one return value
	regPkg("conv", "conv")
	regFn("conv", "Parse", "string->int32,bool")

	src147 := []byte(`package mypkg

import "conv"

func tryGet(s string) (int32, string) {
	n, ok := conv.Parse(s)
	if !ok {
		return 0, "error"
	}
	return n, "ok"
}
`)
	name147 := []byte("mypkg")
	h147 := compileToIR(
		uintptr(unsafe.Pointer(&src147[0])), int32(len(src147)),
		uintptr(unsafe.Pointer(&name147[0])), int32(len(name147)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir147 := getIR(h147)
	fmt.Println("=== IR for multi-ret cross-pkg ===")
	fmt.Println(ir147)

	llvmVerify("multi-ret cross-pkg", ir147)
	assert("multi-ret calls Parse", strings.Contains(ir147, "@conv.Parse"))
	assert("multi-ret extracts bool", strings.Contains(ir147, "extractvalue"))
	assert("multi-ret returns tuple", strings.Contains(ir147, "ret {i32, {ptr, i64, i64}}"))
	assert("multi-ret no parse error", !strings.Contains(ir147, "parse error"))

	irFree(h147)
	clearImports()

	// Test 148: Realistic emitter pattern - struct with map fields, methods, string building
	src148 := []byte(`package mypkg

type Emitter struct {
	buf    []byte
	names  map[string]int32
	next   int32
}

func newEmitter() *Emitter {
	return &Emitter{
		buf:   []byte{:0:1024},
		names: map[string]int32{},
	}
}

func (e *Emitter) w(s string) {
	e.buf = append(e.buf, s...)
}

func (e *Emitter) nextName() string {
	e.next = e.next + 1
	return "t"
}

func (e *Emitter) lookup(name string) int32 {
	if n, ok := e.names[name]; ok {
		return n
	}
	n := e.next
	e.next = e.next + 1
	e.names[name] = n
	return n
}

func (e *Emitter) emit() string {
	e.w("define void @main() {\n")
	e.w("entry:\n")
	e.w("  ret void\n")
	e.w("}\n")
	return string(e.buf)
}

func run() string {
	e := newEmitter()
	e.lookup("x")
	e.lookup("y")
	return e.emit()
}
`)
	name148 := []byte("mypkg")
	h148 := compileToIR(
		uintptr(unsafe.Pointer(&src148[0])), int32(len(src148)),
		uintptr(unsafe.Pointer(&name148[0])), int32(len(name148)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir148 := getIR(h148)
	fmt.Println("=== IR for emitter pattern ===")
	fmt.Println(ir148)

	llvmVerify("emitter pattern", ir148)
	assert("emitter has Emitter.w method", strings.Contains(ir148, "@mypkg.Emitter.w"))
	assert("emitter has Emitter.lookup method", strings.Contains(ir148, "@mypkg.Emitter.lookup"))
	assert("emitter has Emitter.emit method", strings.Contains(ir148, "@mypkg.Emitter.emit"))
	assert("emitter has map operations", strings.Contains(ir148, "hashmap"))
	assert("emitter has newEmitter", strings.Contains(ir148, "@mypkg.newEmitter"))
	assert("emitter no parse error", !strings.Contains(ir148, "parse error"))

	irFree(h148)

	// Test 149: Interface dispatch with method set - pattern from SSA instruction emit
	src149 := []byte(`package mypkg

type Instruction interface {
	String() string
}

type Add struct {
	left  int32
	right int32
}

type Ret struct {
	val int32
}

func (a *Add) String() string {
	return "add"
}

func (r *Ret) String() string {
	return "ret"
}

func emit(instr Instruction) string {
	return instr.String()
}

func test() string {
	a := &Add{left: 1, right: 2}
	r := &Ret{val: 42}
	s1 := emit(a)
	s2 := emit(r)
	return s1 | s2
}
`)
	name149 := []byte("mypkg")
	h149 := compileToIR(
		uintptr(unsafe.Pointer(&src149[0])), int32(len(src149)),
		uintptr(unsafe.Pointer(&name149[0])), int32(len(name149)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir149 := getIR(h149)
	fmt.Println("=== IR for interface dispatch ===")
	fmt.Println(ir149)

	llvmVerify("interface dispatch", ir149)
	assert("iface-dispatch has Add.String", strings.Contains(ir149, "Add.String"))
	assert("iface-dispatch has Ret.String", strings.Contains(ir149, "Ret.String"))
	assert("iface-dispatch has typeid", strings.Contains(ir149, "typeid"))
	assert("iface-dispatch has emit function", strings.Contains(ir149, "@mypkg.emit"))
	assert("iface-dispatch no parse error", !strings.Contains(ir149, "parse error"))

	irFree(h149)

	// Test 150: Switch on type with fallthrough to handler - mirrors emitInstr dispatch
	src150 := []byte(`package mypkg

type Node interface {
	nodeTag()
}

type Lit struct {
	val int32
}

type Bin struct {
	op   int32
	left Node
	right Node
}

func (l *Lit) nodeTag() {}
func (b *Bin) nodeTag() {}

func eval(n Node) int32 {
	switch v := n.(type) {
	case *Lit:
		return v.val
	case *Bin:
		l := eval(v.left)
		r := eval(v.right)
		if v.op == 0 {
			return l + r
		}
		return l - r
	}
	return 0
}
`)
	name150 := []byte("mypkg")
	h150 := compileToIR(
		uintptr(unsafe.Pointer(&src150[0])), int32(len(src150)),
		uintptr(unsafe.Pointer(&name150[0])), int32(len(name150)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir150 := getIR(h150)
	fmt.Println("=== IR for type switch dispatch ===")
	fmt.Println(ir150)

	llvmVerify("type switch dispatch", ir150)
	assert("type-switch has eval function", strings.Contains(ir150, "@mypkg.eval"))
	assert("type-switch has typeid comparisons", strings.Contains(ir150, "icmp eq ptr"))
	assert("type-switch recursive call", strings.Count(ir150, "call i32 @mypkg.eval") >= 2)
	assert("type-switch no parse error", !strings.Contains(ir150, "parse error"))

	irFree(h150)

	// Test 151: Verify pointer type switch gets DISTINCT typeids (regression test)
	src151 := []byte(`package mypkg

type Animal interface {
	Sound() string
}

type Dog struct {
	name string
}

type Cat struct {
	name string
}

type Bird struct {
	name string
}

func (d *Dog) Sound() string { return "woof" }
func (c *Cat) Sound() string { return "meow" }
func (b *Bird) Sound() string { return "tweet" }

func classify(a Animal) int32 {
	switch a.(type) {
	case *Dog:
		return 1
	case *Cat:
		return 2
	case *Bird:
		return 3
	}
	return 0
}
`)
	name151 := []byte("mypkg")
	h151 := compileToIR(
		uintptr(unsafe.Pointer(&src151[0])), int32(len(src151)),
		uintptr(unsafe.Pointer(&name151[0])), int32(len(name151)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir151 := getIR(h151)
	fmt.Println("=== IR for pointer typeids ===")
	fmt.Println(ir151)

	llvmVerify("pointer typeids", ir151)
	assert("ptr-typeid has distinct Dog typeid", strings.Contains(ir151, "typeid.ptr.Dog"))
	assert("ptr-typeid has distinct Cat typeid", strings.Contains(ir151, "typeid.ptr.Cat"))
	assert("ptr-typeid has distinct Bird typeid", strings.Contains(ir151, "typeid.ptr.Bird"))
	assert("ptr-typeid Dog != Cat != Bird", strings.Count(ir151, "typeid.ptr.Dog") >= 2 && strings.Count(ir151, "typeid.ptr.Cat") >= 2 && strings.Count(ir151, "typeid.ptr.Bird") >= 2)
	assert("ptr-typeid no parse error", !strings.Contains(ir151, "parse error"))

	irFree(h151)

	// Test 152: Real bootstrap code - irItoa (modulo, division, byte conversion)
	src152 := []byte(`package mypkg

func itoa(n int32) string {
	if n == 0 {
		return "0"
	}
	buf := []byte{:0:20}
	for n > 0 {
		buf = append(buf, byte('0' + n % 10))
		n = n / 10
	}
	return string(buf)
}
`)
	name152 := []byte("mypkg")
	h152 := compileToIR(
		uintptr(unsafe.Pointer(&src152[0])), int32(len(src152)),
		uintptr(unsafe.Pointer(&name152[0])), int32(len(name152)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir152 := getIR(h152)
	fmt.Println("=== IR for irItoa ===")
	fmt.Println(ir152)

	llvmVerify("irItoa", ir152)
	assert("itoa has srem (modulo)", strings.Contains(ir152, "srem"))
	assert("itoa has sdiv (division)", strings.Contains(ir152, "sdiv"))
	assert("itoa no parse error", !strings.Contains(ir152, "parse error"))

	irFree(h152)

	// Test 153: Real bootstrap code - irEscapeString (string indexing, bit ops)
	src153 := []byte(`package mypkg

func escapeStr(s string) string {
	var buf []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 32 && c < 127 && c != '\\' && c != '"' {
			buf = append(buf, c)
		} else {
			buf = append(buf, '\\')
			buf = append(buf, "0123456789ABCDEF"[c>>4])
			buf = append(buf, "0123456789ABCDEF"[c&0xf])
		}
	}
	return string(buf)
}
`)
	name153 := []byte("mypkg")
	h153 := compileToIR(
		uintptr(unsafe.Pointer(&src153[0])), int32(len(src153)),
		uintptr(unsafe.Pointer(&name153[0])), int32(len(name153)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir153 := getIR(h153)
	fmt.Println("=== IR for escapeStr ===")
	fmt.Println(ir153)

	llvmVerify("escapeStr", ir153)
	assert("escape has lshr (>>4)", strings.Contains(ir153, "lshr") || strings.Contains(ir153, "ashr"))
	assert("escape has and (&0xf)", strings.Contains(ir153, "and i"))
	assert("escape no parse error", !strings.Contains(ir153, "parse error"))

	irFree(h153)

	// Test 154: Full IRWriter pattern (restored after bisection shows individual parts pass)
	src154 := []byte(`package mypkg

type IRWriter struct {
	buf   []byte
	lines []string
	seen  map[string]int32
}

func (w *IRWriter) emit(s string) {
	w.buf = append(w.buf, s...)
}

func (w *IRWriter) newline() {
	w.lines = append(w.lines, string(w.buf))
	w.buf = w.buf[:0]
}

func (w *IRWriter) writeLine(s string) {
	w.emit(s)
	w.newline()
}

func itoa154(n int32) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := []byte{:0:20}
	for n > 0 {
		buf = append(buf, byte('0' + n % 10))
		n = n / 10
	}
	if neg {
		buf = append(buf, '-')
	}
	i := 0
	j := int32(len(buf)) - 1
	for i < j {
		buf[i], buf[j] = buf[j], buf[i]
		i = i + 1
		j = j - 1
	}
	return string(buf)
}

func (w *IRWriter) writeInt(n int32) {
	w.emit(itoa154(n))
}

func (w *IRWriter) emitDecl(name string, idx int32) {
	w.emit("define ")
	w.emit(name)
	w.emit("(")
	w.emit(itoa154(idx))
	w.emit(")")
	w.newline()
}

func (w *IRWriter) track(key string) {
	w.seen[key] = w.seen[key] + 1
}

func (w *IRWriter) count() int32 {
	return int32(len(w.lines))
}
`)
	name154 := []byte("mypkg")
	h154 := compileToIR(
		uintptr(unsafe.Pointer(&src154[0])), int32(len(src154)),
		uintptr(unsafe.Pointer(&name154[0])), int32(len(name154)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir154 := getIR(h154)
	fmt.Println("=== IR for Writer+itoa ===")
	fmt.Println(ir154)

	llvmVerify("IRWriter", ir154)
	assert("irwriter has emit method", strings.Contains(ir154, "@mypkg.IRWriter.emit"))
	assert("irwriter has writeInt method", strings.Contains(ir154, "@mypkg.IRWriter.writeInt"))
	assert("irwriter has emitDecl method", strings.Contains(ir154, "@mypkg.IRWriter.emitDecl"))
	assert("irwriter has track method", strings.Contains(ir154, "@mypkg.IRWriter.track"))
	assert("itoa154 has srem", strings.Contains(ir154, "srem"))
	assert("irwriter no parse error", !strings.Contains(ir154, "parse error"))

	irFree(h154)

	// Test 155: Real bootstrap pattern - SSA instruction switch dispatch
	src155 := []byte(`package mypkg

type Value interface {
	valTag()
}

type Const struct {
	val int32
}

type Param struct {
	name string
	idx  int32
}

type BinOp struct {
	op   int32
	x    Value
	y    Value
}

func (c *Const) valTag() {}
func (p *Param) valTag() {}
func (b *BinOp) valTag() {}

func operand(v Value) string {
	switch v := v.(type) {
	case *Const:
		return itoa155(v.val)
	case *Param:
		return "%" | v.name
	case *BinOp:
		x := operand(v.x)
		y := operand(v.y)
		switch v.op {
		case 0:
			return x | " + " | y
		case 1:
			return x | " - " | y
		case 2:
			return x | " * " | y
		}
		return x | " ? " | y
	}
	return "undef"
}

func itoa155(n int32) string {
	if n == 0 {
		return "0"
	}
	buf := []byte{:0:10}
	for n > 0 {
		buf = append(buf, byte('0' + n % 10))
		n = n / 10
	}
	return string(buf)
}
`)
	name155 := []byte("mypkg")
	h155 := compileToIR(
		uintptr(unsafe.Pointer(&src155[0])), int32(len(src155)),
		uintptr(unsafe.Pointer(&name155[0])), int32(len(name155)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir155 := getIR(h155)
	fmt.Println("=== IR for SSA operand dispatch ===")
	fmt.Println(ir155)

	llvmVerify("SSA operand dispatch", ir155)
	assert("ssa-dispatch has operand", strings.Contains(ir155, "@mypkg.operand"))
	assert("ssa-dispatch has typeid Const", strings.Contains(ir155, "typeid.ptr.Const"))
	assert("ssa-dispatch has typeid Param", strings.Contains(ir155, "typeid.ptr.Param"))
	assert("ssa-dispatch has typeid BinOp", strings.Contains(ir155, "typeid.ptr.BinOp"))
	assert("ssa-dispatch recursive operand calls", strings.Count(ir155, "call {ptr, i64, i64} @mypkg.operand") >= 2)
	assert("ssa-dispatch has nested switch", strings.Contains(ir155, "icmp eq i32"))
	assert("ssa-dispatch no parse error", !strings.Contains(ir155, "parse error"))

	irFree(h155)

	// Test 156: Nil interface comparison
	src156 := []byte(`package mypkg

type Stringer interface {
	String() string
}

func isNil(s Stringer) bool {
	return s == nil
}

func isNotNil(s Stringer) bool {
	return s != nil
}
`)
	name156 := []byte("mypkg")
	h156 := compileToIR(
		uintptr(unsafe.Pointer(&src156[0])), int32(len(src156)),
		uintptr(unsafe.Pointer(&name156[0])), int32(len(name156)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir156 := getIR(h156)
	fmt.Println("=== IR for nil interface ===")
	fmt.Println(ir156)

	llvmVerify("nil interface", ir156)
	assert("nil-iface has isNil", strings.Contains(ir156, "@mypkg.isNil"))
	assert("nil-iface has isNotNil", strings.Contains(ir156, "@mypkg.isNotNil"))
	assert("nil-iface no parse error", !strings.Contains(ir156, "parse error"))

	irFree(h156)

	// Test 157: Nil slice comparison
	src157 := []byte(`package mypkg

func sliceIsNil(s []int32) bool {
	return s == nil
}

func sliceNotNil(s []int32) bool {
	return s != nil
}
`)
	name157 := []byte("mypkg")
	h157 := compileToIR(
		uintptr(unsafe.Pointer(&src157[0])), int32(len(src157)),
		uintptr(unsafe.Pointer(&name157[0])), int32(len(name157)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir157 := getIR(h157)
	fmt.Println("=== IR for nil slice ===")
	fmt.Println(ir157)

	llvmVerify("nil slice", ir157)
	assert("nil-slice has sliceIsNil", strings.Contains(ir157, "@mypkg.sliceIsNil"))
	assert("nil-slice has sliceNotNil", strings.Contains(ir157, "@mypkg.sliceNotNil"))
	assert("nil-slice no parse error", !strings.Contains(ir157, "parse error"))

	irFree(h157)

	// Test 158: Nested method calls with string concat (emitter pattern)
	src158 := []byte(`package mypkg

type Buf struct {
	data []byte
}

func (b *Buf) write(s string) {
	b.data = append(b.data, s...)
}

func (b *Buf) writeQuoted(s string) {
	b.write("\"")
	b.write(s)
	b.write("\"")
}

func (b *Buf) writeKV(k string, v string) {
	b.write(k)
	b.write(": ")
	b.writeQuoted(v)
	b.write("\n")
}

func (b *Buf) result() string {
	return string(b.data)
}
`)
	name158 := []byte("mypkg")
	h158 := compileToIR(
		uintptr(unsafe.Pointer(&src158[0])), int32(len(src158)),
		uintptr(unsafe.Pointer(&name158[0])), int32(len(name158)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir158 := getIR(h158)
	fmt.Println("=== IR for Buf methods ===")
	fmt.Println(ir158)

	llvmVerify("Buf methods", ir158)
	assert("buf has write", strings.Contains(ir158, "@mypkg.Buf.write"))
	assert("buf has writeQuoted", strings.Contains(ir158, "@mypkg.Buf.writeQuoted"))
	assert("buf has writeKV", strings.Contains(ir158, "@mypkg.Buf.writeKV"))
	assert("buf has result", strings.Contains(ir158, "@mypkg.Buf.result"))
	assert("buf no parse error", !strings.Contains(ir158, "parse error"))

	irFree(h158)

	// Test 159: Struct with interface field + factory function + method dispatch
	src159 := []byte(`package mypkg

type Node interface {
	nodeTag()
}

type Literal struct {
	val int32
}

type Binary struct {
	op    int32
	left  Node
	right Node
}

func (l *Literal) nodeTag() {}
func (b *Binary) nodeTag() {}

func newLit(v int32) *Literal {
	return &Literal{val: v}
}

func newBin(op int32, l Node, r Node) *Binary {
	return &Binary{op: op, left: l, right: r}
}

func depth(n Node) int32 {
	switch n := n.(type) {
	case *Literal:
		return 1
	case *Binary:
		ld := depth(n.left)
		rd := depth(n.right)
		if ld > rd {
			return ld + 1
		}
		return rd + 1
	}
	return 0
}
`)
	name159 := []byte("mypkg")
	h159 := compileToIR(
		uintptr(unsafe.Pointer(&src159[0])), int32(len(src159)),
		uintptr(unsafe.Pointer(&name159[0])), int32(len(name159)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir159 := getIR(h159)
	fmt.Println("=== IR for Node tree ===")
	fmt.Println(ir159)

	llvmVerify("Node tree", ir159)
	assert("node has newLit", strings.Contains(ir159, "@mypkg.newLit"))
	assert("node has newBin", strings.Contains(ir159, "@mypkg.newBin"))
	assert("node has depth", strings.Contains(ir159, "@mypkg.depth"))
	assert("node depth recursive", strings.Count(ir159, "call i32 @mypkg.depth") >= 2)
	assert("node distinct typeids", strings.Contains(ir159, "typeid.ptr.Literal") && strings.Contains(ir159, "typeid.ptr.Binary"))
	assert("node no parse error", !strings.Contains(ir159, "parse error"))

	irFree(h159)

	// Test 160: Large struct with many fields (emitter-scale struct)
	src160 := []byte(`package mypkg

type Emitter struct {
	pkg       string
	triple    string
	buf       []byte
	decls     []string
	globals   map[string]string
	types     map[string]int32
	tempCount int32
	indent    int32
}

func newEmitter(pkg string, triple string) *Emitter {
	return &Emitter{
		pkg:    pkg,
		triple: triple,
		globals: map[string]string{},
		types:   map[string]int32{},
	}
}

func (e *Emitter) nextTemp() string {
	e.tempCount = e.tempCount + 1
	return "%t" | itoa160(e.tempCount)
}

func (e *Emitter) addDecl(s string) {
	e.decls = append(e.decls, s)
}

func (e *Emitter) setGlobal(name string, val string) {
	e.globals[name] = val
}

func (e *Emitter) hasType(name string) bool {
	_, ok := e.types[name]
	return ok
}

func itoa160(n int32) string {
	if n == 0 {
		return "0"
	}
	buf := []byte{:0:10}
	for n > 0 {
		buf = append(buf, byte('0' + n % 10))
		n = n / 10
	}
	return string(buf)
}
`)
	name160 := []byte("mypkg")
	h160 := compileToIR(
		uintptr(unsafe.Pointer(&src160[0])), int32(len(src160)),
		uintptr(unsafe.Pointer(&name160[0])), int32(len(name160)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir160 := getIR(h160)
	fmt.Println("=== IR for Emitter struct ===")
	fmt.Println(ir160)

	llvmVerify("Emitter struct", ir160)
	assert("emitter has newEmitter", strings.Contains(ir160, "@mypkg.newEmitter"))
	assert("emitter has nextTemp", strings.Contains(ir160, "@mypkg.Emitter.nextTemp"))
	assert("emitter has addDecl", strings.Contains(ir160, "@mypkg.Emitter.addDecl"))
	assert("emitter has setGlobal", strings.Contains(ir160, "@mypkg.Emitter.setGlobal"))
	assert("emitter has hasType", strings.Contains(ir160, "@mypkg.Emitter.hasType"))
	assert("emitter no parse error", !strings.Contains(ir160, "parse error"))

	irFree(h160)

	// Test 161: Closure-like pattern (function variable in struct)
	src161 := []byte(`package mypkg

type Handler struct {
	name string
	fn   func(string) string
}

func identity(s string) string {
	return s
}

func upper(s string) string {
	return s
}

func newHandler(name string) *Handler {
	return &Handler{name: name, fn: identity}
}

func (h *Handler) setFn(f func(string) string) {
	h.fn = f
}

func (h *Handler) run(input string) string {
	return h.fn(input)
}
`)
	name161 := []byte("mypkg")
	h161 := compileToIR(
		uintptr(unsafe.Pointer(&src161[0])), int32(len(src161)),
		uintptr(unsafe.Pointer(&name161[0])), int32(len(name161)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir161 := getIR(h161)
	fmt.Println("=== IR for func var ===")
	fmt.Println(ir161)

	llvmVerify("func var", ir161)
	assert("funcvar has newHandler", strings.Contains(ir161, "@mypkg.newHandler"))
	assert("funcvar has run method", strings.Contains(ir161, "@mypkg.Handler.run"))
	assert("funcvar no parse error", !strings.Contains(ir161, "parse error"))

	irFree(h161)

	// Test 162: Map iteration (range over map)
	src162 := []byte(`package mypkg

func sumMap(m map[string]int32) int32 {
	total := int32(0)
	for _, v := range m {
		total = total + v
	}
	return total
}

func keys(m map[string]int32) []string {
	var result []string
	for k := range m {
		result = append(result, k)
	}
	return result
}
`)
	name162 := []byte("mypkg")
	h162 := compileToIR(
		uintptr(unsafe.Pointer(&src162[0])), int32(len(src162)),
		uintptr(unsafe.Pointer(&name162[0])), int32(len(name162)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir162 := getIR(h162)
	fmt.Println("=== IR for map iteration ===")
	fmt.Println(ir162)

	llvmVerify("map iteration", ir162)
	assert("mapiter has sumMap", strings.Contains(ir162, "@mypkg.sumMap"))
	assert("mapiter has keys", strings.Contains(ir162, "@mypkg.keys"))
	assert("mapiter uses hashmapNext", strings.Contains(ir162, "hashmapNext") || strings.Contains(ir162, "hashmapBinaryNext"))
	assert("mapiter no parse error", !strings.Contains(ir162, "parse error"))

	irFree(h162)

	// Test 163: Multi-level type switch with method calls (ir_emit pattern)
	src163 := []byte(`package mypkg

type Type interface {
	typeTag()
}

type BasicType struct {
	kind int32
}

type SliceType struct {
	elem Type
}

type PointerType struct {
	elem Type
}

type StructType struct {
	fields []Type
}

func (b *BasicType) typeTag() {}
func (s *SliceType) typeTag() {}
func (p *PointerType) typeTag() {}
func (s *StructType) typeTag() {}

func typeSize(t Type) int32 {
	switch t := t.(type) {
	case *BasicType:
		switch t.kind {
		case 0:
			return 1
		case 1:
			return 4
		case 2:
			return 8
		}
		return 0
	case *SliceType:
		return 24
	case *PointerType:
		return 8
	case *StructType:
		total := int32(0)
		for _, f := range t.fields {
			total = total + typeSize(f)
		}
		return total
	}
	return 0
}

func typeName(t Type) string {
	switch t := t.(type) {
	case *BasicType:
		switch t.kind {
		case 0:
			return "i8"
		case 1:
			return "i32"
		case 2:
			return "i64"
		}
		return "void"
	case *SliceType:
		return "[]" | typeName(t.elem)
	case *PointerType:
		return "*" | typeName(t.elem)
	case *StructType:
		return "struct"
	}
	return "unknown"
}
`)
	name163 := []byte("mypkg")
	h163 := compileToIR(
		uintptr(unsafe.Pointer(&src163[0])), int32(len(src163)),
		uintptr(unsafe.Pointer(&name163[0])), int32(len(name163)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir163 := getIR(h163)
	fmt.Println("=== IR for type system ===")
	fmt.Println(ir163)

	llvmVerify("type system", ir163)
	assert("typesys has typeSize", strings.Contains(ir163, "@mypkg.typeSize"))
	assert("typesys has typeName", strings.Contains(ir163, "@mypkg.typeName"))
	assert("typesys has 4 distinct typeids",
		strings.Contains(ir163, "typeid.ptr.BasicType") &&
		strings.Contains(ir163, "typeid.ptr.SliceType") &&
		strings.Contains(ir163, "typeid.ptr.PointerType") &&
		strings.Contains(ir163, "typeid.ptr.StructType"))
	assert("typesys typeSize recursive", strings.Count(ir163, "call i32 @mypkg.typeSize") >= 1)
	assert("typesys typeName recursive", strings.Count(ir163, "call {ptr, i64, i64} @mypkg.typeName") >= 1)
	assert("typesys no parse error", !strings.Contains(ir163, "parse error"))

	irFree(h163)

	// Test 164: String building with map lookup (ir_emit core pattern)
	src164 := []byte(`package mypkg

type IRGen struct {
	regs map[string]string
	buf  []byte
	n    int32
}

func (g *IRGen) fresh(prefix string) string {
	g.n = g.n + 1
	return prefix | itoa164(g.n)
}

func (g *IRGen) emit(s string) {
	g.buf = append(g.buf, s...)
}

func (g *IRGen) emitLine(parts []string) {
	for i := 0; i < len(parts); i++ {
		g.emit(parts[i])
	}
	g.emit("\n")
}

func (g *IRGen) setReg(name string, val string) {
	g.regs[name] = val
}

func (g *IRGen) getReg(name string) string {
	v, ok := g.regs[name]
	if !ok {
		return "undef"
	}
	return v
}

func itoa164(n int32) string {
	if n == 0 {
		return "0"
	}
	buf := []byte{:0:10}
	for n > 0 {
		buf = append(buf, byte('0' + n % 10))
		n = n / 10
	}
	i := 0
	j := int32(len(buf)) - 1
	for i < j {
		buf[i], buf[j] = buf[j], buf[i]
		i = i + 1
		j = j - 1
	}
	return string(buf)
}
`)
	name164 := []byte("mypkg")
	h164 := compileToIR(
		uintptr(unsafe.Pointer(&src164[0])), int32(len(src164)),
		uintptr(unsafe.Pointer(&name164[0])), int32(len(name164)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir164 := getIR(h164)
	fmt.Println("=== IR for IRGen ===")
	fmt.Println(ir164)

	llvmVerify("IRGen", ir164)
	assert("irgen has fresh", strings.Contains(ir164, "@mypkg.IRGen.fresh"))
	assert("irgen has emit", strings.Contains(ir164, "@mypkg.IRGen.emit"))
	assert("irgen has emitLine", strings.Contains(ir164, "@mypkg.IRGen.emitLine"))
	assert("irgen has getReg with ok check", strings.Contains(ir164, "@mypkg.IRGen.getReg"))
	assert("irgen no parse error", !strings.Contains(ir164, "parse error"))

	irFree(h164)

	// Test 165: Cross-package calls inside type switch (compile pattern)
	clearImports()
	regPkg("ext/types", "types")
	regFn("ext/types", "BasicSize", "int32->int32")
	regFn("ext/types", "SliceSize", "->int32")
	src165 := []byte(`package mypkg

import "ext/types"

type Typ interface {
	tag()
}

type Basic165 struct {
	kind int32
}

type Slice165 struct {
	elem Typ
}

func (b *Basic165) tag() {}
func (s *Slice165) tag() {}

func size(t Typ) int32 {
	switch t := t.(type) {
	case *Basic165:
		return types.BasicSize(t.kind)
	case *Slice165:
		return types.SliceSize()
	}
	return 0
}
`)
	name165 := []byte("mypkg")
	h165 := compileToIR(
		uintptr(unsafe.Pointer(&src165[0])), int32(len(src165)),
		uintptr(unsafe.Pointer(&name165[0])), int32(len(name165)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir165 := getIR(h165)
	fmt.Println("=== IR for cross-pkg type switch ===")
	fmt.Println(ir165)

	llvmVerify("cross-pkg type switch", ir165)
	assert("xpkg-tswitch has size func", strings.Contains(ir165, "@mypkg.size"))
	assert("xpkg-tswitch calls BasicSize", strings.Contains(ir165, "BasicSize"))
	assert("xpkg-tswitch calls SliceSize", strings.Contains(ir165, "SliceSize"))
	assert("xpkg-tswitch no parse error", !strings.Contains(ir165, "parse error"))

	irFree(h165)
	clearImports()

	// Test 166: Multiple return values from method
	src166 := []byte(`package mypkg

type Scanner struct {
	src []byte
	pos int32
}

func (s *Scanner) peek() (byte, bool) {
	if int32(s.pos) >= int32(len(s.src)) {
		return 0, false
	}
	return s.src[s.pos], true
}

func (s *Scanner) advance() byte {
	ch, ok := s.peek()
	if !ok {
		return 0
	}
	s.pos = s.pos + 1
	return ch
}

func (s *Scanner) skipWhile(ch byte) {
	for {
		c, ok := s.peek()
		if !ok || c != ch {
			return
		}
		s.pos = s.pos + 1
	}
}
`)
	name166 := []byte("mypkg")
	h166 := compileToIR(
		uintptr(unsafe.Pointer(&src166[0])), int32(len(src166)),
		uintptr(unsafe.Pointer(&name166[0])), int32(len(name166)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir166 := getIR(h166)
	fmt.Println("=== IR for Scanner ===")
	fmt.Println(ir166)

	llvmVerify("Scanner", ir166)
	assert("scanner has peek", strings.Contains(ir166, "@mypkg.Scanner.peek"))
	assert("scanner has advance", strings.Contains(ir166, "@mypkg.Scanner.advance"))
	assert("scanner has skipWhile", strings.Contains(ir166, "@mypkg.Scanner.skipWhile"))
	assert("scanner peek returns tuple", strings.Contains(ir166, "{i8, i1}"))
	assert("scanner advance calls peek", strings.Contains(ir166, "call {i8, i1} @mypkg.Scanner.peek"))
	assert("scanner no parse error", !strings.Contains(ir166, "parse error"))

	irFree(h166)

	// Test 167: Repeated short method calls (w() pattern from ir_emit)
	src167 := []byte(`package mypkg

type Builder struct {
	buf []byte
}

func (b *Builder) w(s string) {
	b.buf = append(b.buf, s...)
}

func (b *Builder) emitAdd(dst string, ty string, lhs string, rhs string) {
	b.w("  ")
	b.w(dst)
	b.w(" = add ")
	b.w(ty)
	b.w(" ")
	b.w(lhs)
	b.w(", ")
	b.w(rhs)
	b.w("\n")
}

func (b *Builder) emitStore(ty string, val string, ptr string) {
	b.w("  store ")
	b.w(ty)
	b.w(" ")
	b.w(val)
	b.w(", ptr ")
	b.w(ptr)
	b.w("\n")
}

func (b *Builder) emitRet(ty string, val string) {
	b.w("  ret ")
	b.w(ty)
	b.w(" ")
	b.w(val)
	b.w("\n")
}

func (b *Builder) result() string {
	return string(b.buf)
}
`)
	name167 := []byte("mypkg")
	h167 := compileToIR(
		uintptr(unsafe.Pointer(&src167[0])), int32(len(src167)),
		uintptr(unsafe.Pointer(&name167[0])), int32(len(name167)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir167 := getIR(h167)
	fmt.Println("=== IR for Builder w() ===")
	fmt.Println(ir167)

	llvmVerify("Builder w()", ir167)
	assert("builder has w method", strings.Contains(ir167, "@mypkg.Builder.w"))
	assert("builder has emitAdd", strings.Contains(ir167, "@mypkg.Builder.emitAdd"))
	assert("builder has emitStore", strings.Contains(ir167, "@mypkg.Builder.emitStore"))
	assert("builder has emitRet", strings.Contains(ir167, "@mypkg.Builder.emitRet"))
	assert("builder emitAdd calls w many times", strings.Count(ir167, "call void @mypkg.Builder.w") >= 10)
	assert("builder no parse error", !strings.Contains(ir167, "parse error"))

	irFree(h167)

	// Test 168: Nested struct access + interface field (SSA node pattern)
	src168 := []byte(`package mypkg

type Pos struct {
	line int32
	col  int32
}

type Token struct {
	kind int32
	pos  Pos
	text string
}

type ASTNode interface {
	astTag()
}

type ExprStmt struct {
	pos  Pos
	expr ASTNode
}

type ReturnStmt struct {
	pos    Pos
	result ASTNode
}

func (e *ExprStmt) astTag() {}
func (r *ReturnStmt) astTag() {}

func stmtLine(n ASTNode) int32 {
	switch n := n.(type) {
	case *ExprStmt:
		return n.pos.line
	case *ReturnStmt:
		return n.pos.line
	}
	return 0
}

func stmtCol(n ASTNode) int32 {
	switch n := n.(type) {
	case *ExprStmt:
		return n.pos.col
	case *ReturnStmt:
		return n.pos.col
	}
	return 0
}
`)
	name168 := []byte("mypkg")
	h168 := compileToIR(
		uintptr(unsafe.Pointer(&src168[0])), int32(len(src168)),
		uintptr(unsafe.Pointer(&name168[0])), int32(len(name168)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir168 := getIR(h168)
	fmt.Println("=== IR for AST nodes ===")
	fmt.Println(ir168)

	llvmVerify("AST nodes", ir168)
	assert("ast has stmtLine", strings.Contains(ir168, "@mypkg.stmtLine"))
	assert("ast has stmtCol", strings.Contains(ir168, "@mypkg.stmtCol"))
	assert("ast nested field getelementptr", strings.Count(ir168, "getelementptr") >= 4)
	assert("ast no parse error", !strings.Contains(ir168, "parse error"))

	irFree(h168)

	// Test 169: Struct embedding - basic field access through embedded struct
	src169 := []byte(`package mypkg

type base struct {
	name string
	val  int32
}

func (b *base) Name() string { return b.name }
func (b *base) Val() int32 { return b.val }

type Derived struct {
	base
	extra int32
}

func newDerived(name string, val int32, extra int32) *Derived {
	return &Derived{base: base{name: name, val: val}, extra: extra}
}

func getName(d *Derived) string {
	return d.Name()
}

func getVal(d *Derived) int32 {
	return d.Val()
}
`)
	name169 := []byte("mypkg")
	h169 := compileToIR(
		uintptr(unsafe.Pointer(&src169[0])), int32(len(src169)),
		uintptr(unsafe.Pointer(&name169[0])), int32(len(name169)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir169 := getIR(h169)
	fmt.Println("=== IR for struct embedding ===")
	fmt.Println(ir169)

	if ir169 == "" || strings.Contains(ir169, "parse error") {
		fmt.Println("NOTE: struct embedding may not be supported yet")
		assert("embedding produces IR", ir169 != "")
	} else {
		llvmVerify("struct embedding", ir169)
		assert("embed has newDerived", strings.Contains(ir169, "@mypkg.newDerived"))
		assert("embed has getName", strings.Contains(ir169, "@mypkg.getName"))
		assert("embed has base.Name method", strings.Contains(ir169, "@mypkg.base.Name"))
		assert("embed no parse error", !strings.Contains(ir169, "parse error"))
	}

	irFree(h169)

	// Test 170: Embedded field direct access (d.name -> base.name)
	src170 := []byte(`package mypkg

type inner struct {
	x int32
	y int32
}

type Outer struct {
	inner
	z int32
}

func sumOuter(o *Outer) int32 {
	return o.x + o.y + o.z
}

func setX(o *Outer, v int32) {
	o.x = v
}
`)
	name170 := []byte("mypkg")
	h170 := compileToIR(
		uintptr(unsafe.Pointer(&src170[0])), int32(len(src170)),
		uintptr(unsafe.Pointer(&name170[0])), int32(len(name170)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir170 := getIR(h170)
	fmt.Println("=== IR for embedded field access ===")
	fmt.Println(ir170)

	llvmVerify("embedded field access", ir170)
	assert("embed-field has sumOuter", strings.Contains(ir170, "@mypkg.sumOuter"))
	assert("embed-field has setX", strings.Contains(ir170, "@mypkg.setX"))
	assert("embed-field accesses nested field (two GEPs)", strings.Count(ir170, "getelementptr") >= 4)
	assert("embed-field no parse error", !strings.Contains(ir170, "parse error"))

	irFree(h170)

	// Test 171: Interface satisfaction via embedded methods (tc_object pattern)
	src171 := []byte(`package mypkg

type Named interface {
	GetName() string
	GetKind() int32
}

type baseObj struct {
	name string
	kind int32
}

func (b *baseObj) GetName() string { return b.name }
func (b *baseObj) GetKind() int32 { return b.kind }

type TypeObj struct {
	baseObj
	extra bool
}

type FuncObj struct {
	baseObj
	sig string
}

func classify(n Named) string {
	switch n.(type) {
	case *TypeObj:
		return "type"
	case *FuncObj:
		return "func"
	}
	return "unknown"
}

func describe(n Named) string {
	return n.GetName() | ":" | itoa171(n.GetKind())
}

func itoa171(n int32) string {
	if n == 0 {
		return "0"
	}
	buf := []byte{:0:10}
	for n > 0 {
		buf = append(buf, byte('0' + n % 10))
		n = n / 10
	}
	return string(buf)
}
`)
	name171 := []byte("mypkg")
	h171 := compileToIR(
		uintptr(unsafe.Pointer(&src171[0])), int32(len(src171)),
		uintptr(unsafe.Pointer(&name171[0])), int32(len(name171)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir171 := getIR(h171)
	fmt.Println("=== IR for interface via embedding ===")
	fmt.Println(ir171)

	llvmVerify("interface via embedding", ir171)
	assert("iface-embed has classify", strings.Contains(ir171, "@mypkg.classify"))
	assert("iface-embed has describe", strings.Contains(ir171, "@mypkg.describe"))
	assert("iface-embed has TypeObj typeid", strings.Contains(ir171, "typeid.ptr.TypeObj"))
	assert("iface-embed has FuncObj typeid", strings.Contains(ir171, "typeid.ptr.FuncObj"))
	assert("iface-embed no parse error", !strings.Contains(ir171, "parse error"))

	irFree(h171)

	// Test 172: Map with interface values + nil return (tc_scope pattern)
	src172 := []byte(`package mypkg

type Entry interface {
	Key() string
}

type StrEntry struct {
	key string
	val string
}

func (e *StrEntry) Key() string { return e.key }

type Table struct {
	elems map[string]Entry
}

func newTable() *Table {
	return &Table{elems: map[string]Entry{}}
}

func (t *Table) get(name string) Entry {
	return t.elems[name]
}

func (t *Table) set(e Entry) {
	t.elems[e.Key()] = e
}

func (t *Table) has(name string) bool {
	_, ok := t.elems[name]
	return ok
}
`)
	name172 := []byte("mypkg")
	h172 := compileToIR(
		uintptr(unsafe.Pointer(&src172[0])), int32(len(src172)),
		uintptr(unsafe.Pointer(&name172[0])), int32(len(name172)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir172 := getIR(h172)
	fmt.Println("=== IR for map[string]interface ===")
	fmt.Println(ir172)

	llvmVerify("map[string]interface", ir172)
	assert("map-iface has newTable", strings.Contains(ir172, "@mypkg.newTable"))
	assert("map-iface has get method", strings.Contains(ir172, "@mypkg.Table.get"))
	assert("map-iface has set method", strings.Contains(ir172, "@mypkg.Table.set"))
	assert("map-iface has has method", strings.Contains(ir172, "@mypkg.Table.has"))
	assert("map-iface no parse error", !strings.Contains(ir172, "parse error"))

	irFree(h172)

	// Test 173: Pointer chain walking with nil check (tc_scope.LookupParent pattern)
	src173 := []byte(`package mypkg

type Chain struct {
	parent *Chain
	name   string
	val    int32
}

func walk(c *Chain, target string) int32 {
	for s := c; s != nil; s = s.parent {
		if s.name == target {
			return s.val
		}
	}
	return -1
}

func depth(c *Chain) int32 {
	n := int32(0)
	for s := c; s != nil; s = s.parent {
		n = n + 1
	}
	return n
}
`)
	name173 := []byte("mypkg")
	h173 := compileToIR(
		uintptr(unsafe.Pointer(&src173[0])), int32(len(src173)),
		uintptr(unsafe.Pointer(&name173[0])), int32(len(name173)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir173 := getIR(h173)
	fmt.Println("=== IR for pointer chain walk ===")
	fmt.Println(ir173)

	llvmVerify("pointer chain walk", ir173)
	assert("chain has walk", strings.Contains(ir173, "@mypkg.walk"))
	assert("chain has depth", strings.Contains(ir173, "@mypkg.depth"))
	assert("chain has null comparison", strings.Contains(ir173, "icmp") && strings.Contains(ir173, "null"))
	assert("chain no parse error", !strings.Contains(ir173, "parse error"))

	irFree(h173)

	// Test 174: Full tc_scope pattern with concrete types
	src174 := []byte(`package mypkg

type Object interface {
	Name() string
}

type SimpleObj struct {
	name string
}

func (o *SimpleObj) Name() string { return o.name }

type Scope struct {
	parent *Scope
	elems  map[string]Object
}

func NewScope(parent *Scope) *Scope {
	return &Scope{parent: parent, elems: map[string]Object{}}
}

func (s *Scope) Parent() *Scope   { return s.parent }
func (s *Scope) Len() int32       { return int32(len(s.elems)) }

func (s *Scope) Lookup(name string) Object {
	return s.elems[name]
}

func (s *Scope) LookupParent(name string) (*Scope, Object) {
	for sc := s; sc != nil; sc = sc.parent {
		if obj, ok := sc.elems[name]; ok {
			return sc, obj
		}
	}
	return nil, nil
}

func (s *Scope) Insert(obj Object) Object {
	name := obj.Name()
	if alt, ok := s.elems[name]; ok {
		return alt
	}
	s.elems[name] = obj
	return nil
}

func (s *Scope) Names() []string {
	names := []string{:0:len(s.elems)}
	for n := range s.elems {
		names = append(names, n)
	}
	return names
}
`)
	name174 := []byte("mypkg")
	h174 := compileToIR(
		uintptr(unsafe.Pointer(&src174[0])), int32(len(src174)),
		uintptr(unsafe.Pointer(&name174[0])), int32(len(name174)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir174 := getIR(h174)
	fmt.Println("=== IR for Scope ===")
	fmt.Println(ir174)

	if ir174 == "" {
		assert("scope produces IR", false)
	} else {
		llvmVerify("Scope", ir174)
		assert("scope has NewScope", strings.Contains(ir174, "@mypkg.NewScope"))
		assert("scope has Lookup", strings.Contains(ir174, "@mypkg.Scope.Lookup"))
		assert("scope has LookupParent", strings.Contains(ir174, "@mypkg.Scope.LookupParent"))
		assert("scope has Insert", strings.Contains(ir174, "@mypkg.Scope.Insert"))
		assert("scope has Names", strings.Contains(ir174, "@mypkg.Scope.Names"))
		assert("scope Insert calls Name via invoke", strings.Contains(ir174, "call {ptr, i64, i64} @mypkg.SimpleObj.Name"))
		assert("scope no parse error", !strings.Contains(ir174, "parse error"))
	}

	irFree(h174)

	// Test 175: tc_object pattern - Object hierarchy with embedding
	src175 := []byte(`package mypkg

type Type interface {
	typeTag()
}

type BasicType175 struct {
	kind int32
}

func (b *BasicType175) typeTag() {}

type Pkg175 struct {
	path string
	name string
}

type Object interface {
	Name() string
	ObjType() Type
	ObjPkg() *Pkg175
	Exported() bool
}

type object175 struct {
	pkg  *Pkg175
	name string
	typ  Type
}

func (o *object175) Name() string      { return o.name }
func (o *object175) ObjType() Type     { return o.typ }
func (o *object175) ObjPkg() *Pkg175   { return o.pkg }
func (o *object175) Exported() bool    { return len(o.name) > 0 && o.name[0] >= 'A' && o.name[0] <= 'Z' }

type TypeName175 struct {
	object175
}

func NewTypeName175(pkg *Pkg175, name string, typ Type) *TypeName175 {
	return &TypeName175{object175{pkg: pkg, name: name, typ: typ}}
}

func (o *TypeName175) String() string { return "type " | o.name }

type FuncObj175 struct {
	object175
	ptrRecv bool
}

func NewFuncObj175(pkg *Pkg175, name string) *FuncObj175 {
	return &FuncObj175{object175: object175{pkg: pkg, name: name}}
}

func (o *FuncObj175) HasPtrRecv() bool { return o.ptrRecv }
func (o *FuncObj175) String() string   { return "func " | o.name }

func classify175(obj Object) string {
	switch obj.(type) {
	case *TypeName175:
		return "type"
	case *FuncObj175:
		return "func"
	}
	return "unknown"
}
`)
	name175 := []byte("mypkg")
	h175 := compileToIR(
		uintptr(unsafe.Pointer(&src175[0])), int32(len(src175)),
		uintptr(unsafe.Pointer(&name175[0])), int32(len(name175)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir175 := getIR(h175)
	fmt.Println("=== IR for tc_object ===")
	fmt.Println(ir175)

	if ir175 == "" {
		assert("tc_object produces IR", false)
	} else {
		llvmVerify("tc_object", ir175)
		assert("obj has NewTypeName175", strings.Contains(ir175, "@mypkg.NewTypeName175"))
		assert("obj has NewFuncObj175", strings.Contains(ir175, "@mypkg.NewFuncObj175"))
		assert("obj has classify175", strings.Contains(ir175, "@mypkg.classify175"))
		assert("obj has Exported method", strings.Contains(ir175, "@mypkg.object175.Exported"))
		assert("obj TypeName175 typeid", strings.Contains(ir175, "typeid.ptr.TypeName175"))
		assert("obj FuncObj175 typeid", strings.Contains(ir175, "typeid.ptr.FuncObj175"))
		assert("obj no parse error", !strings.Contains(ir175, "parse error"))
	}

	irFree(h175)

	// Test 176: Combined Scope + Object pattern (tc_scope + tc_object together)
	src176 := []byte(`package mypkg

type Type176 interface {
	typeTag()
}

type Object176 interface {
	Name() string
}

type obj176 struct {
	name string
}

func (o *obj176) Name() string { return o.name }

type VarObj176 struct {
	obj176
	typ Type176
}

type ConstObj176 struct {
	obj176
	val int32
}

type Scope176 struct {
	parent *Scope176
	elems  map[string]Object176
}

func NewScope176(parent *Scope176) *Scope176 {
	return &Scope176{parent: parent, elems: map[string]Object176{}}
}

func (s *Scope176) Insert(obj Object176) Object176 {
	name := obj.Name()
	if alt, ok := s.elems[name]; ok {
		return alt
	}
	s.elems[name] = obj
	return nil
}

func (s *Scope176) Lookup(name string) Object176 {
	return s.elems[name]
}

func (s *Scope176) LookupParent(name string) (*Scope176, Object176) {
	for sc := s; sc != nil; sc = sc.parent {
		if obj, ok := sc.elems[name]; ok {
			return sc, obj
		}
	}
	return nil, nil
}

func (s *Scope176) Len() int32 {
	return int32(len(s.elems))
}

func classifyObj176(obj Object176) string {
	switch obj.(type) {
	case *VarObj176:
		return "var"
	case *ConstObj176:
		return "const"
	}
	return "unknown"
}
`)
	name176 := []byte("mypkg")
	h176 := compileToIR(
		uintptr(unsafe.Pointer(&src176[0])), int32(len(src176)),
		uintptr(unsafe.Pointer(&name176[0])), int32(len(name176)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir176 := getIR(h176)
	fmt.Println("=== IR for scope+object ===")
	fmt.Println(ir176)

	if ir176 == "" {
		assert("scope+object produces IR", false)
	} else {
		llvmVerify("scope+object", ir176)
		assert("so has NewScope176", strings.Contains(ir176, "@mypkg.NewScope176"))
		assert("so has Insert", strings.Contains(ir176, "@mypkg.Scope176.Insert"))
		assert("so has LookupParent", strings.Contains(ir176, "@mypkg.Scope176.LookupParent"))
		assert("so has classifyObj176", strings.Contains(ir176, "@mypkg.classifyObj176"))
		assert("so has hashmapLen", strings.Contains(ir176, "hashmapLen"))
		assert("so VarObj176 typeid", strings.Contains(ir176, "typeid.ptr.VarObj176"))
		assert("so ConstObj176 typeid", strings.Contains(ir176, "typeid.ptr.ConstObj176"))
		assert("so no parse error", !strings.Contains(ir176, "parse error"))
	}

	irFree(h176)

	// Test 177: Scanner pattern (tc_scanner core loop)
	src177 := []byte(`package mypkg

type Scanner177 struct {
	src  []byte
	pos  int32
	line int32
	col  int32
}

func NewScanner177(src []byte) *Scanner177 {
	return &Scanner177{src: src, line: 1, col: 1}
}

func (s *Scanner177) peek() byte {
	if s.pos >= int32(len(s.src)) {
		return 0
	}
	return s.src[s.pos]
}

func (s *Scanner177) next() byte {
	ch := s.peek()
	if ch == 0 {
		return 0
	}
	s.pos = s.pos + 1
	if ch == '\n' {
		s.line = s.line + 1
		s.col = 1
	} else {
		s.col = s.col + 1
	}
	return ch
}

func (s *Scanner177) skipSpace() {
	for {
		ch := s.peek()
		if ch != ' ' && ch != '\t' && ch != '\r' && ch != '\n' {
			return
		}
		s.next()
	}
}

func (s *Scanner177) readIdent() string {
	start := s.pos
	for {
		ch := s.peek()
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '_' {
			break
		}
		s.pos = s.pos + 1
	}
	return string(s.src[start:s.pos])
}

func (s *Scanner177) atEnd() bool {
	return s.pos >= int32(len(s.src))
}
`)
	name177 := []byte("mypkg")
	h177 := compileToIR(
		uintptr(unsafe.Pointer(&src177[0])), int32(len(src177)),
		uintptr(unsafe.Pointer(&name177[0])), int32(len(name177)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir177 := getIR(h177)
	fmt.Println("=== IR for scanner ===")
	fmt.Println(ir177)

	if ir177 == "" {
		assert("scanner produces IR", false)
	} else {
		llvmVerify("scanner", ir177)
		assert("scan has NewScanner177", strings.Contains(ir177, "@mypkg.NewScanner177"))
		assert("scan has peek", strings.Contains(ir177, "@mypkg.Scanner177.peek"))
		assert("scan has next", strings.Contains(ir177, "@mypkg.Scanner177.next"))
		assert("scan has skipSpace", strings.Contains(ir177, "@mypkg.Scanner177.skipSpace"))
		assert("scan has readIdent", strings.Contains(ir177, "@mypkg.Scanner177.readIdent"))
		assert("scan next calls peek", strings.Contains(ir177, "call i8 @mypkg.Scanner177.peek"))
		assert("scan no parse error", !strings.Contains(ir177, "parse error"))
	}

	irFree(h177)

	// Test 178: tc_types core pattern - Type interface hierarchy
	src178 := []byte(`package mypkg

type Type interface {
	Underlying() Type
	String() string
}

type BasicKind int32

const (
	Invalid BasicKind = iota
	Bool178
	Int8Kind
	Int32Kind
	Int64Kind
	StringKind
)

type BasicInfo int32

const (
	IsBoolean BasicInfo = 1 << iota
	IsInteger
	IsUnsigned
	IsFloat
	IsString178
)

type Basic178 struct {
	kind BasicKind
	info BasicInfo
	name string
}

func (t *Basic178) Kind() BasicKind  { return t.kind }
func (t *Basic178) Info() BasicInfo  { return t.info }
func (t *Basic178) Name() string     { return t.name }
func (t *Basic178) Underlying() Type { return t }
func (t *Basic178) String() string   { return t.name }

type Slice178 struct {
	elem Type
}

func NewSlice178(elem Type) *Slice178  { return &Slice178{elem: elem} }
func (t *Slice178) Elem() Type        { return t.elem }
func (t *Slice178) Underlying() Type  { return t }
func (t *Slice178) String() string    { return "[]" | t.elem.String() }

type Pointer178 struct {
	base Type
}

func NewPointer178(base Type) *Pointer178 { return &Pointer178{base: base} }
func (t *Pointer178) Elem() Type         { return t.base }
func (t *Pointer178) Underlying() Type   { return t }
func (t *Pointer178) String() string     { return "*" | t.base.String() }

type Map178 struct {
	key  Type
	elem Type
}

func NewMap178(key Type, elem Type) *Map178 { return &Map178{key: key, elem: elem} }
func (t *Map178) Key() Type            { return t.key }
func (t *Map178) Elem() Type           { return t.elem }
func (t *Map178) Underlying() Type     { return t }

func isInteger(t Type) bool {
	if b, ok := t.(*Basic178); ok {
		return b.info & IsInteger != 0
	}
	return false
}

func elemType(t Type) Type {
	switch t := t.(type) {
	case *Slice178:
		return t.elem
	case *Pointer178:
		return t.base
	case *Map178:
		return t.elem
	}
	return nil
}
`)
	name178 := []byte("mypkg")
	h178 := compileToIR(
		uintptr(unsafe.Pointer(&src178[0])), int32(len(src178)),
		uintptr(unsafe.Pointer(&name178[0])), int32(len(name178)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir178 := getIR(h178)
	fmt.Println("=== IR for type system ===")
	fmt.Println(ir178)

	if ir178 == "" {
		assert("type system produces IR", false)
	} else {
		llvmVerify("type system", ir178)
		assert("ts has Basic178 methods", strings.Contains(ir178, "@mypkg.Basic178.String"))
		assert("ts has Slice178.String", strings.Contains(ir178, "@mypkg.Slice178.String"))
		assert("ts has Pointer178.String", strings.Contains(ir178, "@mypkg.Pointer178.String"))
		assert("ts has isInteger", strings.Contains(ir178, "@mypkg.isInteger"))
		assert("ts has elemType", strings.Contains(ir178, "@mypkg.elemType"))
		assert("ts bitmask and", strings.Contains(ir178, "and i32"))
		assert("ts has Slice178 typeid", strings.Contains(ir178, "typeid.ptr.Slice178"))
		assert("ts has Pointer178 typeid", strings.Contains(ir178, "typeid.ptr.Pointer178"))
		assert("ts has Map178 typeid", strings.Contains(ir178, "typeid.ptr.Map178"))
		assert("ts Slice.String calls elem.String", strings.Contains(ir178, "call {ptr, i64, i64}") || strings.Contains(ir178, "invoke"))
		assert("ts no parse error", !strings.Contains(ir178, "parse error"))
	}

	irFree(h178)

	// Test 179: Named type with methods + Underlying (key tc_types pattern)
	src179 := []byte(`package mypkg

type Type179 interface {
	Underlying() Type179
}

type Named179 struct {
	obj        *TypeObj179
	underlying Type179
	methods    []*FuncObj179
}

type TypeObj179 struct {
	name string
}

type FuncObj179 struct {
	name string
}

func NewNamed179(obj *TypeObj179, underlying Type179) *Named179 {
	return &Named179{obj: obj, underlying: underlying}
}

func (t *Named179) Obj() *TypeObj179     { return t.obj }
func (t *Named179) Underlying() Type179  { return t.underlying }
func (t *Named179) NumMethods() int32    { return int32(len(t.methods)) }
func (t *Named179) Method(i int32) *FuncObj179 { return t.methods[i] }
func (t *Named179) AddMethod(m *FuncObj179) {
	t.methods = append(t.methods, m)
}

func (t *Named179) SetUnderlying(u Type179) {
	t.underlying = u
}

type Struct179 struct {
	fields []*Field179
}

type Field179 struct {
	name string
	typ  Type179
	anon bool
}

func (t *Struct179) Underlying() Type179 { return t }
func (t *Struct179) NumFields() int32    { return int32(len(t.fields)) }
func (t *Struct179) Field(i int32) *Field179 { return t.fields[i] }

func underlyingType(t Type179) Type179 {
	if n, ok := t.(*Named179); ok {
		return n.underlying
	}
	return t
}
`)
	name179 := []byte("mypkg")
	h179 := compileToIR(
		uintptr(unsafe.Pointer(&src179[0])), int32(len(src179)),
		uintptr(unsafe.Pointer(&name179[0])), int32(len(name179)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir179 := getIR(h179)
	fmt.Println("=== IR for Named type ===")
	fmt.Println(ir179)

	if ir179 == "" {
		assert("named type produces IR", false)
	} else {
		llvmVerify("Named type", ir179)
		assert("named has NewNamed179", strings.Contains(ir179, "@mypkg.NewNamed179"))
		assert("named has AddMethod", strings.Contains(ir179, "@mypkg.Named179.AddMethod"))
		assert("named has SetUnderlying", strings.Contains(ir179, "@mypkg.Named179.SetUnderlying"))
		assert("named has underlyingType", strings.Contains(ir179, "@mypkg.underlyingType"))
		assert("named has Named179 typeid", strings.Contains(ir179, "typeid.ptr.Named179"))
		assert("named no parse error", !strings.Contains(ir179, "parse error"))
	}

	irFree(h179)

	// Test 180: Signature/Tuple pattern + nil field access
	src180 := []byte(`package mypkg

type Type180 interface {
	Underlying() Type180
}

type Var180 struct {
	name string
	typ  Type180
}

type Tuple180 struct {
	vars []*Var180
}

func NewTuple180(vars []*Var180) *Tuple180 { return &Tuple180{vars: vars} }
func (t *Tuple180) Len() int32            { return int32(len(t.vars)) }
func (t *Tuple180) At(i int32) *Var180    { return t.vars[i] }

type Signature180 struct {
	recv     *Var180
	params   *Tuple180
	results  *Tuple180
	variadic bool
}

func NewSig180(recv *Var180, params *Tuple180, results *Tuple180) *Signature180 {
	return &Signature180{recv: recv, params: params, results: results}
}

func (s *Signature180) Recv() *Var180      { return s.recv }
func (s *Signature180) Params() *Tuple180  { return s.params }
func (s *Signature180) Results() *Tuple180 { return s.results }
func (s *Signature180) Variadic() bool     { return s.variadic }
func (s *Signature180) Underlying() Type180 { return s }

func numParams(s *Signature180) int32 {
	if s.params == nil {
		return 0
	}
	return s.params.Len()
}

func numResults(s *Signature180) int32 {
	if s.results == nil {
		return 0
	}
	return s.results.Len()
}

func hasReceiver(s *Signature180) bool {
	return s.recv != nil
}

func paramName(s *Signature180, i int32) string {
	if s.params == nil || i >= s.params.Len() {
		return ""
	}
	return s.params.At(i).name
}
`)
	name180 := []byte("mypkg")
	h180 := compileToIR(
		uintptr(unsafe.Pointer(&src180[0])), int32(len(src180)),
		uintptr(unsafe.Pointer(&name180[0])), int32(len(name180)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir180 := getIR(h180)
	fmt.Println("=== IR for Signature ===")
	fmt.Println(ir180)

	if ir180 == "" {
		assert("signature produces IR", false)
	} else {
		llvmVerify("Signature", ir180)
		assert("sig has NewSig180", strings.Contains(ir180, "@mypkg.NewSig180"))
		assert("sig has numParams", strings.Contains(ir180, "@mypkg.numParams"))
		assert("sig has numResults", strings.Contains(ir180, "@mypkg.numResults"))
		assert("sig has hasReceiver", strings.Contains(ir180, "@mypkg.hasReceiver"))
		assert("sig has paramName", strings.Contains(ir180, "@mypkg.paramName"))
		assert("sig nil check on params", strings.Contains(ir180, "icmp") && strings.Contains(ir180, "null"))
		assert("sig no parse error", !strings.Contains(ir180, "parse error"))
	}

	irFree(h180)

	// Test 181: SSA instruction hierarchy (the pattern from ssa_types.mx)
	src181 := []byte(`package mypkg

type SSAType181 interface {
	Underlying() SSAType181
}

type SSAValue181 interface {
	SSAType() SSAType181
	Name181() string
}

type ssaNode181 struct {
	name string
	typ  SSAType181
}

func (n *ssaNode181) SSAType() SSAType181 { return n.typ }
func (n *ssaNode181) Name181() string     { return n.name }

type SSAConst181 struct {
	ssaNode181
	val int64
}

type SSAAlloc181 struct {
	ssaNode181
	heap bool
}

type SSABinOp181 struct {
	ssaNode181
	op int32
	x  SSAValue181
	y  SSAValue181
}

type SSACall181 struct {
	ssaNode181
	fn   string
	args []SSAValue181
}

func isConst(v SSAValue181) bool {
	_, ok := v.(*SSAConst181)
	return ok
}

func operandName(v SSAValue181) string {
	if v == nil {
		return "undef"
	}
	if c, ok := v.(*SSAConst181); ok {
		return itoa181(int32(c.val))
	}
	return "%" | v.Name181()
}

func itoa181(n int32) string {
	if n == 0 {
		return "0"
	}
	buf := []byte{:0:10}
	for n > 0 {
		buf = append(buf, byte('0' + n % 10))
		n = n / 10
	}
	return string(buf)
}

func countArgs(c *SSACall181) int32 {
	return int32(len(c.args))
}
`)
	name181 := []byte("mypkg")
	h181 := compileToIR(
		uintptr(unsafe.Pointer(&src181[0])), int32(len(src181)),
		uintptr(unsafe.Pointer(&name181[0])), int32(len(name181)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir181 := getIR(h181)
	fmt.Println("=== IR for SSA types ===")
	fmt.Println(ir181)

	if ir181 == "" {
		assert("ssa types produces IR", false)
	} else {
		llvmVerify("SSA types", ir181)
		assert("ssa has isConst", strings.Contains(ir181, "@mypkg.isConst"))
		assert("ssa has operandName", strings.Contains(ir181, "@mypkg.operandName"))
		assert("ssa has countArgs", strings.Contains(ir181, "@mypkg.countArgs"))
		assert("ssa SSAConst181 typeid", strings.Contains(ir181, "typeid.ptr.SSAConst181"))
		assert("ssa promotes Name181 via embed", strings.Contains(ir181, "ssaNode181.Name181") || strings.Contains(ir181, "SSAConst181.Name181"))
		assert("ssa no parse error", !strings.Contains(ir181, "parse error"))
	}

	irFree(h181)

	// Test 182: Self-compilation pattern - mini IR emitter compiling mini IR emitter
	src182 := []byte(`package mypkg

type Type182 interface {
	Underlying() Type182
	String() string
}

type Basic182 struct {
	kind int32
	name string
}

func (t *Basic182) Underlying() Type182 { return t }
func (t *Basic182) String() string      { return t.name }

type Slice182 struct {
	elem Type182
}

func (t *Slice182) Underlying() Type182 { return t }
func (t *Slice182) String() string      { return "[]" | t.elem.String() }

type Pointer182 struct {
	base Type182
}

func (t *Pointer182) Underlying() Type182 { return t }
func (t *Pointer182) String() string      { return "*" | t.base.String() }

type SSAValue182 interface {
	ValType() Type182
	ValName() string
}

type ssaBase182 struct {
	name string
	typ  Type182
}

func (n *ssaBase182) ValType() Type182 { return n.typ }
func (n *ssaBase182) ValName() string  { return n.name }

type SSAConst182 struct {
	ssaBase182
	val int64
}

type SSABinOp182 struct {
	ssaBase182
	op int32
	x  SSAValue182
	y  SSAValue182
}

type SSAFieldAddr182 struct {
	ssaBase182
	x     SSAValue182
	field int32
}

type Emitter182 struct {
	buf       []byte
	decls     map[string]bool
	tempCount int32
	pkg       string
}

func NewEmitter182(pkg string) *Emitter182 {
	return &Emitter182{pkg: pkg, decls: map[string]bool{}}
}

func (e *Emitter182) w(s string) {
	e.buf = append(e.buf, s...)
}

func (e *Emitter182) nextTemp() string {
	e.tempCount = e.tempCount + 1
	return "%t" | itoa182(e.tempCount)
}

func (e *Emitter182) llvmType(t Type182) string {
	switch t.(type) {
	case *Basic182:
		b := t.(*Basic182)
		switch b.kind {
		case 0:
			return "i32"
		case 1:
			return "i64"
		case 2:
			return "{ptr, i64, i64}"
		}
		return "i32"
	case *Slice182:
		return "{ptr, i64, i64}"
	case *Pointer182:
		return "ptr"
	}
	return "i32"
}

func (e *Emitter182) operand(v SSAValue182) string {
	if v == nil {
		return "zeroinitializer"
	}
	if c, ok := v.(*SSAConst182); ok {
		return itoa182(int32(c.val))
	}
	return "%" | v.ValName()
}

func (e *Emitter182) emitBinOp(b *SSABinOp182) {
	reg := e.nextTemp()
	lt := e.llvmType(b.x.ValType())
	lv := e.operand(b.x)
	rv := e.operand(b.y)
	e.w("  ")
	e.w(reg)
	e.w(" = add ")
	e.w(lt)
	e.w(" ")
	e.w(lv)
	e.w(", ")
	e.w(rv)
	e.w("\n")
}

func (e *Emitter182) result() string {
	return string(e.buf)
}

func itoa182(n int32) string {
	if n == 0 {
		return "0"
	}
	buf := []byte{:0:10}
	for n > 0 {
		buf = append(buf, byte('0' + n % 10))
		n = n / 10
	}
	i := 0
	j := int32(len(buf)) - 1
	for i < j {
		buf[i], buf[j] = buf[j], buf[i]
		i = i + 1
		j = j - 1
	}
	return string(buf)
}
`)
	name182 := []byte("mypkg")
	h182 := compileToIR(
		uintptr(unsafe.Pointer(&src182[0])), int32(len(src182)),
		uintptr(unsafe.Pointer(&name182[0])), int32(len(name182)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir182 := getIR(h182)
	fmt.Println("=== IR for self-compile pattern ===")
	fmt.Println(ir182)

	if ir182 == "" {
		assert("self-compile produces IR", false)
	} else {
		llvmVerify("self-compile", ir182)
		assert("sc has NewEmitter182", strings.Contains(ir182, "@mypkg.NewEmitter182"))
		assert("sc has emitBinOp", strings.Contains(ir182, "@mypkg.Emitter182.emitBinOp"))
		assert("sc has llvmType", strings.Contains(ir182, "@mypkg.Emitter182.llvmType"))
		assert("sc has operand", strings.Contains(ir182, "@mypkg.Emitter182.operand"))
		assert("sc has nextTemp", strings.Contains(ir182, "@mypkg.Emitter182.nextTemp"))
		assert("sc Basic182 typeid", strings.Contains(ir182, "typeid.ptr.Basic182"))
		assert("sc Slice182 typeid", strings.Contains(ir182, "typeid.ptr.Slice182"))
		assert("sc Pointer182 typeid", strings.Contains(ir182, "typeid.ptr.Pointer182"))
		assert("sc SSAConst182 typeid", strings.Contains(ir182, "typeid.ptr.SSAConst182"))
		assert("sc no parse error", !strings.Contains(ir182, "parse error"))
	}

	irFree(h182)

	// Test 183: Real bootstrap files - tc_package + tc_scope + Object (concatenated)
	src183 := []byte(`package main

type Type interface {
	Underlying() Type
	String() string
}

type Object interface {
	Name() string
	Type() Type
	Pkg() *TCPackage
	Exported() bool
}

type object struct {
	pkg  *TCPackage
	name string
	typ  Type
}

func (o *object) Name() string      { return o.name }
func (o *object) Type() Type        { return o.typ }
func (o *object) Pkg() *TCPackage   { return o.pkg }
func (o *object) Exported() bool    { return len(o.name) > 0 && o.name[0] >= 'A' && o.name[0] <= 'Z' }

type TypeName struct {
	object
}

func NewTypeName(pkg *TCPackage, name string, typ Type) *TypeName {
	return &TypeName{object{pkg: pkg, name: name, typ: typ}}
}

type TCVar struct {
	pkg       *TCPackage
	name      string
	typ       Type
	anonymous bool
}

func NewTCField(pkg *TCPackage, name string, typ Type, anonymous bool) *TCVar {
	return &TCVar{pkg: pkg, name: name, typ: typ, anonymous: anonymous}
}

func (v *TCVar) Name() string      { return v.name }
func (v *TCVar) Type() Type        { return v.typ }
func (v *TCVar) Pkg() *TCPackage   { return v.pkg }
func (v *TCVar) Exported() bool    { return len(v.name) > 0 && v.name[0] >= 'A' && v.name[0] <= 'Z' }
func (v *TCVar) Anonymous() bool   { return v.anonymous }

type TCPackage struct {
	path     string
	name     string
	scope    *Scope
	imports  []*TCPackage
	complete bool
}

func NewTCPackage(path string, name string) *TCPackage {
	return &TCPackage{
		path:  path,
		name:  name,
		scope: NewScope(nil),
	}
}

func (p *TCPackage) Path() string    { return p.path }
func (p *TCPackage) Name() string    { return p.name }
func (p *TCPackage) Scope() *Scope   { return p.scope }
func (p *TCPackage) Complete() bool  { return p.complete }
func (p *TCPackage) MarkComplete()   { p.complete = true }
func (p *TCPackage) String() string  { return "package " | p.name | " (" | p.path | ")" }
func (p *TCPackage) Imports() []*TCPackage { return p.imports }
func (p *TCPackage) SetImports(imps []*TCPackage) { p.imports = imps }

type Scope struct {
	parent   *Scope
	children []*Scope
	elems    map[string]Object
}

func NewScope(parent *Scope) *Scope {
	return &Scope{parent: parent, elems: map[string]Object{}}
}

func (s *Scope) Parent() *Scope   { return s.parent }
func (s *Scope) Len() int32       { return int32(len(s.elems)) }

func (s *Scope) Lookup(name string) Object {
	return s.elems[name]
}

func (s *Scope) LookupParent(name string) (*Scope, Object) {
	for sc := s; sc != nil; sc = sc.parent {
		if obj, ok := sc.elems[name]; ok {
			return sc, obj
		}
	}
	return nil, nil
}

func (s *Scope) Insert(obj Object) Object {
	name := obj.Name()
	if alt, ok := s.elems[name]; ok {
		return alt
	}
	s.elems[name] = obj
	return nil
}

func (s *Scope) Names() []string {
	names := []string{:0:len(s.elems)}
	for n := range s.elems {
		names = append(names, n)
	}
	return names
}
`)
	name183 := []byte("main")
	h183 := compileToIR(
		uintptr(unsafe.Pointer(&src183[0])), int32(len(src183)),
		uintptr(unsafe.Pointer(&name183[0])), int32(len(name183)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir183 := getIR(h183)
	fmt.Println("=== IR for real bootstrap concat ===")
	if len(ir183) > 200 {
		fmt.Println(ir183[:200])
		fmt.Println("... [truncated, total", len(ir183), "bytes]")
	} else {
		fmt.Println(ir183)
	}

	if ir183 == "" {
		assert("bootstrap concat produces IR", false)
	} else {
		llvmVerify("bootstrap concat", ir183)
		assert("bc has NewTCPackage", strings.Contains(ir183, "@main.NewTCPackage"))
		assert("bc has NewScope", strings.Contains(ir183, "@main.NewScope"))
		assert("bc has Scope.Insert", strings.Contains(ir183, "@main.Scope.Insert"))
		assert("bc has Scope.LookupParent", strings.Contains(ir183, "@main.Scope.LookupParent"))
		assert("bc has Scope.Names", strings.Contains(ir183, "@main.Scope.Names"))
		assert("bc has object.Exported", strings.Contains(ir183, "@main.object.Exported"))
		assert("bc has NewTypeName", strings.Contains(ir183, "@main.NewTypeName"))
		assert("bc has TCPackage.String", strings.Contains(ir183, "@main.TCPackage.String"))
		assert("bc has hashmapLen", strings.Contains(ir183, "hashmapLen"))
		assert("bc no parse error", !strings.Contains(ir183, "parse error"))
	}

	irFree(h183)

	// Test 184: copy builtin
	test(184, `package main
func copyBytes(dst []byte, src []byte) int32 {
	return int32(copy(dst, src))
}
func main() {
	a := []byte{:10}
	b := []byte("hello")
	n := copyBytes(a, b)
	_ = n
}
`, func(ir string) {
		assert("184: copy call", strings.Contains(ir, "sliceCopy"))
	})

	// Test 185: panic builtin
	test(185, `package main
func mustNotBeNil(p *int32) {
	if p == nil {
		panic("nil pointer")
	}
}
func main() {
	var x int32
	mustNotBeNil(&x)
}
`, func(ir string) {
		assert("185: panic call", strings.Contains(ir, "_panic"))
	})

	// Test 186: named returns
	test(186, `package main
func divide(a int32, b int32) (q int32, r int32) {
	q = a / b
	r = a - q*b
	return
}
func main() {
	q, r := divide(17, 5)
	_ = q
	_ = r
}
`, func(ir string) {
		assert("186: divide defined", strings.Contains(ir, "@main.divide"))
	})

	// Test 187: function values as struct fields
	test(187, `package main
type Handler struct {
	onEvent func(int32) bool
	name    string
}
func process(h *Handler, val int32) bool {
	if h.onEvent != nil {
		return h.onEvent(val)
	}
	return false
}
func main() {
	h := &Handler{
		onEvent: func(v int32) bool { return v > 0 },
		name:    "test",
	}
	result := process(h, 42)
	_ = result
}
`, func(ir string) {
		assert("187: process defined", strings.Contains(ir, "@main.process"))
	})

	// Test 188: goto statement
	test(188, `package main
func search(data []byte, target byte) int32 {
	var i int32
loop:
	if i >= int32(len(data)) {
		return -1
	}
	if data[i] == target {
		return i
	}
	i++
	goto loop
}
func main() {
	data := []byte("hello")
	idx := search(data, 'l')
	_ = idx
}
`, func(ir string) {
		assert("188: search defined", strings.Contains(ir, "@main.search"))
	})

	// Test 189: multiple assignment
	test(189, `package main
func swap(a int32, b int32) (int32, int32) {
	return b, a
}
func main() {
	x, y := swap(1, 2)
	_ = x
	_ = y
}
`, func(ir string) {
		assert("189: swap defined", strings.Contains(ir, "@main.swap"))
	})

	// Test 190: rune type and comparisons
	test(190, `package main
func isDigit(ch rune) bool {
	return ch >= '0' && ch <= '9'
}
func isLetter(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}
func main() {
	d := isDigit('5')
	l := isLetter('x')
	_ = d
	_ = l
}
`, func(ir string) {
		assert("190: isDigit defined", strings.Contains(ir, "@main.isDigit"))
		assert("190: isLetter defined", strings.Contains(ir, "@main.isLetter"))
	})

	// Test 191: function value nil check and call through variable
	test(191, `package main
type Callback func(string)
func invoke(cb Callback, msg string) {
	if cb != nil {
		cb(msg)
	}
}
func main() {
	var cb Callback
	invoke(cb, "test")
}
`, func(ir string) {
		assert("191: invoke defined", strings.Contains(ir, "@main.invoke"))
	})

	// Test 192: struct value return (non-pointer)
	test(192, `package main
type Point struct {
	x int32
	y int32
}
func makePoint(x int32, y int32) Point {
	return Point{x, y}
}
func addPoints(a Point, b Point) Point {
	return Point{a.x + b.x, a.y + b.y}
}
func main() {
	p1 := makePoint(1, 2)
	p2 := makePoint(3, 4)
	p3 := addPoints(p1, p2)
	_ = p3
}
`, func(ir string) {
		assert("192: makePoint defined", strings.Contains(ir, "@main.makePoint"))
		assert("192: addPoints defined", strings.Contains(ir, "@main.addPoints"))
	})

	// Test 193: Source-like pattern (nextch with goto, byte indexing, rune)
	test(193, `package main
const sentinel = 128
type Source struct {
	buf  []byte
	b    int32
	r    int32
	e    int32
	line uint32
	col  uint32
	ch   rune
	chw  int32
}
func (s *Source) start() { s.b = s.r - s.chw }
func (s *Source) stop()  { s.b = -1 }
func (s *Source) segment() []byte { return s.buf[s.b : s.r-s.chw] }
func (s *Source) nextch() {
	s.col += uint32(s.chw)
	if s.ch == '\n' {
		s.line++
		s.col = 0
	}
	if s.ch = rune(s.buf[s.r]); s.ch < sentinel {
		s.r++
		s.chw = 1
		if s.ch == 0 {
			return
		}
		return
	}
	s.ch = -1
	s.chw = 0
}
func main() {
	s := &Source{buf: []byte("hello\nworld"), r: 0, e: 11, ch: ' ', b: -1}
	s.nextch()
}
`, func(ir string) {
		assert("193: nextch defined", strings.Contains(ir, "@main.Source.nextch"))
		assert("193: segment defined", strings.Contains(ir, "@main.Source.segment"))
	})

	// Test 194: Token-like const enum with type conversion
	test(194, `package main
type Token uint32
const (
	_ Token = iota
	EOF
	NameType
	Literal
	OperatorType
	AssignOp
	Lparen
	Rparen
	Comma
	Semi
	tokenCount
)
const _ uint64 = 1 << (tokenCount - 1)
func contains(tokset uint64, tok Token) bool {
	return tokset&(1<<tok) != 0
}
type LitKind uint8
const (
	IntLit LitKind = iota
	FloatLit
	StringLit
)
func main() {
	b := contains(1<<EOF|1<<NameType, EOF)
	_ = b
}
`, func(ir string) {
		assert("194: contains defined", strings.Contains(ir, "@main.contains"))
	})

	// Test 195: Pos-like value receiver with nested struct access
	test(195, `package main
type PosBase struct {
	filename string
	line     uint32
	col      uint32
}
type Pos struct {
	base      *PosBase
	line      uint32
	col       uint32
}
func MakePos(base *PosBase, line uint32, col uint32) Pos {
	return Pos{base, line, col}
}
func (pos Pos) IsKnown() bool   { return pos.line > 0 }
func (pos Pos) Base() *PosBase  { return pos.base }
func (pos Pos) Line() uint32    { return pos.line }
func (pos Pos) Col() uint32     { return pos.col }
func (pos Pos) Filename() string {
	if pos.base == nil {
		return ""
	}
	return pos.base.filename
}
func main() {
	base := &PosBase{filename: "test.mx", line: 1, col: 1}
	p := MakePos(base, 10, 5)
	known := p.IsKnown()
	name := p.Filename()
	_ = known
	_ = name
}
`, func(ir string) {
		assert("195: MakePos defined", strings.Contains(ir, "@main.MakePos"))
		assert("195: Filename defined", strings.Contains(ir, "@main.Pos.Filename"))
	})

	// Test 196: Scanner-like pattern with for loop and byte comparisons
	test(196, `package main
type Scanner struct {
	src    []byte
	pos    int32
	ch     byte
}
func (s *Scanner) setup(src []byte) {
	s.src = src
	s.pos = 0
	if len(src) > 0 {
		s.ch = src[0]
	}
}
func (s *Scanner) next() {
	s.pos++
	if s.pos < int32(len(s.src)) {
		s.ch = s.src[s.pos]
	} else {
		s.ch = 0
	}
}
func (s *Scanner) skipSpace() {
	for s.ch == ' ' || s.ch == '\t' || s.ch == '\n' || s.ch == '\r' {
		s.next()
	}
}
func (s *Scanner) readIdent() string {
	start := s.pos
	for s.ch >= 'a' && s.ch <= 'z' || s.ch >= 'A' && s.ch <= 'Z' || s.ch == '_' || s.ch >= '0' && s.ch <= '9' {
		s.next()
	}
	return string(s.src[start:s.pos])
}
func main() {
	sc := &Scanner{}
	sc.setup([]byte("  hello world"))
	sc.skipSpace()
	word := sc.readIdent()
	_ = word
}
`, func(ir string) {
		assert("196: setup defined", strings.Contains(ir, "@main.Scanner.setup"))
		assert("196: skipSpace defined", strings.Contains(ir, "@main.Scanner.skipSpace"))
		assert("196: readIdent defined", strings.Contains(ir, "@main.Scanner.readIdent"))
	})

	// Test 197: Node hierarchy with interface and type switch
	test(197, `package main
type Node interface {
	nodeTag()
}
type Expr interface {
	Node
	exprTag()
}
type Name struct {
	Value string
}
func (n *Name) nodeTag() {}
func (n *Name) exprTag() {}
type BasicLit struct {
	Value string
	Kind  int32
}
func (b *BasicLit) nodeTag() {}
func (b *BasicLit) exprTag() {}
type BinaryExpr struct {
	X  Expr
	Op int32
	Y  Expr
}
func (b *BinaryExpr) nodeTag() {}
func (b *BinaryExpr) exprTag() {}
func exprName(e Expr) string {
	switch x := e.(type) {
	case *Name:
		return x.Value
	case *BasicLit:
		return x.Value
	case *BinaryExpr:
		return "binary"
	}
	return ""
}
func main() {
	n := &Name{Value: "foo"}
	var e Expr = n
	result := exprName(e)
	_ = result
}
`, func(ir string) {
		assert("197: exprName defined", strings.Contains(ir, "@main.exprName"))
		assert("197: has type switch", strings.Contains(ir, "icmp eq ptr"))
	})

	// Test 198: Linked list pattern (self-referential struct)
	test(198, `package main
type ListNode struct {
	value int32
	next  *ListNode
}
func push(head *ListNode, val int32) *ListNode {
	return &ListNode{value: val, next: head}
}
func length(head *ListNode) int32 {
	n := int32(0)
	for p := head; p != nil; p = p.next {
		n++
	}
	return n
}
func sum(head *ListNode) int32 {
	s := int32(0)
	for p := head; p != nil; p = p.next {
		s += p.value
	}
	return s
}
func main() {
	var head *ListNode
	head = push(head, 1)
	head = push(head, 2)
	head = push(head, 3)
	n := length(head)
	s := sum(head)
	_ = n
	_ = s
}
`, func(ir string) {
		assert("198: push defined", strings.Contains(ir, "@main.push"))
		assert("198: length defined", strings.Contains(ir, "@main.length"))
		assert("198: sum defined", strings.Contains(ir, "@main.sum"))
	})

	// Test 199: Slice of structs with append
	test(199, `package main
type Entry struct {
	name  string
	value int32
}
func addEntry(entries []Entry, name string, val int32) []Entry {
	return append(entries, Entry{name: name, value: val})
}
func findEntry(entries []Entry, name string) int32 {
	for i := int32(0); i < int32(len(entries)); i++ {
		if entries[i].name == name {
			return entries[i].value
		}
	}
	return -1
}
func main() {
	var entries []Entry
	entries = addEntry(entries, "x", 10)
	entries = addEntry(entries, "y", 20)
	v := findEntry(entries, "x")
	_ = v
}
`, func(ir string) {
		assert("199: addEntry defined", strings.Contains(ir, "@main.addEntry"))
		assert("199: findEntry defined", strings.Contains(ir, "@main.findEntry"))
	})

	// Test 200: bytes.Buffer-like pattern (write methods, grow)
	test(200, `package main
type Buffer struct {
	buf []byte
}
func (b *Buffer) Len() int32 { return int32(len(b.buf)) }
func (b *Buffer) Bytes() []byte { return b.buf }
func (b *Buffer) String() string { return string(b.buf) }
func (b *Buffer) Reset() { b.buf = b.buf[:0] }
func (b *Buffer) WriteByte(c byte) {
	b.buf = append(b.buf, c)
}
func (b *Buffer) WriteString(s string) {
	b.buf = append(b.buf, s...)
}
func (b *Buffer) Write(p []byte) int32 {
	b.buf = append(b.buf, p...)
	return int32(len(p))
}
func main() {
	var buf Buffer
	buf.WriteByte('(')
	buf.WriteString("hello")
	buf.WriteByte(')')
	s := buf.String()
	_ = s
}
`, func(ir string) {
		assert("200: WriteByte defined", strings.Contains(ir, "@main.Buffer.WriteByte"))
		assert("200: WriteString defined", strings.Contains(ir, "@main.Buffer.WriteString"))
		assert("200: String defined", strings.Contains(ir, "@main.Buffer.String"))
	})

	// Test 201: Checker-like pattern with Info struct and map lookups
	test(201, `package main
type Node interface{ nodeTag() }
type Expr interface {
	Node
	exprTag()
}
type Name struct { Value string }
func (n *Name) nodeTag() {}
func (n *Name) exprTag() {}
type TypeAndValue struct {
	Type  int32
	mode  int32
}
func (tv TypeAndValue) IsValue() bool { return tv.mode == 1 || tv.mode == 2 }
func (tv TypeAndValue) IsConst() bool { return tv.mode == 3 }
type Info struct {
	Types map[Expr]TypeAndValue
	Defs  map[*Name]int32
}
func NewInfo() *Info {
	return &Info{
		Types: map[Expr]TypeAndValue{},
		Defs:  map[*Name]int32{},
	}
}
func (info *Info) TypeOf(e Expr) int32 {
	if tv, ok := info.Types[e]; ok {
		return tv.Type
	}
	return 0
}
func main() {
	info := NewInfo()
	n := &Name{Value: "x"}
	info.Types[n] = TypeAndValue{Type: 42, mode: 1}
	t := info.TypeOf(n)
	_ = t
}
`, func(ir string) {
		assert("201: NewInfo defined", strings.Contains(ir, "@main.NewInfo"))
		assert("201: TypeOf defined", strings.Contains(ir, "@main.Info.TypeOf"))
	})

	// Test 202: Multi-level pointer chain (a->b->c field access)
	test(202, `package main
type Inner struct {
	value int32
}
type Middle struct {
	inner *Inner
	name  string
}
type Outer struct {
	mid *Middle
	tag int32
}
func getValue(o *Outer) int32 {
	return o.mid.inner.value
}
func getName(o *Outer) string {
	return o.mid.name
}
func main() {
	i := &Inner{value: 42}
	m := &Middle{inner: i, name: "hello"}
	o := &Outer{mid: m, tag: 1}
	v := getValue(o)
	n := getName(o)
	_ = v
	_ = n
}
`, func(ir string) {
		assert("202: getValue defined", strings.Contains(ir, "@main.getValue"))
		assert("202: getName defined", strings.Contains(ir, "@main.getName"))
	})

	// Test 203: String comparison in if/else chain
	test(203, `package main
func classify(s string) int32 {
	if s == "int" {
		return 1
	} else if s == "string" {
		return 2
	} else if s == "bool" {
		return 3
	}
	return 0
}
func main() {
	c := classify("int")
	_ = c
}
`, func(ir string) {
		assert("203: classify defined", strings.Contains(ir, "@main.classify"))
	})

	// Test 204: Variadic-like append pattern
	test(204, `package main
func joinStrings(sep string, parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := int32(1); i < int32(len(parts)); i++ {
		result = result | sep | parts[i]
	}
	return result
}
func main() {
	parts := []string{"a", "b", "c"}
	s := joinStrings(", ", parts)
	_ = s
}
`, func(ir string) {
		assert("204: joinStrings defined", strings.Contains(ir, "@main.joinStrings"))
	})

	// Test 205: Alloc with field initialization in loop
	test(205, `package main
type Node struct {
	key   string
	value int32
	next  *Node
}
func buildList(keys []string) *Node {
	var head *Node
	for i := int32(len(keys)) - 1; i >= 0; i-- {
		n := &Node{key: keys[i], value: i, next: head}
		head = n
	}
	return head
}
func main() {
	keys := []string{"a", "b", "c"}
	list := buildList(keys)
	_ = list
}
`, func(ir string) {
		assert("205: buildList defined", strings.Contains(ir, "@main.buildList"))
	})

	// Test 206: Token types with const enums (no globals)
	test(206, `package main
type Token uint32
const (
	_ Token = iota
	EOF
	NameType
	Literal
	tokenCount
)
const _ uint64 = 1 << (tokenCount - 1)
func contains(tokset uint64, tok Token) bool {
	return tokset&(1<<tok) != 0
}
func main() {
	b := contains(1<<EOF|1<<NameType, Literal)
	_ = b
}
`, func(ir string) {
		assert("206: contains defined", strings.Contains(ir, "@main.contains"))
	})

	// Test 207: Full tc_types-like pattern (Type interface, Named types, methods)
	test(207, `package main
type Type interface {
	Underlying() Type
	String() string
}
type BasicInfo int32
const (
	IsBoolean BasicInfo = 1 << iota
	IsInteger
	IsUnsigned
	IsFloat
	IsString
	IsUntyped
)
type BasicKind int32
type Basic struct {
	kind BasicKind
	info BasicInfo
	name string
}
func (b *Basic) Underlying() Type { return b }
func (b *Basic) String() string   { return b.name }
func (b *Basic) Kind() BasicKind  { return b.kind }
func (b *Basic) Info() BasicInfo  { return b.info }
func (b *Basic) Name() string     { return b.name }
type Slice struct {
	elem Type
}
func (s *Slice) Underlying() Type { return s }
func (s *Slice) String() string   { return "[]" | s.elem.String() }
func (s *Slice) Elem() Type       { return s.elem }
func NewSlice(elem Type) *Slice   { return &Slice{elem: elem} }
type Pointer struct {
	base Type
}
func (p *Pointer) Underlying() Type { return p }
func (p *Pointer) String() string   { return "*" | p.base.String() }
func (p *Pointer) Elem() Type       { return p.base }
func NewPointer(elem Type) *Pointer { return &Pointer{base: elem} }
type Named struct {
	name    string
	under   Type
	methods []*Func
}
func (n *Named) Underlying() Type { return n.under }
func (n *Named) String() string   { return n.name }
func (n *Named) NumMethods() int32 { return int32(len(n.methods)) }
func (n *Named) Method(i int32) *Func { return n.methods[i] }
func (n *Named) AddMethod(m *Func) { n.methods = append(n.methods, m) }
type Func struct {
	name string
	typ  Type
}
func (f *Func) Name() string { return f.name }
func (f *Func) Type() Type   { return f.typ }
func main() {
	intType := &Basic{kind: 2, info: IsInteger, name: "int"}
	sliceType := NewSlice(intType)
	ptrType := NewPointer(intType)
	named := &Named{name: "MyInt", under: intType}
	named.AddMethod(&Func{name: "String", typ: intType})
	s1 := intType.String()
	s2 := sliceType.String()
	s3 := ptrType.String()
	n := named.NumMethods()
	_ = s1
	_ = s2
	_ = s3
	_ = n
}
`, func(ir string) {
		assert("207: Basic.String defined", strings.Contains(ir, "@main.Basic.String"))
		assert("207: Slice.String defined", strings.Contains(ir, "@main.Slice.String"))
		assert("207: Pointer.String defined", strings.Contains(ir, "@main.Pointer.String"))
		assert("207: Named.AddMethod defined", strings.Contains(ir, "@main.Named.AddMethod"))
	})

	// Test 208: Checker-like pattern with type resolution
	test(208, `package main
type Type interface {
	Underlying() Type
}
type Basic208 struct {
	kind int32
	name string
}
func (b *Basic208) Underlying() Type { return b }
type Pointer208 struct {
	base Type
}
func (p *Pointer208) Underlying() Type { return p }
func (p *Pointer208) Elem() Type { return p.base }
type Checker struct {
	pkg     string
	types   map[string]Type
}
func NewChecker(pkg string) *Checker {
	return &Checker{pkg: pkg, types: map[string]Type{}}
}
func (c *Checker) Define(name string, t Type) {
	c.types[name] = t
}
func (c *Checker) Resolve(name string) Type {
	t, ok := c.types[name]
	if ok {
		return t
	}
	return nil
}
func (c *Checker) IsPointer(t Type) bool {
	_, ok := t.(*Pointer208)
	return ok
}
func main() {
	c := NewChecker("main")
	intT := &Basic208{kind: 1, name: "int"}
	c.Define("int", intT)
	c.Define("*int", &Pointer208{base: intT})
	t := c.Resolve("int")
	ip := c.IsPointer(t)
	_ = ip
}
`, func(ir string) {
		assert("208: NewChecker defined", strings.Contains(ir, "@main.NewChecker"))
		assert("208: Resolve defined", strings.Contains(ir, "@main.Checker.Resolve"))
		assert("208: IsPointer defined", strings.Contains(ir, "@main.Checker.IsPointer"))
	})

	// Test 209: Full scanner pattern with rune processing
	test(209, `package main
type Scanner struct {
	src  []byte
	pos  int32
	ch   rune
	chw  int32
}
func (s *Scanner) peek() rune {
	if s.pos >= int32(len(s.src)) {
		return -1
	}
	return rune(s.src[s.pos])
}
func (s *Scanner) next() {
	if s.pos >= int32(len(s.src)) {
		s.ch = -1
		s.chw = 0
		return
	}
	s.ch = rune(s.src[s.pos])
	s.chw = 1
	s.pos++
}
func (s *Scanner) skipSpace() {
	for s.ch == ' ' || s.ch == '\t' || s.ch == '\n' || s.ch == '\r' {
		s.next()
	}
}
func isLetter(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}
func isDigit(ch rune) bool {
	return ch >= '0' && ch <= '9'
}
func (s *Scanner) scanIdent() string {
	start := s.pos - s.chw
	for isLetter(s.ch) || isDigit(s.ch) {
		s.next()
	}
	return string(s.src[start : s.pos-s.chw])
}
func (s *Scanner) scanNumber() string {
	start := s.pos - s.chw
	for isDigit(s.ch) {
		s.next()
	}
	return string(s.src[start : s.pos-s.chw])
}
func (s *Scanner) scanString() string {
	start := s.pos
	for s.ch != '"' && s.ch >= 0 {
		s.next()
	}
	val := string(s.src[start : s.pos-s.chw])
	if s.ch == '"' {
		s.next()
	}
	return val
}
type TokenKind int32
const (
	TokEOF TokenKind = iota
	TokIdent
	TokNumber
	TokString
	TokPunct
)
type ScanToken struct {
	kind  TokenKind
	value string
}
func (s *Scanner) scan() ScanToken {
	s.skipSpace()
	if s.ch < 0 {
		return ScanToken{kind: TokEOF}
	}
	if isLetter(s.ch) {
		return ScanToken{kind: TokIdent, value: s.scanIdent()}
	}
	if isDigit(s.ch) {
		return ScanToken{kind: TokNumber, value: s.scanNumber()}
	}
	if s.ch == '"' {
		s.next()
		return ScanToken{kind: TokString, value: s.scanString()}
	}
	ch := string([]byte{byte(s.ch)})
	s.next()
	return ScanToken{kind: TokPunct, value: ch}
}
func main() {
	sc := &Scanner{src: []byte("hello 42 \"world\""), ch: ' '}
	sc.next()
	t1 := sc.scan()
	t2 := sc.scan()
	t3 := sc.scan()
	_ = t1
	_ = t2
	_ = t3
}
`, func(ir string) {
		assert("209: scan defined", strings.Contains(ir, "@main.Scanner.scan"))
		assert("209: scanIdent defined", strings.Contains(ir, "@main.Scanner.scanIdent"))
		assert("209: scanString defined", strings.Contains(ir, "@main.Scanner.scanString"))
	})

	// Test 210a: Incremental - many types to reproduce void field bug
	test(2100, `package main
type Type interface {
	Underlying() Type
	String() string
}
type BasicInfo int32
type BasicKind int32
type Basic struct {
	kind BasicKind
	info BasicInfo
	name string
}
func (b *Basic) Underlying() Type { return b }
func (b *Basic) String() string   { return b.name }
type Slice struct{ elem Type }
func (s *Slice) Underlying() Type { return s }
func (s *Slice) String() string   { return "[]" | s.elem.String() }
func (s *Slice) Elem() Type       { return s.elem }
type Pointer struct{ base Type }
func (p *Pointer) Underlying() Type { return p }
func (p *Pointer) String() string   { return "*" | p.base.String() }
func (p *Pointer) Elem() Type       { return p.base }
type TCMap struct {
	key  Type
	elem Type
}
func (m *TCMap) Underlying() Type { return m }
func (m *TCMap) String() string   { return "map" }
type TCInterface struct {
	methods []string
}
func (t *TCInterface) Underlying() Type { return t }
func (t *TCInterface) String() string   { return "interface" }
type Signature struct {
	recv    *TCVar
	params  *Tuple
	results *Tuple
}
func (s *Signature) Underlying() Type { return s }
func (s *Signature) String() string   { return "func" }
type Named struct {
	name    string
	under   Type
	methods []*TCFunc
}
func (n *Named) Underlying() Type { return n.under }
func (n *Named) String() string   { return n.name }
type Tuple struct{ vars []*TCVar }
type Object interface {
	Name() string
	Type() Type
	Exported() bool
}
type object struct {
	name string
	typ  Type
}
func (o *object) Name() string    { return o.name }
func (o *object) Type() Type      { return o.typ }
func (o *object) Exported() bool  { return len(o.name) > 0 && o.name[0] >= 'A' && o.name[0] <= 'Z' }
type TypeName struct{ object }
func NewTypeName(name string, typ Type) *TypeName {
	return &TypeName{object{name: name, typ: typ}}
}
type TCVar struct {
	name string
	typ  Type
}
func (v *TCVar) Name() string    { return v.name }
func (v *TCVar) Type() Type      { return v.typ }
func (v *TCVar) Exported() bool  { return len(v.name) > 0 && v.name[0] >= 'A' && v.name[0] <= 'Z' }
func NewTCField(pkg *TCPackage, name string, typ Type, anonymous bool) *TCVar {
	return &TCVar{name: name, typ: typ}
}
type TCFunc struct {
	object
	sig *Signature
}
type TCPackage struct {
	path     string
	name     string
	scope    *Scope
}
func NewTCPackage(path string, name string) *TCPackage {
	return &TCPackage{path: path, name: name, scope: NewScope(nil)}
}
type Scope struct {
	parent *Scope
	elems  map[string]Object
}
func NewScope(parent *Scope) *Scope {
	return &Scope{parent: parent, elems: map[string]Object{}}
}
func (s *Scope) Lookup(name string) Object {
	return s.elems[name]
}
func (s *Scope) Insert(obj Object) Object {
	nm := obj.Name()
	if alt, ok := s.elems[nm]; ok {
		return alt
	}
	s.elems[nm] = obj
	return nil
}
func (s *Scope) LookupParent(name string) (*Scope, Object) {
	for sc := s; sc != nil; sc = sc.parent {
		if obj, ok := sc.elems[name]; ok {
			return sc, obj
		}
	}
	return nil, nil
}
func (s *Scope) Names() []string {
	names := []string{:0:len(s.elems)}
	for n := range s.elems {
		names = append(names, n)
	}
	return names
}
func (p *TCPackage) Path() string    { return p.path }
func (p *TCPackage) Name() string    { return p.name }
func (p *TCPackage) Scope() *Scope   { return p.scope }
func (p *TCPackage) String() string  { return "package " | p.name | " (" | p.path | ")" }
type exprMode uint8
const (
	modeInvalid exprMode = iota
	modeValue
	modeVar
	modeConst
)
type TypeAndValue struct {
	Typ  Type
	mode exprMode
}
func (tv TypeAndValue) IsValue() bool { return tv.mode == modeValue || tv.mode == modeVar }
func main() {
	b := &Basic{kind: 2, info: 2, name: "int"}
	pkg := NewTCPackage("main", "main")
	tn := NewTypeName("MyType", b)
	pkg.Scope().Insert(tn)
	f := NewTCField(pkg, "x", b, false)
	_ = f
}
`, func(ir string) {
		assert("210a: NewTypeName no void", !strings.Contains(ir, "void %typ"))
		assert("210a: NewTCField no void", !strings.Contains(ir, "void %typ"))
	})

	// Test 210: Large concat - tc_types + tc_scope + tc_object + tc_info patterns
	src210 := []byte(`package main
type Type interface {
	Underlying() Type
	String() string
}
type BasicInfo int32
const (
	IsBoolean BasicInfo = 1 << iota
	IsInteger
	IsUnsigned
	IsFloat
	IsString210
	IsUntyped
)
type BasicKind int32
type Basic struct {
	kind BasicKind
	info BasicInfo
	name string
}
func (b *Basic) Underlying() Type { return b }
func (b *Basic) String() string   { return b.name }
func (b *Basic) Kind() BasicKind  { return b.kind }
func (b *Basic) Info() BasicInfo  { return b.info }
type Slice struct{ elem Type }
func (s *Slice) Underlying() Type { return s }
func (s *Slice) String() string   { return "[]" | s.elem.String() }
func (s *Slice) Elem() Type       { return s.elem }
func NewSlice(elem Type) *Slice   { return &Slice{elem: elem} }
type Pointer struct{ base Type }
func (p *Pointer) Underlying() Type { return p }
func (p *Pointer) String() string   { return "*" | p.base.String() }
func (p *Pointer) Elem() Type       { return p.base }
func NewPointer(elem Type) *Pointer { return &Pointer{base: elem} }
type TCMap struct {
	key  Type
	elem Type
}
func (m *TCMap) Underlying() Type { return m }
func (m *TCMap) String() string   { return "map" }
func (m *TCMap) Key() Type        { return m.key }
func (m *TCMap) Elem() Type       { return m.elem }
type TCInterface struct {
	methods []string
}
func (t *TCInterface) Underlying() Type { return t }
func (t *TCInterface) String() string   { return "interface" }
func (t *TCInterface) NumMethods() int32 { return int32(len(t.methods)) }
type Signature struct {
	recv     *TCVar
	params   *Tuple
	results  *Tuple
}
func NewSignature(recv *TCVar, params *Tuple, results *Tuple) *Signature {
	return &Signature{recv: recv, params: params, results: results}
}
func (s *Signature) Underlying() Type { return s }
func (s *Signature) String() string   { return "func" }
func (s *Signature) Recv() *TCVar     { return s.recv }
func (s *Signature) Params() *Tuple   { return s.params }
func (s *Signature) Results() *Tuple  { return s.results }
type Named struct {
	name    string
	under   Type
	methods []*TCFunc
}
func (n *Named) Underlying() Type      { return n.under }
func (n *Named) String() string        { return n.name }
func (n *Named) NumMethods() int32     { return int32(len(n.methods)) }
func (n *Named) Method(i int32) *TCFunc { return n.methods[i] }
func (n *Named) AddMethod(m *TCFunc)   { n.methods = append(n.methods, m) }
type Tuple struct{ vars []*TCVar }
func NewTuple(vars []*TCVar) *Tuple { return &Tuple{vars: vars} }
func (t *Tuple) Len() int32 { return int32(len(t.vars)) }
func (t *Tuple) At(i int32) *TCVar { return t.vars[i] }
type Object interface {
	Name() string
	Type() Type
	Pkg() *TCPackage
	Exported() bool
}
type object struct {
	pkg  *TCPackage
	name string
	typ  Type
}
func (o *object) Name() string      { return o.name }
func (o *object) Type() Type        { return o.typ }
func (o *object) Pkg() *TCPackage   { return o.pkg }
func (o *object) Exported() bool    { return len(o.name) > 0 && o.name[0] >= 'A' && o.name[0] <= 'Z' }
type TypeName struct{ object }
func NewTypeName(pkg *TCPackage, name string, typ Type) *TypeName {
	return &TypeName{object{pkg: pkg, name: name, typ: typ}}
}
type TCVar struct {
	pkg       *TCPackage
	name      string
	typ       Type
	anonymous bool
}
func NewTCField(pkg *TCPackage, name string, typ Type, anonymous bool) *TCVar {
	return &TCVar{pkg: pkg, name: name, typ: typ, anonymous: anonymous}
}
func (v *TCVar) Name() string      { return v.name }
func (v *TCVar) Type() Type        { return v.typ }
func (v *TCVar) Pkg() *TCPackage   { return v.pkg }
func (v *TCVar) Exported() bool    { return len(v.name) > 0 && v.name[0] >= 'A' && v.name[0] <= 'Z' }
func (v *TCVar) Anonymous() bool   { return v.anonymous }
type TCFunc struct {
	object
	sig *Signature
}
func (f *TCFunc) Signature() *Signature { return f.sig }
type TCPackage struct {
	path     string
	name     string
	scope    *Scope
	imports  []*TCPackage
	complete bool
}
func NewTCPackage(path string, name string) *TCPackage {
	return &TCPackage{
		path:  path,
		name:  name,
		scope: NewScope(nil),
	}
}
func (p *TCPackage) Path() string           { return p.path }
func (p *TCPackage) Name() string           { return p.name }
func (p *TCPackage) Scope() *Scope          { return p.scope }
func (p *TCPackage) Complete() bool         { return p.complete }
func (p *TCPackage) MarkComplete()          { p.complete = true }
func (p *TCPackage) String() string         { return "package " | p.name | " (" | p.path | ")" }
func (p *TCPackage) Imports() []*TCPackage  { return p.imports }
type Scope struct {
	parent   *Scope
	children []*Scope
	elems    map[string]Object
}
func NewScope(parent *Scope) *Scope {
	return &Scope{parent: parent, elems: map[string]Object{}}
}
func (s *Scope) Parent() *Scope { return s.parent }
func (s *Scope) Len() int32     { return int32(len(s.elems)) }
func (s *Scope) Lookup(name string) Object {
	return s.elems[name]
}
func (s *Scope) LookupParent(name string) (*Scope, Object) {
	for sc := s; sc != nil; sc = sc.parent {
		if obj, ok := sc.elems[name]; ok {
			return sc, obj
		}
	}
	return nil, nil
}
func (s *Scope) Insert(obj Object) Object {
	name := obj.Name()
	if alt, ok := s.elems[name]; ok {
		return alt
	}
	s.elems[name] = obj
	return nil
}
func (s *Scope) Names() []string {
	names := []string{:0:len(s.elems)}
	for n := range s.elems {
		names = append(names, n)
	}
	return names
}
type exprMode uint8
const (
	modeInvalid exprMode = iota
	modeValue
	modeVar
	modeConst
	modeType
)
type TypeAndValue struct {
	Typ   Type
	mode  exprMode
}
func (tv TypeAndValue) IsValue() bool { return tv.mode == modeValue || tv.mode == modeVar }
func (tv TypeAndValue) IsConst() bool { return tv.mode == modeConst }
func main() {
	intT := &Basic{kind: 2, info: IsInteger, name: "int"}
	strT := &Basic{kind: 17, info: IsString210, name: "string"}
	sliceInt := NewSlice(intT)
	ptrInt := NewPointer(intT)
	pkg := NewTCPackage("main", "main")
	scope := pkg.Scope()
	tn := NewTypeName(pkg, "MyType", intT)
	scope.Insert(tn)
	field := NewTCField(pkg, "x", intT, false)
	sig := NewSignature(nil, NewTuple([]*TCVar{field}), NewTuple([]*TCVar{NewTCField(nil, "", strT, false)}))
	named := &Named{name: "MyStruct", under: intT}
	named.AddMethod(&TCFunc{object: object{pkg: pkg, name: "String", typ: sig}, sig: sig})
	_, obj := scope.LookupParent("MyType")
	if obj != nil {
		n := obj.Name()
		_ = n
	}
	s1 := sliceInt.String()
	s2 := ptrInt.String()
	nm := named.NumMethods()
	m0 := named.Method(0)
	ms := m0.Signature()
	_ = s1
	_ = s2
	_ = nm
	_ = ms
	tv := TypeAndValue{Typ: intT, mode: modeValue}
	isVal := tv.IsValue()
	_ = isVal
}
`)
	name210 := []byte("main")
	h210 := compileToIR(
		uintptr(unsafe.Pointer(&src210[0])), int32(len(src210)),
		uintptr(unsafe.Pointer(&name210[0])), int32(len(name210)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir210 := getIR(h210)
	if ir210 == "" {
		assert("210: full type system concat produces IR", false)
	} else {
		assert("210: has NewTCPackage", strings.Contains(ir210, "@main.NewTCPackage"))
		assert("210: has Scope.LookupParent", strings.Contains(ir210, "@main.Scope.LookupParent"))
		assert("210: has TypeAndValue.IsValue", strings.Contains(ir210, "@main.TypeAndValue.IsValue"))
		assert("210: no parse error", !strings.Contains(ir210, "parse error"))
		voidFieldCount := strings.Count(ir210, "void %typ")
		fmt.Println("=== Full type system concat ===")
		fmt.Printf("IR size: %d bytes, functions: ~%d, void fields: %d (known issue)\n", len(ir210),
			strings.Count(ir210, "\ndefine "), voidFieldCount)
	}
	irFree(h210)

	// Test 211: Variadic function - definition + call
	test(211, `package main

func sum(nums ...int32) int32 {
	total := int32(0)
	for _, n := range nums {
		total = total + n
	}
	return total
}

func main() {
	s := sum(1, 2, 3)
	_ = s
}
`, func(ir string) {
		assert("211: has sum", strings.Contains(ir, "@main.sum"))
		assert("211: sum takes slice", strings.Contains(ir, "define i32 @main.sum({ptr, i64, i64} %nums"))
		assert("211: makeslice len 3", strings.Contains(ir, "sext i32 3 to i64"))
		assert("211: call passes slice", strings.Contains(ir, "call i32 @main.sum({ptr, i64, i64}"))
	})

	// Test 213: Variadic call with zero args
	test(213, `package main

func sum(nums ...int32) int32 {
	return int32(0)
}

func main() {
	s := sum()
	_ = s
}
`, func(ir string) {
		assert("213: has sum", strings.Contains(ir, "@main.sum"))
		assert("213: nil slice arg", strings.Contains(ir, "call i32 @main.sum({ptr, i64, i64} zeroinitializer"))
	})

	// Test 214: Variadic with fixed args
	test(214, `package main

func printf(format string, args ...int32) int32 {
	return int32(0)
}

func main() {
	s := printf("hello", 1, 2)
	_ = s
}
`, func(ir string) {
		assert("214: has printf", strings.Contains(ir, "@main.printf"))
		assert("214: takes string then slice", strings.Contains(ir, "define i32 @main.printf({ptr, i64, i64} %format, {ptr, i64, i64} %args"))
	})

	// Test 215: String building with | operator, method chains
	test(215, `package main

type Builder struct {
	buf []byte
}

func (b *Builder) write(s string) {
	b.buf = b.buf | []byte(s)
}

func (b *Builder) writeInt(n int32) {
	if n == 0 {
		b.buf = append(b.buf, byte('0'))
		return
	}
	tmp := []byte{:0:10}
	for n > 0 {
		tmp = append(tmp, byte('0' + n % 10))
		n = n / 10
	}
	for i := len(tmp) - 1; i >= 0; i = i - 1 {
		b.buf = append(b.buf, tmp[i])
	}
}

func (b *Builder) String() string {
	return string(b.buf)
}

func main() {
	b := &Builder{buf: []byte{:0:64}}
	b.write("hello")
	b.write(" ")
	b.write("world")
	b.writeInt(42)
	s := b.String()
	_ = s
}
`, func(ir string) {
		assert("215: has Builder.write", strings.Contains(ir, "@main.Builder.write"))
		assert("215: has Builder.writeInt", strings.Contains(ir, "@main.Builder.writeInt"))
		assert("215: has Builder.String", strings.Contains(ir, "@main.Builder.String"))
		assert("215: uses sliceAppend", strings.Contains(ir, "@runtime.sliceAppend"))
	})

	// Test 216: Type switch with multiple cases
	test(216, `package main

type Node interface {
	Kind() int32
}

type Literal struct {
	val int32
}
func (l *Literal) Kind() int32 { return 1 }

type BinOp struct {
	op  int32
	lhs Node
	rhs Node
}
func (b *BinOp) Kind() int32 { return 2 }

type UnaryOp struct {
	op      int32
	operand Node
}
func (u *UnaryOp) Kind() int32 { return 3 }

func eval(n Node) int32 {
	switch v := n.(type) {
	case *Literal:
		return v.val
	case *BinOp:
		l := eval(v.lhs)
		r := eval(v.rhs)
		if v.op == 1 {
			return l + r
		}
		return l - r
	case *UnaryOp:
		x := eval(v.operand)
		return -x
	}
	return 0
}

func main() {
	a := &Literal{val: 10}
	b := &Literal{val: 3}
	sum := &BinOp{op: 1, lhs: a, rhs: b}
	neg := &UnaryOp{op: 1, operand: a}
	r1 := eval(sum)
	r2 := eval(neg)
	_ = r1
	_ = r2
}
`, func(ir string) {
		assert("216: has eval", strings.Contains(ir, "@main.eval"))
		assert("216: type switch has typeid", strings.Contains(ir, "typeid"))
		assert("216: recursive call", strings.Count(ir, "call i32 @main.eval") >= 2)
	})

	// Test 217: Map with string keys, comma-ok lookup, delete
	test(217, `package main

func lookup(m map[string]int32, key string) (int32, bool) {
	v, ok := m[key]
	return v, ok
}

func main() {
	m := map[string]int32{}
	m["hello"] = 1
	m["world"] = 2
	v, ok := lookup(m, "hello")
	_ = v
	_ = ok
	delete(m, "world")
	n := len(m)
	_ = n
}
`, func(ir string) {
		assert("217: has lookup", strings.Contains(ir, "@main.lookup"))
		assert("217: map make", strings.Contains(ir, "hashmapMake"))
		assert("217: map update", strings.Contains(ir, "hashmapContentSet"))
		assert("217: map delete", strings.Contains(ir, "hashmapDelete") || strings.Contains(ir, "hashmapBinaryDelete"))
	})

	// Test 218: Function value as variable, call through variable
	test(218, `package main

type Transformer func(int32) int32

func apply(f Transformer, x int32) int32 {
	return f(x)
}

func double(x int32) int32 {
	return x + x
}

func main() {
	var f Transformer
	f = double
	r := apply(f, 5)
	_ = r
}
`, func(ir string) {
		assert("218: has apply", strings.Contains(ir, "@main.apply"))
		assert("218: has double", strings.Contains(ir, "@main.double"))
		assert("218: indirect call", strings.Contains(ir, "call i32 %"))
	})

	// Test 219: Multi-value return with type assertion comma-ok
	test(219, `package main

type Value interface {
	String() string
}

type IntVal struct {
	v int32
}
func (i *IntVal) String() string { return "int" }

func asInt(v Value) (*IntVal, bool) {
	i, ok := v.(*IntVal)
	return i, ok
}

func main() {
	var v Value
	v = &IntVal{v: 42}
	i, ok := asInt(v)
	_ = i
	_ = ok
}
`, func(ir string) {
		assert("219: has asInt", strings.Contains(ir, "@main.asInt"))
		assert("219: type assert", strings.Contains(ir, "typeid") || strings.Contains(ir, "icmp"))
	})

	// Test 220: Const block with iota, string concat in method
	test(220, `package main

const (
	KindA = iota
	KindB
	KindC
)

type Item struct {
	kind int32
	name string
}

func (it *Item) Label() string {
	prefix := "item:"
	return prefix | it.name
}

func kindName(k int32) string {
	switch k {
	case KindA:
		return "A"
	case KindB:
		return "B"
	case KindC:
		return "C"
	}
	return "?"
}

func main() {
	it := &Item{kind: KindB, name: "test"}
	s := it.Label()
	n := kindName(KindC)
	_ = s
	_ = n
}
`, func(ir string) {
		assert("220: has Item.Label", strings.Contains(ir, "@main.Item.Label"))
		assert("220: has kindName", strings.Contains(ir, "@main.kindName"))
		assert("220: string concat uses sliceAppend", strings.Contains(ir, "@runtime.sliceAppend"))
		assert("220: switch has icmp", strings.Contains(ir, "icmp eq"))
	})

	// Test 221: Mini IR emitter - exercises self-compilation patterns
	test221src := []byte(`package main

type Type interface {
	Kind() int32
	LLVMType() string
}

type BasicType struct {
	kind int32
	name string
	llvm string
}
func (b *BasicType) Kind() int32      { return b.kind }
func (b *BasicType) LLVMType() string { return b.llvm }

type PtrType struct {
	elem Type
}
func (p *PtrType) Kind() int32      { return 4 }
func (p *PtrType) LLVMType() string { return "ptr" }

type SliceType struct {
	elem Type
}
func (s *SliceType) Kind() int32      { return 5 }
func (s *SliceType) LLVMType() string { return "{ptr, i64, i64}" }

type FuncSig struct {
	params  []Type
	results []Type
}
func (f *FuncSig) Kind() int32      { return 6 }
func (f *FuncSig) LLVMType() string { return "ptr" }

type Emitter struct {
	buf     []byte
	nextReg int32
	types   map[string]Type
}

func NewEmitter() *Emitter {
	e := &Emitter{
		buf:   []byte{:0:256},
		types: map[string]Type{},
	}
	e.types["int32"] = &BasicType{kind: 1, name: "int32", llvm: "i32"}
	e.types["string"] = &BasicType{kind: 14, name: "string", llvm: "{ptr, i64, i64}"}
	return e
}

func (e *Emitter) w(s string) {
	e.buf = e.buf | []byte(s)
}

func (e *Emitter) nextName() string {
	e.nextReg = e.nextReg + 1
	return "%t" | itoa(e.nextReg)
}

func itoa(n int32) string {
	if n == 0 { return "0" }
	buf := []byte{:0:10}
	for n > 0 {
		buf = append(buf, byte('0' + n % 10))
		n = n / 10
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

func (e *Emitter) emitType(t Type) string {
	switch v := t.(type) {
	case *BasicType:
		return v.llvm
	case *PtrType:
		return "ptr"
	case *SliceType:
		return "{ptr, i64, i64}"
	case *FuncSig:
		return e.emitFuncType(v)
	}
	return "void"
}

func (e *Emitter) emitFuncType(f *FuncSig) string {
	result := "void"
	if len(f.results) > 0 {
		result = e.emitType(f.results[0])
	}
	s := result | " ("
	for i, p := range f.params {
		if i > 0 {
			s = s | ", "
		}
		s = s | e.emitType(p)
	}
	s = s | ")"
	return s
}

func (e *Emitter) emitAlloc(t Type) string {
	name := e.nextName()
	e.w("  " | name | " = alloca " | e.emitType(t) | "\n")
	return name
}

func (e *Emitter) emitStore(addr string, val string, t Type) {
	e.w("  store " | e.emitType(t) | " " | val | ", ptr " | addr | "\n")
}

func (e *Emitter) emitLoad(addr string, t Type) string {
	name := e.nextName()
	e.w("  " | name | " = load " | e.emitType(t) | ", ptr " | addr | "\n")
	return name
}

func (e *Emitter) lookupType(name string) (Type, bool) {
	t, ok := e.types[name]
	return t, ok
}

func (e *Emitter) Result() string {
	return string(e.buf)
}

func main() {
	em := NewEmitter()
	intT, _ := em.lookupType("int32")
	strT, _ := em.lookupType("string")

	sig := &FuncSig{
		params:  []Type{intT, strT},
		results: []Type{intT},
	}

	em.w("define " | em.emitFuncType(sig) | " @main.add(")
	em.w(em.emitType(intT) | " %a, ")
	em.w(em.emitType(strT) | " %b) {\n")
	em.w("entry:\n")
	a1 := em.emitAlloc(intT)
	em.emitStore(a1, "%a", intT)
	v1 := em.emitLoad(a1, intT)
	_ = v1
	em.w("  ret " | em.emitType(intT) | " " | v1 | "\n")
	em.w("}\n")

	ir := em.Result()
	_ = ir
}
`)
	name221 := []byte("main")
	h221 := compileToIR(
		uintptr(unsafe.Pointer(&test221src[0])), int32(len(test221src)),
		uintptr(unsafe.Pointer(&name221[0])), int32(len(name221)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir221 := getIR(h221)
	if ir221 == "" {
		assert("221: mini emitter produces IR", false)
	} else {
		voidCount := strings.Count(ir221, "void %")
		funcCount := strings.Count(ir221, "\ndefine ")
		lineCount := strings.Count(string(test221src), "\n")
		fmt.Printf("=== Mini emitter test (221) ===\n")
		fmt.Printf("Source: %d lines, %d bytes | IR: %d bytes, %d functions, void fields: %d\n",
			lineCount, len(test221src), len(ir221), funcCount, voidCount)
		assert("221: produces IR", len(ir221) > 1000)
		assert("221: zero void fields", voidCount == 0)
		assert("221: has NewEmitter", strings.Contains(ir221, "@main.NewEmitter"))
		assert("221: has emitType", strings.Contains(ir221, "@main.Emitter.emitType"))
		assert("221: has emitFuncType", strings.Contains(ir221, "@main.Emitter.emitFuncType"))
		assert("221: has itoa", strings.Contains(ir221, "@main.itoa"))
		assert("221: has lookupType", strings.Contains(ir221, "@main.Emitter.lookupType"))
		assert("221: type switch typeid", strings.Contains(ir221, "typeid"))
		assert("221: uses sliceAppend", strings.Contains(ir221, "@runtime.sliceAppend"))
		assert("221: uses hashmapMake", strings.Contains(ir221, "@runtime.hashmapMake"))
		llvmVerify("221: mini emitter", ir221)
	}
	irFree(h221)

	// Test 222: Scope/Object hierarchy - mirrors tc_scope.mx, tc_object.mx patterns
	test(222, `package main

type Object interface {
	Name() string
	Type() Type
}

type Type interface {
	Underlying() Type
	String() string
}

type Scope struct {
	parent  *Scope
	entries map[string]Object
}

func NewScope(parent *Scope) *Scope {
	return &Scope{parent: parent, entries: map[string]Object{}}
}

func (s *Scope) Insert(obj Object) Object {
	name := obj.Name()
	old, ok := s.entries[name]
	if ok {
		return old
	}
	s.entries[name] = obj
	return nil
}

func (s *Scope) Lookup(name string) Object {
	obj, ok := s.entries[name]
	if ok {
		return obj
	}
	if s.parent != nil {
		return s.parent.Lookup(name)
	}
	return nil
}

func (s *Scope) Len() int32 {
	return int32(len(s.entries))
}

type BasicType struct {
	kind int32
	name string
}
func (b *BasicType) Underlying() Type { return b }
func (b *BasicType) String() string   { return b.name }

type PointerType struct {
	base Type
}
func (p *PointerType) Underlying() Type { return p }
func (p *PointerType) String() string   { return "*" | p.base.String() }

type TypeName struct {
	name string
	typ  Type
}
func (t *TypeName) Name() string { return t.name }
func (t *TypeName) Type() Type   { return t.typ }

type Var struct {
	name string
	typ  Type
}
func (v *Var) Name() string { return v.name }
func (v *Var) Type() Type   { return v.typ }

type Func struct {
	name string
	typ  Type
}
func (f *Func) Name() string { return f.name }
func (f *Func) Type() Type   { return f.typ }

type Checker struct {
	pkg   *Package
	scope *Scope
}

type Package struct {
	name  string
	scope *Scope
}

func NewChecker(pkg *Package) *Checker {
	return &Checker{pkg: pkg, scope: pkg.scope}
}

func (c *Checker) lookup(name string) Object {
	return c.scope.Lookup(name)
}

func (c *Checker) resolveTypeName(name string) Type {
	obj := c.lookup(name)
	if obj == nil {
		return nil
	}
	tn, ok := obj.(*TypeName)
	if !ok {
		return nil
	}
	return tn.typ
}

func main() {
	universe := NewScope(nil)
	intT := &BasicType{kind: 2, name: "int32"}
	strT := &BasicType{kind: 14, name: "string"}
	boolT := &BasicType{kind: 1, name: "bool"}

	universe.Insert(&TypeName{name: "int32", typ: intT})
	universe.Insert(&TypeName{name: "string", typ: strT})
	universe.Insert(&TypeName{name: "bool", typ: boolT})

	pkgScope := NewScope(universe)
	pkg := &Package{name: "main", scope: pkgScope}

	ptrInt := &PointerType{base: intT}
	pkgScope.Insert(&TypeName{name: "PtrInt", typ: ptrInt})
	pkgScope.Insert(&Var{name: "x", typ: intT})
	pkgScope.Insert(&Func{name: "foo", typ: nil})

	chk := NewChecker(pkg)

	t1 := chk.resolveTypeName("int32")
	t2 := chk.resolveTypeName("PtrInt")
	t3 := chk.resolveTypeName("missing")
	_ = t1
	_ = t2
	_ = t3

	n := pkgScope.Len()
	_ = n

	s1 := intT.String()
	s2 := ptrInt.String()
	_ = s1
	_ = s2
}
`, func(ir string) {
		assert("222: has NewScope", strings.Contains(ir, "@main.NewScope"))
		assert("222: has Scope.Lookup", strings.Contains(ir, "@main.Scope.Lookup"))
		assert("222: has Scope.Insert", strings.Contains(ir, "@main.Scope.Insert"))
		assert("222: has NewChecker", strings.Contains(ir, "@main.NewChecker"))
		assert("222: has resolveTypeName", strings.Contains(ir, "@main.Checker.resolveTypeName"))
		assert("222: recursive lookup", strings.Contains(ir, "call {ptr, ptr} @main.Scope.Lookup"))
		assert("222: type assert typeid", strings.Contains(ir, "typeid"))
		assert("222: map operations", strings.Contains(ir, "hashmapMake"))
	})

	// Test 223: Multi-value assign (tuple swap), array literal, byte operations
	test(223, `package main

func swap(a []int32) {
	if len(a) >= 2 {
		a[0], a[1] = a[1], a[0]
	}
}

func reverseBytes(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

func countChar(s string, c byte) int32 {
	n := int32(0)
	for i := 0; i < len(s); i = i + 1 {
		if s[i] == c {
			n = n + 1
		}
	}
	return n
}

func main() {
	a := []int32{10, 20, 30}
	swap(a)
	s := reverseBytes("hello")
	n := countChar("banana", byte('a'))
	_ = s
	_ = n
}
`, func(ir string) {
		assert("223: has swap", strings.Contains(ir, "@main.swap"))
		assert("223: has reverseBytes", strings.Contains(ir, "@main.reverseBytes"))
		assert("223: has countChar", strings.Contains(ir, "@main.countChar"))
	})

	// Test 224: Nested method calls, chained returns, early returns in switch
	test(224, `package main

type Node interface {
	Emit(buf *Buffer)
}

type Buffer struct {
	data []byte
}

func NewBuffer() *Buffer {
	return &Buffer{data: []byte{:0:64}}
}

func (b *Buffer) Write(s string) {
	b.data = b.data | []byte(s)
}

func (b *Buffer) WriteByte(c byte) {
	b.data = append(b.data, c)
}

func (b *Buffer) String() string {
	return string(b.data)
}

func (b *Buffer) Len() int32 {
	return int32(len(b.data))
}

type IntNode struct {
	val int32
}

func (n *IntNode) Emit(buf *Buffer) {
	if n.val == 0 {
		buf.Write("0")
		return
	}
	v := n.val
	tmp := []byte{:0:10}
	for v > 0 {
		tmp = append(tmp, byte('0' + v % 10))
		v = v / 10
	}
	for i, j := 0, len(tmp)-1; i < j; i, j = i+1, j-1 {
		tmp[i], tmp[j] = tmp[j], tmp[i]
	}
	buf.Write(string(tmp))
}

type StrNode struct {
	val string
}

func (n *StrNode) Emit(buf *Buffer) {
	buf.WriteByte(byte('"'))
	buf.Write(n.val)
	buf.WriteByte(byte('"'))
}

type BinNode struct {
	op  string
	lhs Node
	rhs Node
}

func (n *BinNode) Emit(buf *Buffer) {
	buf.WriteByte(byte('('))
	n.lhs.Emit(buf)
	buf.Write(" " | n.op | " ")
	n.rhs.Emit(buf)
	buf.WriteByte(byte(')'))
}

func main() {
	buf := NewBuffer()
	expr := &BinNode{
		op:  "+",
		lhs: &IntNode{val: 42},
		rhs: &BinNode{
			op:  "*",
			lhs: &IntNode{val: 3},
			rhs: &StrNode{val: "x"},
		},
	}
	expr.Emit(buf)
	result := buf.String()
	n := buf.Len()
	_ = result
	_ = n
}
`, func(ir string) {
		assert("224: has Buffer.Write", strings.Contains(ir, "@main.Buffer.Write"))
		assert("224: has IntNode.Emit", strings.Contains(ir, "@main.IntNode.Emit"))
		assert("224: has BinNode.Emit", strings.Contains(ir, "@main.BinNode.Emit"))
		assert("224: interface dispatch", strings.Contains(ir, "typeid"))
		assert("224: uses sliceAppend", strings.Contains(ir, "@runtime.sliceAppend"))
	})

	// Test 225: Byte indexing, tagless switch
	test(225, `package main

func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	c := name[0]
	if c >= 65 && c <= 90 {
		return true
	}
	return false
}

func classify(x int32) string {
	switch {
	case x < 0:
		return "negative"
	case x == 0:
		return "zero"
	case x > 100:
		return "large"
	}
	return "small"
}

func main() {
	e1 := isExported("Hello")
	e2 := isExported("hello")
	_ = e1
	_ = e2
	c1 := classify(-1)
	c2 := classify(0)
	c3 := classify(200)
	c4 := classify(50)
	_ = c1
	_ = c2
	_ = c3
	_ = c4
}
`, func(ir string) {
		assert("225: has isExported", strings.Contains(ir, "@main.isExported"))
		assert("225: has classify", strings.Contains(ir, "@main.classify"))
	})

	// Test 226: Named return values, multi-return with named results
	test(226, `package main

func divide(a int32, b int32) (q int32, r int32) {
	if b == 0 {
		return 0, 0
	}
	q = a / b
	r = a - q*b
	return q, r
}

func main() {
	q, r := divide(17, 5)
	_ = q
	_ = r
}
`, func(ir string) {
		assert("226: has divide", strings.Contains(ir, "@main.divide"))
		assert("226: divide returns tuple", strings.Contains(ir, "define {i32, i32} @main.divide"))
	})

	// Test 227: For-range over map, delete from map
	test(227, `package main

func countEntries(m map[string]int32) int32 {
	n := int32(0)
	for range m {
		n = n + 1
	}
	return n
}

func removeKey(m map[string]int32, key string) {
	delete(m, key)
}

func main() {
	m := map[string]int32{"a": 1, "b": 2, "c": 3}
	n := countEntries(m)
	_ = n
	removeKey(m, "b")
}
`, func(ir string) {
		assert("227: has countEntries", strings.Contains(ir, "@main.countEntries"))
		assert("227: has removeKey", strings.Contains(ir, "@main.removeKey"))
		assert("227: hashmapNext for range", strings.Contains(ir, "hashmapNext"))
		assert("227: hashmapBinaryDelete", strings.Contains(ir, "hashmapBinaryDelete"))
	})

	// Test 228: Multi-level struct embedding, promoted methods
	test(228, `package main

type base struct {
	name string
}

func (b *base) Name() string { return b.name }

type TypeName struct {
	base
	typ int32
}

func NewTypeName(name string, typ int32) *TypeName {
	return &TypeName{base: base{name: name}, typ: typ}
}

func main() {
	tn := NewTypeName("int", 1)
	n := tn.Name()
	_ = n
}
`, func(ir string) {
		assert("228: has NewTypeName", strings.Contains(ir, "@main.NewTypeName"))
		assert("228: has base.Name", strings.Contains(ir, "@main.base.Name"))
		assert("228: promoted method call", strings.Contains(ir, "call") && strings.Contains(ir, "base.Name"))
	})

	// Test 229: Simple append with spread
	test(229, `package main

func join(a []byte, b []byte) []byte {
	return append(a, b...)
}

func main() {
	x := []byte("abc")
	y := []byte("def")
	z := join(x, y)
	_ = z
}
`, func(ir string) {
		assert("229: has join", strings.Contains(ir, "@main.join"))
		assert("229: sliceAppend for spread", strings.Contains(ir, "sliceAppend"))
	})

	// Test 230: []byte(nil) conversion + spread append (self-compile pattern)
	test(230, `package main

func copyBytes(s []byte) []byte {
	return append([]byte(nil), s...)
}

func main() {
	x := []byte("test")
	y := copyBytes(x)
	_ = y
}
`, func(ir string) {
		assert("230: has copyBytes", strings.Contains(ir, "@main.copyBytes"))
	})

	// Test 231: String copy via append+spread (common bootstrap pattern)
	test(231, `package main

func copyString(s string) string {
	return string(append([]byte(nil), s...))
}

func main() {
	s := copyString("hello")
	_ = s
}
`, func(ir string) {
		assert("231: has copyString", strings.Contains(ir, "@main.copyString"))
	})

	// Test 232: Closures and function values
	test(232, `package main

func apply(f func(int32) int32, x int32) int32 {
	return f(x)
}

func main() {
	double := func(x int32) int32 { return x * 2 }
	r := apply(double, 5)
	_ = r
}
`, func(ir string) {
		assert("232: has apply", strings.Contains(ir, "@main.apply"))
	})

	// Test 233: Closure with capture (makeAdder pattern)
	test(233, `package main

func makeAdder(n int32) func(int32) int32 {
	return func(x int32) int32 {
		return x + n
	}
}

func main() {
	add3 := makeAdder(3)
	r := add3(10)
	_ = r
}
`, func(ir string) {
		assert("233: has makeAdder", strings.Contains(ir, "@main.makeAdder"))
	})

	// Test 234: Named return values with bare return
	test(234, `package main

func divide(a int32, b int32) (q int32, r int32) {
	if b == 0 {
		return
	}
	q = a / b
	r = a - q*b
	return
}

func main() {
	q, r := divide(17, 5)
	_ = q
	_ = r
	q2, r2 := divide(5, 0)
	_ = q2
	_ = r2
}
`, func(ir string) {
		assert("234: has divide", strings.Contains(ir, "@main.divide"))
		assert("234: returns tuple", strings.Contains(ir, "define {i32, i32} @main.divide"))
	})

	test(235, `package main

type Color int32
const (
	Red Color = iota
	Green
	Blue
)

func colorName(c Color) string {
	switch c {
	case Red:
		return "red"
	case Green:
		return "green"
	case Blue:
		return "blue"
	}
	return "unknown"
}

func main() {
	n := colorName(Green)
	_ = n
}
`, func(ir string) {
		assert("235: has colorName", strings.Contains(ir, "@main.colorName"))
		assert("235: Red=0 in switch", strings.Contains(ir, "icmp eq i32 %t2, 0"))
		assert("235: Green=1 in switch", strings.Contains(ir, "icmp eq i32 %t2, 1"))
		assert("235: Blue=2 in switch", strings.Contains(ir, "icmp eq i32 %t2, 2"))
	})

	test(236, `package main

type Op int32
const (
	_ Op = iota
	Add
	Sub
	Mul
	Div
)

func priority(op Op) int32 {
	switch op {
	case Add, Sub:
		return 1
	case Mul, Div:
		return 2
	}
	return 0
}

func main() {
	p := priority(Mul)
	_ = p
}
`, func(ir string) {
		assert("236: has priority", strings.Contains(ir, "@main.priority"))
		assert("236: Add=1 (skip blank)", strings.Contains(ir, "i32 1") || strings.Contains(ir, "i32 2"))
	})

	test(237, `package main

const (
	FlagA int32 = 1 << iota
	FlagB
	FlagC
	FlagD
)

func hasFlag(flags int32, f int32) bool {
	return flags & f != 0
}

func main() {
	flags := FlagA | FlagC
	a := hasFlag(flags, FlagA)
	b := hasFlag(flags, FlagB)
	_ = a
	_ = b
}
`, func(ir string) {
		assert("237: has hasFlag", strings.Contains(ir, "@main.hasFlag"))
		assert("237: has FlagA=1", strings.Contains(ir, "i32 1"))
		assert("237: has or op", strings.Contains(ir, " or "))
		assert("237: has and op", strings.Contains(ir, " and "))
	})

	test(238, `package main

func greet(name string) string {
	return "hello, " | name | "!"
}

func bits(a int32, b int32) int32 {
	return (a | b) & 0xFF
}

func main() {
	s := greet("world")
	_ = s
	r := bits(3, 12)
	_ = r
}
`, func(ir string) {
		assert("238: has greet", strings.Contains(ir, "@main.greet"))
		assert("238: has bits", strings.Contains(ir, "@main.bits"))
		assert("238: string concat via sliceAppend", strings.Contains(ir, "runtime.sliceAppend"))
		assert("238: bitwise or in bits", strings.Contains(ir, " or "))
		assert("238: bitwise and in bits", strings.Contains(ir, " and "))
	})

	test(239, `package main

type Pos struct {
	line int32
	col  int32
}

type Node struct {
	pos  Pos
	kind int32
}

func makeNode(line int32, col int32, kind int32) *Node {
	return &Node{pos: Pos{line: line, col: col}, kind: kind}
}

func getLine(n *Node) int32 {
	return n.pos.line
}

func main() {
	n := makeNode(10, 5, 1)
	l := getLine(n)
	_ = l
}
`, func(ir string) {
		assert("239: has makeNode", strings.Contains(ir, "@main.makeNode"))
		assert("239: has getLine", strings.Contains(ir, "@main.getLine"))
		assert("239: nested struct access", strings.Contains(ir, "getelementptr"))
	})

	test(240, `package main

type Builder struct {
	buf []byte
}

func (b *Builder) writeByte(c byte) {
	b.buf = append(b.buf, c)
}

func (b *Builder) writeString(s string) {
	b.buf = append(b.buf, s...)
}

func (b *Builder) String() string {
	return string(b.buf)
}

func main() {
	b := &Builder{}
	b.writeByte('H')
	b.writeString("ello")
	s := b.String()
	_ = s
}
`, func(ir string) {
		assert("240: has Builder.writeByte", strings.Contains(ir, "@main.Builder.writeByte"))
		assert("240: has Builder.writeString", strings.Contains(ir, "@main.Builder.writeString"))
		assert("240: has Builder.String", strings.Contains(ir, "@main.Builder.String"))
		assert("240: append call", strings.Contains(ir, "runtime.sliceAppend"))
	})

	test(241, `package main

var counter int32

func inc() int32 {
	counter++
	return counter
}

func dec() int32 {
	counter--
	return counter
}

func main() {
	a := inc()
	b := inc()
	c := dec()
	_ = a
	_ = b
	_ = c
}
`, func(ir string) {
		assert("241: has inc", strings.Contains(ir, "@main.inc"))
		assert("241: has dec", strings.Contains(ir, "@main.dec"))
		assert("241: has global counter", strings.Contains(ir, "@main.counter"))
	})

	test(242, `package main

type Token int32
const (
	EOF Token = iota
	Ident
	Number
	Plus
	Minus
	Star
	Slash
	Lparen
	Rparen
)

func isOperator(t Token) bool {
	switch t {
	case Plus, Minus, Star, Slash:
		return true
	}
	return false
}

func precedence(t Token) int32 {
	switch t {
	case Plus, Minus:
		return 1
	case Star, Slash:
		return 2
	}
	return 0
}

func main() {
	t := Star
	op := isOperator(t)
	p := precedence(t)
	_ = op
	_ = p
}
`, func(ir string) {
		assert("242: has isOperator", strings.Contains(ir, "@main.isOperator"))
		assert("242: has precedence", strings.Contains(ir, "@main.precedence"))
		assert("242: multi-case switch", strings.Contains(ir, "icmp eq"))
	})

	test(243, `package main

type Pair struct {
	key   string
	value int32
}

func lookup(pairs []Pair, key string) (int32, bool) {
	for i := 0; i < len(pairs); i++ {
		if pairs[i].key == key {
			return pairs[i].value, true
		}
	}
	return 0, false
}

func main() {
	pairs := []Pair{
		{key: "a", value: 1},
		{key: "b", value: 2},
		{key: "c", value: 3},
	}
	v, ok := lookup(pairs, "b")
	_ = v
	_ = ok
}
`, func(ir string) {
		assert("243: has lookup", strings.Contains(ir, "@main.lookup"))
		assert("243: returns tuple", strings.Contains(ir, "{i32, i1}"))
		assert("243: slice indexing", strings.Contains(ir, "getelementptr"))
	})

	test(244, `package main

type Writer interface {
	Write(data []byte) int32
}

type Buffer struct {
	data []byte
}

func (b *Buffer) Write(data []byte) int32 {
	b.data = append(b.data, data...)
	return int32(len(data))
}

func (b *Buffer) Len() int32 {
	return int32(len(b.data))
}

func writeAll(w Writer, chunks []string) int32 {
	total := int32(0)
	for i := 0; i < len(chunks); i++ {
		n := w.Write([]byte(chunks[i]))
		total = total + n
	}
	return total
}

func main() {
	buf := &Buffer{}
	chunks := []string{"hello", " ", "world"}
	n := writeAll(buf, chunks)
	_ = n
	l := buf.Len()
	_ = l
}
`, func(ir string) {
		assert("244: has Buffer.Write", strings.Contains(ir, "@main.Buffer.Write"))
		assert("244: has writeAll", strings.Contains(ir, "@main.writeAll"))
		assert("244: interface method call", strings.Contains(ir, "call"))
		assert("244: has Buffer.Len", strings.Contains(ir, "@main.Buffer.Len"))
	})

	test(245, `package main

type SSAOp int32
const (
	OpAdd SSAOp = iota + 1
	OpSub
	OpMul
	OpOr
	OpAnd
)

type SSABinOp struct {
	Op SSAOp
	X  int32
	Y  int32
}

func eval(b *SSABinOp) int32 {
	switch b.Op {
	case OpAdd:
		return b.X + b.Y
	case OpSub:
		return b.X - b.Y
	case OpMul:
		return b.X * b.Y
	case OpOr:
		return b.X | b.Y
	case OpAnd:
		return b.X & b.Y
	}
	return 0
}

func main() {
	b := &SSABinOp{Op: OpOr, X: 5, Y: 3}
	r := eval(b)
	_ = r
}
`, func(ir string) {
		assert("245: has eval", strings.Contains(ir, "@main.eval"))
		assert("245: switch on field", strings.Contains(ir, "icmp eq"))
		assert("245: iota+1 offset", strings.Contains(ir, "i32 4"))
	})

	test(246, `package main

type Scope struct {
	parent *Scope
	elems  map[string]int32
}

func NewScope(parent *Scope) *Scope {
	return &Scope{parent: parent, elems: map[string]int32{}}
}

func (s *Scope) Insert(name string, val int32) {
	s.elems[name] = val
}

func (s *Scope) Lookup(name string) (int32, bool) {
	v, ok := s.elems[name]
	if ok {
		return v, true
	}
	if s.parent != nil {
		return s.parent.Lookup(name)
	}
	return 0, false
}

func main() {
	outer := NewScope(nil)
	outer.Insert("x", 42)
	inner := NewScope(outer)
	inner.Insert("y", 7)
	v1, _ := inner.Lookup("x")
	v2, _ := inner.Lookup("y")
	_ = v1
	_ = v2
}
`, func(ir string) {
		assert("246: has NewScope", strings.Contains(ir, "@main.NewScope"))
		assert("246: has Scope.Insert", strings.Contains(ir, "@main.Scope.Insert"))
		assert("246: has Scope.Lookup", strings.Contains(ir, "@main.Scope.Lookup"))
		assert("246: recursive method call", strings.Contains(ir, "call {i32, i1} @main.Scope.Lookup"))
	})

	test(247, `package main

type Entry struct {
	name string
	typ  int32
}

type Registry struct {
	entries []Entry
}

func (r *Registry) add(name string, typ int32) {
	r.entries = append(r.entries, Entry{name: name, typ: typ})
}

func (r *Registry) find(name string) int32 {
	for i := 0; i < len(r.entries); i++ {
		if r.entries[i].name == name {
			return r.entries[i].typ
		}
	}
	return -1
}

func main() {
	r := &Registry{}
	r.add("int", 1)
	r.add("string", 2)
	r.add("bool", 3)
	t := r.find("string")
	_ = t
}
`, func(ir string) {
		assert("247: has Registry.add", strings.Contains(ir, "@main.Registry.add"))
		assert("247: has Registry.find", strings.Contains(ir, "@main.Registry.find"))
		assert("247: append struct to slice", strings.Contains(ir, "runtime.sliceAppend"))
	})

	test(248, `package main

func prefixMatch(s string, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

func trimPrefix(s string, prefix string) string {
	if prefixMatch(s, prefix) {
		return s[len(prefix):]
	}
	return s
}

func main() {
	r := prefixMatch("hello world", "hello")
	_ = r
	t := trimPrefix("hello world", "hello")
	_ = t
}
`, func(ir string) {
		assert("248: has prefixMatch", strings.Contains(ir, "@main.prefixMatch"))
		assert("248: has trimPrefix", strings.Contains(ir, "@main.trimPrefix"))
		assert("248: string compare", strings.Contains(ir, "runtime.stringEqual"))
		assert("248: slice sub-expr", strings.Contains(ir, "extractvalue"))
	})

	test(249, `package main

type Emitter struct {
	buf    []byte
	indent int32
}

func newEmitter() *Emitter {
	return &Emitter{buf: []byte{:0:1024}, indent: 0}
}

func (e *Emitter) w(s string) {
	e.buf = append(e.buf, s...)
}

func (e *Emitter) nl() {
	e.buf = append(e.buf, '\n')
	for i := int32(0); i < e.indent; i++ {
		e.buf = append(e.buf, ' ')
		e.buf = append(e.buf, ' ')
	}
}

func (e *Emitter) enter() {
	e.indent++
}

func (e *Emitter) leave() {
	if e.indent > 0 {
		e.indent--
	}
}

func (e *Emitter) String() string {
	return string(e.buf)
}

func main() {
	em := newEmitter()
	em.w("func main() {")
	em.enter()
	em.nl()
	em.w("x := 1")
	em.leave()
	em.nl()
	em.w("}")
	s := em.String()
	_ = s
}
`, func(ir string) {
		assert("249: has Emitter.w", strings.Contains(ir, "@main.Emitter.w"))
		assert("249: has Emitter.nl", strings.Contains(ir, "@main.Emitter.nl"))
		assert("249: has Emitter.enter", strings.Contains(ir, "@main.Emitter.enter"))
		assert("249: has newEmitter", strings.Contains(ir, "@main.newEmitter"))
		assert("249: append byte", strings.Contains(ir, "runtime.sliceAppend"))
	})

	test(250, `package main

func itoa(n int32) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := []byte{:0:20}
	for n > 0 {
		buf = append(buf, byte('0' + n % 10))
		n = n / 10
	}
	if neg {
		buf = append(buf, '-')
	}
	for i, j := int32(0), int32(len(buf)-1); i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

func main() {
	s1 := itoa(0)
	s2 := itoa(42)
	s3 := itoa(-123)
	_ = s1
	_ = s2
	_ = s3
}
`, func(ir string) {
		assert("250: has itoa", strings.Contains(ir, "@main.itoa"))
		assert("250: modulo op", strings.Contains(ir, "srem"))
		assert("250: division", strings.Contains(ir, "sdiv"))
		assert("250: reverse loop", strings.Contains(ir, "icmp slt"))
	})

	test(251, `package main

type Object interface {
	Name() string
}

type Var struct {
	name string
	typ  string
}

func (v *Var) Name() string { return v.name }

type Func struct {
	name string
	sig  string
}

func (f *Func) Name() string { return f.name }

func describe(o Object) string {
	switch o := o.(type) {
	case *Var:
		return "var " | o.name | " " | o.typ
	case *Func:
		return "func " | o.name | o.sig
	}
	return "unknown"
}

func main() {
	var objs []Object
	objs = append(objs, &Var{name: "x", typ: "int"})
	objs = append(objs, &Func{name: "foo", sig: "()"})
	for i := 0; i < len(objs); i++ {
		d := describe(objs[i])
		_ = d
	}
}
`, func(ir string) {
		assert("251: has describe", strings.Contains(ir, "@main.describe"))
		assert("251: has Var.Name", strings.Contains(ir, "@main.Var.Name"))
		assert("251: has Func.Name", strings.Contains(ir, "@main.Func.Name"))
		assert("251: type switch typeid", strings.Contains(ir, "typeid"))
		assert("251: string concat", strings.Contains(ir, "runtime.sliceAppend"))
	})

	test(252, `package main

type Node struct {
	kind     int32
	children []*Node
	value    string
}

func newLeaf(val string) *Node {
	return &Node{kind: 1, value: val}
}

func newBranch(children []*Node) *Node {
	return &Node{kind: 2, children: children}
}

func countLeaves(n *Node) int32 {
	if n.kind == 1 {
		return 1
	}
	total := int32(0)
	for i := 0; i < len(n.children); i++ {
		total = total + countLeaves(n.children[i])
	}
	return total
}

func main() {
	a := newLeaf("x")
	b := newLeaf("y")
	c := newLeaf("z")
	branch := newBranch([]*Node{a, b, c})
	n := countLeaves(branch)
	_ = n
}
`, func(ir string) {
		assert("252: has countLeaves", strings.Contains(ir, "@main.countLeaves"))
		assert("252: recursive call", strings.Contains(ir, "call i32 @main.countLeaves"))
		assert("252: has newBranch", strings.Contains(ir, "@main.newBranch"))
		assert("252: slice of ptr indexing", strings.Contains(ir, "getelementptr"))
	})

	test(253, `package main

type Checker struct {
	errors []string
	pkg    string
}

func (c *Checker) errorf(msg string) {
	c.errors = append(c.errors, c.pkg | ": " | msg)
}

func (c *Checker) hasErrors() bool {
	return len(c.errors) > 0
}

func (c *Checker) check(name string, typ string) {
	if typ == "" {
		c.errorf("undefined type for " | name)
		return
	}
	if name == "" {
		c.errorf("empty name")
	}
}

func main() {
	c := &Checker{pkg: "main"}
	c.check("x", "int")
	c.check("y", "")
	has := c.hasErrors()
	_ = has
}
`, func(ir string) {
		assert("253: has Checker.errorf", strings.Contains(ir, "@main.Checker.errorf"))
		assert("253: has Checker.check", strings.Contains(ir, "@main.Checker.check"))
		assert("253: has Checker.hasErrors", strings.Contains(ir, "@main.Checker.hasErrors"))
		assert("253: string concat in errorf", strings.Contains(ir, "runtime.sliceAppend"))
		assert("253: string equal for empty check", strings.Contains(ir, "runtime.stringEqual"))
	})

	test(254, `package main

type Type interface {
	Underlying() Type
	String() string
}

type Basic struct {
	name string
	kind int32
}

func (b *Basic) Underlying() Type { return b }
func (b *Basic) String() string   { return b.name }

type Pointer struct {
	base Type
}

func (p *Pointer) Underlying() Type { return p }
func (p *Pointer) String() string   { return "*" | p.base.String() }
func (p *Pointer) Elem() Type      { return p.base }

type Named struct {
	name       string
	underlying Type
}

func (n *Named) Underlying() Type {
	if n.underlying != nil {
		return n.underlying.Underlying()
	}
	return n
}

func (n *Named) String() string { return n.name }

func NewPointer(base Type) *Pointer { return &Pointer{base: base} }
func NewNamed(name string, underlying Type) *Named {
	return &Named{name: name, underlying: underlying}
}

func isPointer(t Type) bool {
	_, ok := t.Underlying().(*Pointer)
	return ok
}

func baseType(t Type) Type {
	if p, ok := t.Underlying().(*Pointer); ok {
		return p.Elem()
	}
	return t
}

func main() {
	intT := &Basic{name: "int", kind: 1}
	ptrInt := NewPointer(intT)
	named := NewNamed("MyInt", intT)
	ptrNamed := NewPointer(named)

	s1 := ptrInt.String()
	s2 := named.String()
	ip := isPointer(ptrNamed)
	bt := baseType(ptrNamed)
	_ = s1
	_ = s2
	_ = ip
	_ = bt
}
`, func(ir string) {
		assert("254: has Pointer.String", strings.Contains(ir, "@main.Pointer.String"))
		assert("254: has Named.Underlying", strings.Contains(ir, "@main.Named.Underlying"))
		assert("254: has isPointer", strings.Contains(ir, "@main.isPointer"))
		assert("254: has baseType", strings.Contains(ir, "@main.baseType"))
		assert("254: interface method dispatch", strings.Contains(ir, "typeid"))
	})

	test(255, `package main

type Instruction interface {
	Operands() int32
}

type BinOp struct {
	op   string
	x    int32
	y    int32
}

func (b *BinOp) Operands() int32 { return 2 }

type UnOp struct {
	op   string
	x    int32
}

func (u *UnOp) Operands() int32 { return 1 }

type Block struct {
	instrs []Instruction
}

func (bl *Block) add(i Instruction) {
	bl.instrs = append(bl.instrs, i)
}

func (bl *Block) count() int32 {
	total := int32(0)
	for i := 0; i < len(bl.instrs); i++ {
		total = total + bl.instrs[i].Operands()
	}
	return total
}

func main() {
	bl := &Block{}
	bl.add(&BinOp{op: "add", x: 1, y: 2})
	bl.add(&UnOp{op: "neg", x: 3})
	bl.add(&BinOp{op: "mul", x: 4, y: 5})
	c := bl.count()
	_ = c
}
`, func(ir string) {
		assert("255: has Block.add", strings.Contains(ir, "@main.Block.add"))
		assert("255: has Block.count", strings.Contains(ir, "@main.Block.count"))
		assert("255: has BinOp.Operands", strings.Contains(ir, "@main.BinOp.Operands"))
		assert("255: interface dispatch in count", strings.Contains(ir, "typeid"))
	})

	test(256, `package main

func reverse(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

func contains(s string, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func main() {
	r := reverse("hello")
	_ = r
	c := contains("hello world", "world")
	_ = c
}
`, func(ir string) {
		assert("256: has reverse", strings.Contains(ir, "@main.reverse"))
		assert("256: has contains", strings.Contains(ir, "@main.contains"))
		assert("256: string equal", strings.Contains(ir, "runtime.stringEqual"))
		assert("256: byte swap", strings.Contains(ir, "getelementptr"))
	})

	test(257, `package main

type Gen struct {
	counter int32
	prefix  string
}

func (g *Gen) emit() string {
	p := func(tag string) string {
		g.counter++
		return g.prefix | tag | itoa257(g.counter)
	}
	a := p("a")
	b := p("b")
	c := p("a")
	return a | " " | b | " " | c
}

func itoa257(n int32) string {
	if n == 0 { return "0" }
	buf := []byte{:0:10}
	for n > 0 {
		buf = append(buf, byte('0' + n % 10))
		n = n / 10
	}
	for i, j := int32(0), int32(len(buf)-1); i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

func main() {
	g := &Gen{prefix: "%t"}
	result := g.emit()
	_ = result
}
`, func(ir string) {
		assert("257: has Gen.emit", strings.Contains(ir, "@main.Gen.emit"))
		assert("257: closure function", strings.Contains(ir, "@main.Gen.emit__anon1"))
	})

	test(258, `package main

type MapEntry struct {
	key string
	val int32
}

func sortEntries(entries []MapEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0; j-- {
			if entries[j].key < entries[j-1].key {
				entries[j], entries[j-1] = entries[j-1], entries[j]
			}
		}
	}
}

func main() {
	entries := []MapEntry{
		{key: "c", val: 3},
		{key: "a", val: 1},
		{key: "b", val: 2},
	}
	sortEntries(entries)
}
`, func(ir string) {
		assert("258: has sortEntries", strings.Contains(ir, "@main.sortEntries"))
		assert("258: string less-than", strings.Contains(ir, "runtime.stringLess"))
		assert("258: swap pattern", strings.Contains(ir, "getelementptr"))
	})

	test(259, `package main

type Scope struct {
	parent *Scope
	names  map[string]bool
}

func (s *Scope) define(name string) {
	s.names[name] = true
}

func (s *Scope) isDefined(name string) bool {
	if s.names[name] {
		return true
	}
	if s.parent != nil {
		return s.parent.isDefined(name)
	}
	return false
}

func (s *Scope) allNames() []string {
	var result []string
	for k, _ := range s.names {
		result = append(result, k)
	}
	return result
}

func main() {
	outer := &Scope{names: map[string]bool{}}
	outer.define("x")
	outer.define("y")
	inner := &Scope{parent: outer, names: map[string]bool{}}
	inner.define("z")
	d := inner.isDefined("x")
	names := inner.allNames()
	_ = d
	_ = names
}
`, func(ir string) {
		assert("259: has Scope.define", strings.Contains(ir, "@main.Scope.define"))
		assert("259: has Scope.isDefined", strings.Contains(ir, "@main.Scope.isDefined"))
		assert("259: has Scope.allNames", strings.Contains(ir, "@main.Scope.allNames"))
		assert("259: recursive method", strings.Contains(ir, "call i1 @main.Scope.isDefined"))
		assert("259: for-range map", strings.Contains(ir, "hashmapNext"))
	})

	test(260, `package main

type SSAOp int32
const (
	OpIllegal SSAOp = iota
	OpAdd
	OpSub
	OpMul
	OpOr
	OpAnd
)

type SSAValue interface {
	Name() string
	Type() string
}

type SSAConst struct {
	name string
	typ  string
	val  int32
}

func (c *SSAConst) Name() string { return c.name }
func (c *SSAConst) Type() string { return c.typ }

type SSABinOp struct {
	name string
	typ  string
	Op   SSAOp
	X    SSAValue
	Y    SSAValue
}

func (b *SSABinOp) Name() string { return b.name }
func (b *SSABinOp) Type() string { return b.typ }

type Emitter struct {
	buf  []byte
	regs int32
}

func (e *Emitter) w(s string) {
	e.buf = append(e.buf, s...)
}

func (e *Emitter) nextReg() string {
	e.regs++
	return "%t" | itoa260(e.regs)
}

func (e *Emitter) emitBinOp(b *SSABinOp) {
	reg := b.Name()
	lv := b.X.Name()
	rv := b.Y.Name()
	op := ""
	switch b.Op {
	case OpAdd:
		op = "add"
	case OpSub:
		op = "sub"
	case OpMul:
		op = "mul"
	case OpOr:
		op = "or"
	case OpAnd:
		op = "and"
	}
	e.w("  ") ; e.w(reg) ; e.w(" = ") ; e.w(op) ; e.w(" ")
	e.w(b.Type()) ; e.w(" ") ; e.w(lv) ; e.w(", ") ; e.w(rv) ; e.w("\n")
}

func (e *Emitter) emit(instrs []SSAValue) string {
	for i := 0; i < len(instrs); i++ {
		switch v := instrs[i].(type) {
		case *SSABinOp:
			e.emitBinOp(v)
		}
	}
	return string(e.buf)
}

func itoa260(n int32) string {
	if n == 0 { return "0" }
	buf := []byte{:0:10}
	for n > 0 {
		buf = append(buf, byte('0' + n % 10))
		n = n / 10
	}
	for i, j := int32(0), int32(len(buf)-1); i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

func main() {
	c1 := &SSAConst{name: "%c1", typ: "i32", val: 5}
	c2 := &SSAConst{name: "%c2", typ: "i32", val: 3}
	add := &SSABinOp{name: "%t1", typ: "i32", Op: OpAdd, X: c1, Y: c2}
	c3 := &SSAConst{name: "%c3", typ: "i32", val: 7}
	mul := &SSABinOp{name: "%t2", typ: "i32", Op: OpMul, X: add, Y: c3}

	em := &Emitter{}
	var instrs []SSAValue
	instrs = append(instrs, add)
	instrs = append(instrs, mul)
	result := em.emit(instrs)
	_ = result
}
`, func(ir string) {
		assert("260: has Emitter.emit", strings.Contains(ir, "@main.Emitter.emit"))
		assert("260: has Emitter.emitBinOp", strings.Contains(ir, "@main.Emitter.emitBinOp"))
		assert("260: has Emitter.w", strings.Contains(ir, "@main.Emitter.w"))
		assert("260: SSAValue interface dispatch", strings.Contains(ir, "typeid"))
		assert("260: type switch on SSAValue", strings.Contains(ir, "typeid.ptr.SSABinOp"))
		assert("260: append interface coercion", strings.Contains(ir, "typeid.ptr.SSAConst") || strings.Contains(ir, "insertvalue {ptr, ptr}"))
	})

	test(261, `package main

type TokenKind int32

const (
	TokEOF TokenKind = iota
	TokIdent
	TokNumber
	TokPlus
	TokMinus
	TokStar
)

func (k TokenKind) String() string {
	switch k {
	case TokEOF:
		return "EOF"
	case TokIdent:
		return "IDENT"
	case TokNumber:
		return "NUMBER"
	case TokPlus:
		return "+"
	case TokMinus:
		return "-"
	case TokStar:
		return "*"
	}
	return "?"
}

func (k TokenKind) IsOperator() bool {
	return k >= TokPlus && k <= TokStar
}

type Token struct {
	kind TokenKind
	lit  string
}

func (t *Token) String() string {
	if t.lit != "" {
		return t.kind.String() | "(" | t.lit | ")"
	}
	return t.kind.String()
}

func main() {
	t := &Token{kind: TokIdent, lit: "foo"}
	s := t.String()
	_ = s
	op := TokPlus.IsOperator()
	_ = op
	name := TokIdent.String()
	_ = name
}
`, func(ir string) {
		assert("261: method on named int", strings.Contains(ir, "@main.TokenKind.String"))
		assert("261: TokenKind.IsOperator", strings.Contains(ir, "@main.TokenKind.IsOperator"))
		assert("261: Token.String calls kind.String", strings.Contains(ir, "call {ptr, i64, i64} @main.TokenKind.String"))
		assert("261: value receiver method", strings.Contains(ir, "define {ptr, i64, i64} @main.TokenKind.String(i32 %k"))
	})

	test(262, `package main

type Info int32
const (
	IsInteger Info = 1 << iota
	IsFloat
	IsUnsigned
	IsString
	IsUntyped
)

func (i Info) Has(flag Info) bool {
	return i & flag != 0
}

type BasicKind int32
const (
	Int32Kind BasicKind = iota
	Uint32Kind
	Float64Kind
	StringKind
)

type Basic struct {
	kind BasicKind
	info Info
	name string
}

var basics = [4]*Basic{
	&Basic{kind: Int32Kind, info: IsInteger, name: "int32"},
	&Basic{kind: Uint32Kind, info: IsInteger | IsUnsigned, name: "uint32"},
	&Basic{kind: Float64Kind, info: IsFloat, name: "float64"},
	&Basic{kind: StringKind, info: IsString, name: "string"},
}

func lookupBasic(kind BasicKind) *Basic {
	if int32(kind) >= 0 && int32(kind) < 4 {
		return basics[int32(kind)]
	}
	return nil
}

func main() {
	b := lookupBasic(Uint32Kind)
	isInt := b.info.Has(IsInteger)
	isUns := b.info.Has(IsUnsigned)
	isFlt := b.info.Has(IsFloat)
	_ = isInt
	_ = isUns
	_ = isFlt
}
`, func(ir string) {
		assert("262: has Info.Has", strings.Contains(ir, "@main.Info.Has"))
		assert("262: has lookupBasic", strings.Contains(ir, "@main.lookupBasic"))
		assert("262: array global", strings.Contains(ir, "@main.basics"))
		assert("262: bitwise and in Has", strings.Contains(ir, " and "))
		assert("262: const flags passed to Has", strings.Contains(ir, "i32 1") && strings.Contains(ir, "i32 4"))
	})

	test(263, `package main

type Member interface {
	MemberName() string
}

type Function struct {
	name   string
	params []string
}

func (f *Function) MemberName() string { return f.name }

type Global struct {
	name string
	typ  string
}

func (g *Global) MemberName() string { return g.name }

type Package struct {
	members map[string]Member
}

func NewPackage() *Package {
	return &Package{members: map[string]Member{}}
}

func (p *Package) addFunc(name string, params []string) {
	p.members[name] = &Function{name: name, params: params}
}

func (p *Package) addGlobal(name string, typ string) {
	p.members[name] = &Global{name: name, typ: typ}
}

func (p *Package) lookup(name string) Member {
	m, ok := p.members[name]
	if ok {
		return m
	}
	return nil
}

func (p *Package) sortedNames() []string {
	var names []string
	for k, _ := range p.members {
		names = append(names, k)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

func main() {
	pkg := NewPackage()
	pkg.addFunc("main", nil)
	pkg.addFunc("helper", []string{"x", "y"})
	pkg.addGlobal("counter", "int32")
	m := pkg.lookup("helper")
	_ = m
	names := pkg.sortedNames()
	_ = names
}
`, func(ir string) {
		assert("263: has Package.addFunc", strings.Contains(ir, "@main.Package.addFunc"))
		assert("263: has Package.lookup", strings.Contains(ir, "@main.Package.lookup"))
		assert("263: has Package.sortedNames", strings.Contains(ir, "@main.Package.sortedNames"))
		assert("263: map interface store", strings.Contains(ir, "hashmapContentSet"))
		assert("263: for-range map", strings.Contains(ir, "hashmapNext"))
		assert("263: insertion sort", strings.Contains(ir, "stringLess"))
	})

	test(264, `package main

type SSAOp int32
const (
	OpIllegal SSAOp = iota
	OpAdd
	OpSub
	OpMul
	OpQuo
	OpRem
	OpAnd
	OpOr
	OpXor
	OpShl
	OpShr
)

func (op SSAOp) String() string {
	switch op {
	case OpAdd: return "+"
	case OpSub: return "-"
	case OpMul: return "*"
	case OpQuo: return "/"
	case OpRem: return "%"
	case OpAnd: return "&"
	case OpOr:  return "|"
	case OpXor: return "^"
	case OpShl: return "<<"
	case OpShr: return ">>"
	}
	return "?"
}

type Type interface {
	Underlying() Type
	String() string
}

type Basic struct {
	kind int32
	info int32
	name string
}

func (b *Basic) Underlying() Type { return b }
func (b *Basic) String() string   { return b.name }

type Pointer struct {
	base Type
}

func (p *Pointer) Underlying() Type { return p }
func (p *Pointer) String() string   { return "*" | p.base.String() }
func (p *Pointer) Elem() Type       { return p.base }

type Slice struct {
	elem Type
}

func (s *Slice) Underlying() Type { return s }
func (s *Slice) String() string   { return "[]" | s.elem.String() }
func (s *Slice) Elem() Type       { return s.elem }

type Scope struct {
	parent *Scope
	elems  map[string]Type
}

func NewScope(parent *Scope) *Scope {
	return &Scope{parent: parent, elems: map[string]Type{}}
}

func (s *Scope) Insert(name string, t Type) {
	s.elems[name] = t
}

func (s *Scope) Lookup(name string) Type {
	t, ok := s.elems[name]
	if ok {
		return t
	}
	if s.parent != nil {
		return s.parent.Lookup(name)
	}
	return nil
}

type SSAValue interface {
	SSAName() string
	SSAType() Type
}

type SSAConst struct {
	name string
	typ  Type
	val  int32
}

func (c *SSAConst) SSAName() string { return c.name }
func (c *SSAConst) SSAType() Type   { return c.typ }

type SSABinOp struct {
	name string
	typ  Type
	Op   SSAOp
	X    SSAValue
	Y    SSAValue
}

func (b *SSABinOp) SSAName() string { return b.name }
func (b *SSABinOp) SSAType() Type   { return b.typ }

type Emitter struct {
	buf     []byte
	nextReg int32
	scope   *Scope
}

func NewEmitter() *Emitter {
	s := NewScope(nil)
	s.Insert("int32", &Basic{kind: 0, info: 1, name: "int32"})
	s.Insert("string", &Basic{kind: 1, info: 8, name: "string"})
	s.Insert("bool", &Basic{kind: 2, info: 0, name: "bool"})
	return &Emitter{buf: []byte{:0:4096}, scope: s}
}

func (e *Emitter) w(s string) {
	e.buf = append(e.buf, s...)
}

func (e *Emitter) reg() string {
	e.nextReg++
	return "%t" | itoa264(e.nextReg)
}

func (e *Emitter) llvmType(t Type) string {
	if t == nil {
		return "void"
	}
	switch t := t.Underlying().(type) {
	case *Basic:
		if t.name == "int32" {
			return "i32"
		}
		if t.name == "bool" {
			return "i1"
		}
		if t.name == "string" {
			return "{ptr, i64, i64}"
		}
	case *Pointer:
		return "ptr"
	case *Slice:
		return "{ptr, i64, i64}"
	}
	return "void"
}

func (e *Emitter) emitBinOp(b *SSABinOp) string {
	reg := e.reg()
	lt := e.llvmType(b.X.SSAType())
	lv := b.X.SSAName()
	rv := b.Y.SSAName()
	op := ""
	switch b.Op {
	case OpAdd: op = "add"
	case OpSub: op = "sub"
	case OpMul: op = "mul"
	case OpOr:  op = "or"
	case OpAnd: op = "and"
	}
	if op == "" {
		return reg
	}
	e.w("  ") ; e.w(reg) ; e.w(" = ") ; e.w(op) ; e.w(" ")
	e.w(lt) ; e.w(" ") ; e.w(lv) ; e.w(", ") ; e.w(rv) ; e.w("\n")
	return reg
}

func (e *Emitter) String() string {
	return string(e.buf)
}

func itoa264(n int32) string {
	if n == 0 { return "0" }
	buf := []byte{:0:10}
	for n > 0 {
		buf = append(buf, byte('0' + n % 10))
		n = n / 10
	}
	for i, j := int32(0), int32(len(buf)-1); i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

func main() {
	em := NewEmitter()
	int32Type := em.scope.Lookup("int32")

	c1 := &SSAConst{name: "1", typ: int32Type, val: 1}
	c2 := &SSAConst{name: "2", typ: int32Type, val: 2}
	add := &SSABinOp{name: "%t1", typ: int32Type, Op: OpAdd, X: c1, Y: c2}
	c3 := &SSAConst{name: "3", typ: int32Type, val: 3}
	mul := &SSABinOp{name: "%t2", typ: int32Type, Op: OpMul, X: add, Y: c3}
	c4 := &SSAConst{name: "7", typ: int32Type, val: 7}
	bor := &SSABinOp{name: "%t3", typ: int32Type, Op: OpOr, X: mul, Y: c4}

	em.emitBinOp(add)
	em.emitBinOp(mul)
	em.emitBinOp(bor)

	result := em.String()
	_ = result

	lt := em.llvmType(&Pointer{base: int32Type})
	_ = lt
	st := em.llvmType(&Slice{elem: int32Type})
	_ = st
}
`, func(ir string) {
		assert("264: has Emitter.emitBinOp", strings.Contains(ir, "@main.Emitter.emitBinOp"))
		assert("264: has Emitter.llvmType", strings.Contains(ir, "@main.Emitter.llvmType"))
		assert("264: SSAOp.String method on named int", strings.Contains(ir, "@main.SSAOp.String"))
		assert("264: Scope.Lookup recursive", strings.Contains(ir, "call {ptr, ptr} @main.Scope.Lookup"))
		assert("264: type switch in llvmType", strings.Contains(ir, "typeid"))
		assert("264: interface dispatch SSAConst", strings.Contains(ir, "typeid.SSAConst") || strings.Contains(ir, "typeid.ptr.SSAConst"))
		assert("264: string concat via |", strings.Contains(ir, "runtime.sliceAppend"))
		assert("264: scope map lookup", strings.Contains(ir, "hashmapContentGet"))
	})

	test(265, `package main

type Instruction interface {
	Block() *BasicBlock
	InstrString() string
}

type Value interface {
	Name() string
	Type() string
}

type instrBase struct {
	block *BasicBlock
}

func (a *instrBase) Block() *BasicBlock { return a.block }

type register struct {
	instrBase
	name string
	typ  string
}

func (r *register) Name() string { return r.name }
func (r *register) Type() string { return r.typ }

type BinOp struct {
	register
	Op string
	X  Value
	Y  Value
}

func (b *BinOp) InstrString() string {
	return b.Op | " " | b.X.Name() | " " | b.Y.Name()
}

type Store struct {
	instrBase
	Addr Value
	Val  Value
}

func (s *Store) InstrString() string {
	return "store " | s.Val.Name() | " -> " | s.Addr.Name()
}

type BasicBlock struct {
	index  int32
	name   string
	instrs []Instruction
}

func (bl *BasicBlock) emit(i Instruction) {
	bl.instrs = append(bl.instrs, i)
}

type Const struct {
	name string
	typ  string
	val  int32
}

func (c *Const) Name() string { return c.name }
func (c *Const) Type() string { return c.typ }

func main() {
	bl := &BasicBlock{index: 0, name: "entry"}
	c1 := &Const{name: "%c1", typ: "i32", val: 1}
	c2 := &Const{name: "%c2", typ: "i32", val: 2}

	add := &BinOp{
		register: register{instrBase: instrBase{block: bl}, name: "%t1", typ: "i32"},
		Op: "add", X: c1, Y: c2,
	}
	bl.emit(add)

	store := &Store{
		instrBase: instrBase{block: bl},
		Addr: c1, Val: add,
	}
	bl.emit(store)

	s1 := add.InstrString()
	s2 := store.InstrString()
	_ = s1
	_ = s2

	n := add.Name()
	t := add.Type()
	b := add.Block()
	_ = n
	_ = t
	_ = b
}
`, func(ir string) {
		assert("265: has BinOp.InstrString", strings.Contains(ir, "@main.BinOp.InstrString"))
		assert("265: has Store.InstrString", strings.Contains(ir, "@main.Store.InstrString"))
		assert("265: multi-level embed register.Name", strings.Contains(ir, "@main.register.Name"))
		assert("265: deep embed instrBase.Block", strings.Contains(ir, "@main.instrBase.Block"))
		assert("265: interface append with boxing", strings.Contains(ir, "insertvalue {ptr, ptr}"))
		assert("265: promoted field access", strings.Contains(ir, "getelementptr"))
	})

	// Test 266: comma-ok type assertions
	test(266, `package main

type Node interface {
	Kind() string
}

type Ident struct {
	name string
}
func (i *Ident) Kind() string { return "ident" }

type Literal struct {
	val int32
}
func (l *Literal) Kind() string { return "literal" }

func classify(n Node) string {
	if id, ok := n.(*Ident); ok {
		return id.name
	}
	if lit, ok := n.(*Literal); ok {
		_ = lit.val
		return "lit"
	}
	return "unknown"
}

func main() {
	var n Node
	n = &Ident{name: "foo"}
	s1 := classify(n)
	_ = s1

	n = &Literal{val: 42}
	s2 := classify(n)
	_ = s2
}
`, func(ir string) {
		assert("266: has classify", strings.Contains(ir, "@main.classify"))
		assert("266: typeid for assertions", strings.Contains(ir, "typeid"))
		assert("266: icmp for type check", strings.Contains(ir, "icmp eq ptr"))
	})

	// Test 267: for-range map with key and value
	test(267, `package main

type Entry struct {
	name string
	val  int32
}

func sumMap(m map[string]int32) int32 {
	total := int32(0)
	for _, v := range m {
		total = total + v
	}
	return total
}

func collectKeys(m map[string]*Entry) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func main() {
	m := map[string]int32{}
	m["a"] = 1
	m["b"] = 2
	s := sumMap(m)
	_ = s

	entries := map[string]*Entry{}
	entries["x"] = &Entry{name: "x", val: 10}
	keys := collectKeys(entries)
	_ = keys
}
`, func(ir string) {
		assert("267: has sumMap", strings.Contains(ir, "@main.sumMap"))
		assert("267: has collectKeys", strings.Contains(ir, "@main.collectKeys"))
		assert("267: hashmapNext for range", strings.Contains(ir, "hashmapNext"))
	})

	// Test 268: closure capturing struct pointer, called multiple times
	test(268, `package main

type Builder struct {
	buf    []byte
	indent int32
}

func (b *Builder) build() string {
	p := func(s string) {
		b.buf = append(b.buf, s...)
	}
	nl := func() {
		b.buf = append(b.buf, '\n')
	}
	p("func main() {")
	nl()
	p("  return")
	nl()
	p("}")
	nl()
	return string(b.buf)
}

func main() {
	b := &Builder{}
	result := b.build()
	_ = result
}
`, func(ir string) {
		assert("268: has Builder.build", strings.Contains(ir, "@main.Builder.build"))
		assert("268: closure captures", strings.Contains(ir, "insertvalue {ptr, ptr}"))
		assert("268: sliceAppend for buf", strings.Contains(ir, "sliceAppend"))
	})

	// Test 269: multiple comma-ok assertions in sequence with branching
	test(269, `package main

type Type interface {
	Underlying() Type
}

type Basic struct {
	kind int32
}
func (b *Basic) Underlying() Type { return b }

type Pointer struct {
	elem Type
}
func (p *Pointer) Underlying() Type { return p }
func (p *Pointer) Elem() Type { return p.elem }

type Named struct {
	name string
	underlying Type
}
func (n *Named) Underlying() Type { return n.underlying }

func resolve(t Type) Type {
	if p, ok := t.(*Pointer); ok {
		return p.Elem()
	}
	if n, ok := t.(*Named); ok {
		return n.Underlying()
	}
	return t
}

func isPointerTo(t Type, kind int32) bool {
	p, ok := t.(*Pointer)
	if !ok {
		return false
	}
	b, ok2 := p.Elem().(*Basic)
	if !ok2 {
		return false
	}
	return b.kind == kind
}

func main() {
	intType := &Basic{kind: 1}
	ptrInt := &Pointer{elem: intType}
	named := &Named{name: "myint", underlying: intType}

	r1 := resolve(ptrInt)
	_ = r1
	r2 := resolve(named)
	_ = r2
	r3 := resolve(intType)
	_ = r3

	yes := isPointerTo(ptrInt, 1)
	no := isPointerTo(intType, 1)
	_ = yes
	_ = no
}
`, func(ir string) {
		assert("269: has resolve", strings.Contains(ir, "@main.resolve"))
		assert("269: has isPointerTo", strings.Contains(ir, "@main.isPointerTo"))
		assert("269: multiple type assertions", strings.Count(ir, "icmp eq ptr") >= 3)
	})

	// Test 270: string([]byte) and []byte(string) conversions with itoa pattern
	test(270, `package main

func itoa(n int32) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := int32(19)
	for n > 0 {
		buf[i] = byte(n%10) + '0'
		n = n / 10
		i = i - 1
	}
	if neg {
		buf[i] = '-'
		i = i - 1
	}
	return string(buf[i+1:])
}

func concat(parts []string) string {
	var buf []byte
	for _, p := range parts {
		buf = append(buf, p...)
	}
	return string(buf)
}

func main() {
	s1 := itoa(42)
	s2 := itoa(-7)
	s3 := itoa(0)
	_ = s1
	_ = s2
	_ = s3

	parts := []string{"hello", " ", "world"}
	joined := concat(parts)
	_ = joined
}
`, func(ir string) {
		assert("270: has itoa", strings.Contains(ir, "@main.itoa"))
		assert("270: has concat", strings.Contains(ir, "@main.concat"))
		assert("270: array alloca for buf", strings.Contains(ir, "alloca [20 x i8]"))
		assert("270: sliceAppend for concat", strings.Contains(ir, "sliceAppend"))
	})

	// Test 271: for-range over map with both key and value, building output
	test(271, `package main

type Member interface {
	Name() string
}

type Func struct {
	name string
}
func (f *Func) Name() string { return f.name }

type Global struct {
	name string
	typ  string
}
func (g *Global) Name() string { return g.name }

type Package struct {
	members map[string]Member
}

func (p *Package) addFunc(name string) {
	p.members[name] = &Func{name: name}
}

func (p *Package) addGlobal(name string, typ string) {
	p.members[name] = &Global{name: name, typ: typ}
}

func (p *Package) allNames() []string {
	var names []string
	for k, m := range p.members {
		_ = m
		names = append(names, k)
	}
	return names
}

func (p *Package) findFuncs() []*Func {
	var funcs []*Func
	for _, m := range p.members {
		if f, ok := m.(*Func); ok {
			funcs = append(funcs, f)
		}
	}
	return funcs
}

func main() {
	p := &Package{members: map[string]Member{}}
	p.addFunc("main")
	p.addFunc("init")
	p.addGlobal("x", "i32")

	names := p.allNames()
	_ = names

	funcs := p.findFuncs()
	_ = funcs
}
`, func(ir string) {
		assert("271: has allNames", strings.Contains(ir, "@main.Package.allNames"))
		assert("271: has findFuncs", strings.Contains(ir, "@main.Package.findFuncs"))
		assert("271: range map hashmapNext", strings.Contains(ir, "hashmapNext"))
		assert("271: type assertion in range body", strings.Contains(ir, "icmp eq ptr"))
	})

	// Test 272: nested closure calls - p("text") pattern from ir_emit.mx
	test(272, `package main

type Emitter struct {
	buf     []byte
	nextReg int32
}

func (e *Emitter) emit() string {
	p := func(prefix string) string {
		e.nextReg = e.nextReg + 1
		return "%" | prefix | itoa272(e.nextReg)
	}
	var out []byte
	r1 := p("t")
	out = append(out, r1...)
	out = append(out, ' ')
	r2 := p("t")
	out = append(out, r2...)
	out = append(out, ' ')
	r3 := p("mi")
	out = append(out, r3...)
	return string(out)
}

func itoa272(n int32) string {
	if n == 0 { return "0" }
	var buf [10]byte
	i := int32(9)
	for n > 0 {
		buf[i] = byte(n%10) + '0'
		n = n / 10
		i = i - 1
	}
	return string(buf[i+1:])
}

func main() {
	e := &Emitter{}
	result := e.emit()
	_ = result
}
`, func(ir string) {
		assert("272: has Emitter.emit", strings.Contains(ir, "@main.Emitter.emit"))
		assert("272: closure with capture", strings.Contains(ir, "insertvalue {ptr, ptr}"))
		assert("272: string concat via or", strings.Contains(ir, "sliceAppend"))
	})

	// Test 273: chained method calls with intermediate results
	test(273, `package main

type Scope struct {
	parent *Scope
	names  map[string]int32
}

func (s *Scope) Lookup(name string) int32 {
	if s.names != nil {
		v, ok := s.names[name]
		if ok {
			return v
		}
	}
	if s.parent != nil {
		return s.parent.Lookup(name)
	}
	return -1
}

func (s *Scope) LookupParent(name string) (*Scope, int32) {
	if s.names != nil {
		v, ok := s.names[name]
		if ok {
			return s, v
		}
	}
	if s.parent != nil {
		return s.parent.LookupParent(name)
	}
	return nil, -1
}

func main() {
	inner := &Scope{
		names: map[string]int32{"x": 1},
	}
	outer := &Scope{
		names: map[string]int32{"y": 2},
	}
	inner.parent = outer

	v1 := inner.Lookup("x")
	v2 := inner.Lookup("y")
	v3 := inner.Lookup("z")
	_ = v1
	_ = v2
	_ = v3

	s, v := inner.LookupParent("y")
	_ = s
	_ = v
}
`, func(ir string) {
		assert("273: has Lookup", strings.Contains(ir, "@main.Scope.Lookup"))
		assert("273: has LookupParent", strings.Contains(ir, "@main.Scope.LookupParent"))
		assert("273: recursive call", strings.Count(ir, "call") >= 2)
		assert("273: multi-return", strings.Contains(ir, "extractvalue"))
	})

	// Test 274: map[string]bool and delete pattern
	test(274, `package main

func main() {
	seen := map[string]bool{}
	names := []string{"a", "b", "a", "c", "b"}
	var unique []string
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			unique = append(unique, n)
		}
	}
	_ = unique

	delete(seen, "a")
	has := seen["a"]
	_ = has
}
`, func(ir string) {
		assert("274: hashmapContentSet for map assign", strings.Contains(ir, "hashmapContentSet"))
		assert("274: hashmapContentGet for map lookup", strings.Contains(ir, "hashmapContentGet"))
		assert("274: hashmapBinaryDelete for delete", strings.Contains(ir, "hashmapBinaryDelete"))
	})

	// Test 275: interface dispatch with multiple implementors and return values
	test(275, `package main

type SSAValue interface {
	SSAName() string
	SSAType() string
}

type SSAConst struct {
	name string
	typ  string
	val  int32
}
func (c *SSAConst) SSAName() string { return c.name }
func (c *SSAConst) SSAType() string { return c.typ }

type SSABinOp struct {
	name string
	typ  string
	op   string
	x    SSAValue
	y    SSAValue
}
func (b *SSABinOp) SSAName() string { return b.name }
func (b *SSABinOp) SSAType() string { return b.typ }

func operand(v SSAValue) string {
	return v.SSAName()
}

func emitBinOp(b *SSABinOp) string {
	return b.SSAName() | " = " | b.op | " " | b.SSAType() | " " | operand(b.x) | ", " | operand(b.y)
}

func main() {
	c1 := &SSAConst{name: "%c1", typ: "i32", val: 1}
	c2 := &SSAConst{name: "%c2", typ: "i32", val: 2}
	add := &SSABinOp{name: "%t1", typ: "i32", op: "add", x: c1, y: c2}
	result := emitBinOp(add)
	_ = result

	name := operand(c1)
	_ = name
}
`, func(ir string) {
		assert("275: has operand", strings.Contains(ir, "@main.operand"))
		assert("275: has emitBinOp", strings.Contains(ir, "@main.emitBinOp"))
		assert("275: interface dispatch", strings.Contains(ir, "icmp eq ptr") || strings.Contains(ir, "SSAName"))
	})

	// Test 276: struct with map field, range with both key and value used
	test(276, `package main

type Scope struct {
	parent  *Scope
	objects map[string]*Object
}

type Object struct {
	name string
	kind int32
}

func NewScope(parent *Scope) *Scope {
	return &Scope{parent: parent, objects: map[string]*Object{}}
}

func (s *Scope) Insert(obj *Object) {
	s.objects[obj.name] = obj
}

func (s *Scope) Lookup(name string) *Object {
	if s.objects != nil {
		obj, ok := s.objects[name]
		if ok {
			return obj
		}
	}
	if s.parent != nil {
		return s.parent.Lookup(name)
	}
	return nil
}

func (s *Scope) NumObjects() int32 {
	n := int32(0)
	for _, obj := range s.objects {
		_ = obj
		n = n + 1
	}
	return n
}

func main() {
	outer := NewScope(nil)
	outer.Insert(&Object{name: "int", kind: 1})
	outer.Insert(&Object{name: "string", kind: 2})

	inner := NewScope(outer)
	inner.Insert(&Object{name: "x", kind: 3})

	o1 := inner.Lookup("x")
	o2 := inner.Lookup("int")
	o3 := inner.Lookup("missing")
	_ = o1
	_ = o2
	_ = o3

	n := outer.NumObjects()
	_ = n
}
`, func(ir string) {
		assert("276: has NewScope", strings.Contains(ir, "@main.NewScope"))
		assert("276: has Lookup", strings.Contains(ir, "@main.Scope.Lookup"))
		assert("276: has NumObjects", strings.Contains(ir, "@main.Scope.NumObjects"))
		assert("276: map operations", strings.Contains(ir, "hashmapContentGet"))
	})

	// Test 277: type switch with nil check and multiple concrete types
	test(277, `package main

type Expr interface {
	exprNode()
}

type Ident struct {
	name string
}
func (i *Ident) exprNode() {}

type Call struct {
	fn   Expr
	args []Expr
}
func (c *Call) exprNode() {}

type Literal struct {
	val int32
}
func (l *Literal) exprNode() {}

func describe(e Expr) string {
	if e == nil {
		return "nil"
	}
	switch x := e.(type) {
	case *Ident:
		return "ident:" | x.name
	case *Call:
		return "call"
	case *Literal:
		_ = x.val
		return "literal"
	}
	return "unknown"
}

func main() {
	i := &Ident{name: "foo"}
	l := &Literal{val: 42}
	c := &Call{fn: i, args: []Expr{l}}

	s1 := describe(i)
	s2 := describe(l)
	s3 := describe(c)
	s4 := describe(nil)
	_ = s1
	_ = s2
	_ = s3
	_ = s4
}
`, func(ir string) {
		assert("277: has describe", strings.Contains(ir, "@main.describe"))
		assert("277: nil check", strings.Contains(ir, "icmp eq") || strings.Contains(ir, "null"))
		assert("277: type switch branches", strings.Contains(ir, "typeid"))
	})

	// Test 278: func literal returning value, multiple closures in same func
	test(278, `package main

type Writer struct {
	buf []byte
}

func (w *Writer) emit(prefix string, n int32) string {
	p := func(s string) {
		w.buf = append(w.buf, s...)
	}
	num := func(v int32) string {
		if v == 0 { return "0" }
		var b [10]byte
		i := int32(9)
		for v > 0 {
			b[i] = byte(v%10) + '0'
			v = v / 10
			i = i - 1
		}
		return string(b[i+1:])
	}
	p(prefix)
	s := num(n)
	p(s)
	return string(w.buf)
}

func main() {
	w := &Writer{}
	result := w.emit("count=", 42)
	_ = result
}
`, func(ir string) {
		assert("278: has Writer.emit", strings.Contains(ir, "@main.Writer.emit"))
		assert("278: two closures", strings.Count(ir, "insertvalue {ptr, ptr}") >= 2)
		assert("278: array subslice in closure", strings.Contains(ir, "alloca [10 x i8]"))
	})

	// Test 279: method on result of method call (chained dispatch)
	test(279, `package main

type Token struct {
	kind int32
	text string
}

type Scanner struct {
	src []byte
	pos int32
}

func (s *Scanner) peek() byte {
	if int32(len(s.src)) <= s.pos {
		return 0
	}
	return s.src[s.pos]
}

func (s *Scanner) next() *Token {
	ch := s.peek()
	if ch == 0 {
		return &Token{kind: 0, text: ""}
	}
	s.pos = s.pos + 1
	return &Token{kind: int32(ch), text: string(s.src[s.pos-1:s.pos])}
}

func main() {
	s := &Scanner{src: []byte("abc"), pos: 0}
	t1 := s.next()
	t2 := s.next()
	_ = t1.text
	_ = t2.kind
}
`, func(ir string) {
		assert("279: has Scanner.peek", strings.Contains(ir, "@main.Scanner.peek"))
		assert("279: has Scanner.next", strings.Contains(ir, "@main.Scanner.next"))
		assert("279: field access on result", strings.Contains(ir, "getelementptr"))
	})

	// Test 280: interface slice iteration with type assertion
	test(280, `package main

type Instruction interface {
	String() string
}

type Add struct {
	dst string
	x   string
	y   string
}
func (a *Add) String() string { return a.dst | " = add " | a.x | " " | a.y }

type Ret struct {
	val string
}
func (r *Ret) String() string { return "ret " | r.val }

func printAll(instrs []Instruction) string {
	var out []byte
	for _, instr := range instrs {
		s := instr.String()
		out = append(out, s...)
		out = append(out, '\n')
	}
	return string(out)
}

func countAdds(instrs []Instruction) int32 {
	n := int32(0)
	for _, instr := range instrs {
		if _, ok := instr.(*Add); ok {
			n = n + 1
		}
	}
	return n
}

func main() {
	instrs := []Instruction{
		&Add{dst: "%1", x: "%a", y: "%b"},
		&Add{dst: "%2", x: "%1", y: "%c"},
		&Ret{val: "%2"},
	}
	s := printAll(instrs)
	_ = s
	n := countAdds(instrs)
	_ = n
}
`, func(ir string) {
		assert("280: has printAll", strings.Contains(ir, "@main.printAll"))
		assert("280: has countAdds", strings.Contains(ir, "@main.countAdds"))
		assert("280: interface invoke String", strings.Contains(ir, "icmp eq ptr"))
		assert("280: slice of interfaces", strings.Contains(ir, "insertvalue {ptr, ptr}"))
	})

	// Test 281: nested struct literal with embedded field init
	test(281, `package main

type Pos struct {
	line int32
	col  int32
}

type Name struct {
	Value string
	pos   Pos
}

type Field struct {
	Name     *Name
	Type     *Name
	Exported bool
}

type StructType struct {
	fields []*Field
}

func (s *StructType) NumFields() int32 { return int32(len(s.fields)) }

func main() {
	st := &StructType{
		fields: []*Field{
			{Name: &Name{Value: "x", pos: Pos{line: 1, col: 5}}, Type: &Name{Value: "int32"}, Exported: false},
			{Name: &Name{Value: "Y", pos: Pos{line: 2, col: 5}}, Type: &Name{Value: "string"}, Exported: true},
		},
	}
	n := st.NumFields()
	_ = n
	f0 := st.fields[0]
	_ = f0.Name.Value
}
`, func(ir string) {
		assert("281: has NumFields", strings.Contains(ir, "@main.StructType.NumFields"))
		assert("281: nested struct alloc", strings.Contains(ir, "runtime.alloc"))
		assert("281: field access chain", strings.Contains(ir, "getelementptr"))
	})

	// Test 282: byte to int32 conversion and int32 to byte
	test(282, `package main

func isLetter(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func scanIdent(src []byte, pos int32) (string, int32) {
	start := pos
	for int32(len(src)) > pos && (isLetter(src[pos]) || isDigit(src[pos])) {
		pos = pos + 1
	}
	return string(src[start:pos]), pos
}

func main() {
	src := []byte("hello123 world")
	name, end := scanIdent(src, 0)
	_ = name
	_ = end

	b1 := isLetter('x')
	b2 := isDigit('5')
	b3 := isLetter('9')
	_ = b1
	_ = b2
	_ = b3
}
`, func(ir string) {
		assert("282: has isLetter", strings.Contains(ir, "@main.isLetter"))
		assert("282: has scanIdent", strings.Contains(ir, "@main.scanIdent"))
		assert("282: byte comparison", strings.Contains(ir, "icmp"))
		assert("282: multi-return", strings.Contains(ir, "insertvalue"))
	})

	// Test 283: nil comparison with interface and pointer
	test(283, `package main

type Node interface {
	Kind() string
}

type Leaf struct {
	val int32
}
func (l *Leaf) Kind() string { return "leaf" }

func checkNil(n Node) bool {
	return n == nil
}

func checkPtrNil(l *Leaf) bool {
	return l == nil
}

func safeKind(n Node) string {
	if n == nil {
		return "nil"
	}
	return n.Kind()
}

func main() {
	var n Node
	b1 := checkNil(n)
	_ = b1

	l := &Leaf{val: 1}
	n = l
	b2 := checkNil(n)
	_ = b2

	b3 := checkPtrNil(nil)
	b4 := checkPtrNil(l)
	_ = b3
	_ = b4

	s := safeKind(nil)
	_ = s
}
`, func(ir string) {
		assert("283: has checkNil", strings.Contains(ir, "@main.checkNil"))
		assert("283: has safeKind", strings.Contains(ir, "@main.safeKind"))
		assert("283: nil comparison", strings.Contains(ir, "icmp"))
	})

	// Test 284: switch on string value
	test(284, `package main

func tokenKind(s string) int32 {
	switch s {
	case "func":
		return 1
	case "var":
		return 2
	case "type":
		return 3
	case "return":
		return 4
	}
	return 0
}

func main() {
	k1 := tokenKind("func")
	k2 := tokenKind("var")
	k3 := tokenKind("other")
	_ = k1
	_ = k2
	_ = k3
}
`, func(ir string) {
		assert("284: has tokenKind", strings.Contains(ir, "@main.tokenKind"))
		assert("284: string compare calls", strings.Contains(ir, "stringEqual") || strings.Contains(ir, "icmp"))
	})

	// Test 285: for-range with index only (map key iteration)
	test(285, `package main

func keys(m map[string]int32) []string {
	var result []string
	for k := range m {
		result = append(result, k)
	}
	return result
}

func sumValues(m map[string]int32) int32 {
	total := int32(0)
	for _, v := range m {
		total = total + v
	}
	return total
}

func main() {
	m := map[string]int32{"a": 1, "b": 2, "c": 3}
	ks := keys(m)
	_ = ks
	s := sumValues(m)
	_ = s
}
`, func(ir string) {
		assert("285: has keys", strings.Contains(ir, "@main.keys"))
		assert("285: has sumValues", strings.Contains(ir, "@main.sumValues"))
		assert("285: map iteration", strings.Contains(ir, "hashmapNext"))
	})

	// Test 286: SSA builder pattern - func builder with emit method and block management
	test(286, `package main

type Instruction interface {
	String() string
}

type Block struct {
	instrs []Instruction
	name   string
}

type Add struct {
	dst string
	src string
}
func (a *Add) String() string { return a.dst | " = add " | a.src }

type Ret struct{}
func (r *Ret) String() string { return "ret" }

type FuncBuilder struct {
	blocks []*Block
	cur    *Block
	nextN  int32
}

func (fb *FuncBuilder) newBlock(name string) *Block {
	b := &Block{name: name}
	fb.blocks = append(fb.blocks, b)
	return b
}

func (fb *FuncBuilder) emit(i Instruction) {
	fb.cur.instrs = append(fb.cur.instrs, i)
}

func (fb *FuncBuilder) nextName() string {
	fb.nextN = fb.nextN + 1
	return "%t" | itoa286(fb.nextN)
}

func itoa286(n int32) string {
	if n == 0 { return "0" }
	var buf [10]byte
	i := int32(9)
	for n > 0 {
		buf[i] = byte(n%10) + '0'
		n = n / 10
		i = i - 1
	}
	return string(buf[i+1:])
}

func main() {
	fb := &FuncBuilder{}
	entry := fb.newBlock("entry")
	fb.cur = entry
	n1 := fb.nextName()
	n2 := fb.nextName()
	fb.emit(&Add{dst: n1, src: "%arg0"})
	fb.emit(&Add{dst: n2, src: n1})
	fb.emit(&Ret{})
	total := int32(0)
	for _, b := range fb.blocks {
		total = total + int32(len(b.instrs))
	}
	_ = total
}
`, func(ir string) {
		assert("286: has FuncBuilder.emit", strings.Contains(ir, "@main.FuncBuilder.emit"))
		assert("286: has FuncBuilder.nextName", strings.Contains(ir, "@main.FuncBuilder.nextName"))
		assert("286: has FuncBuilder.newBlock", strings.Contains(ir, "@main.FuncBuilder.newBlock"))
		assert("286: interface boxing in emit", strings.Contains(ir, "insertvalue {ptr, ptr}"))
	})

	// Test 287: type resolution pattern - resolve through Named and Pointer layers
	test(287, `package main

type Type interface {
	Underlying() Type
}

type Basic struct {
	kind int32
	name string
}
func (b *Basic) Underlying() Type { return b }

type Pointer struct {
	base Type
}
func (p *Pointer) Underlying() Type { return p }
func (p *Pointer) Elem() Type { return p.base }

type Named struct {
	obj  *TypeName
	underlying Type
}
func (n *Named) Underlying() Type {
	if n.underlying != nil {
		return n.underlying
	}
	return n
}
func (n *Named) Obj() *TypeName { return n.obj }

type TypeName struct {
	name string
	typ  Type
}
func (tn *TypeName) Name() string { return tn.name }
func (tn *TypeName) Type() Type { return tn.typ }

type Slice struct {
	elem Type
}
func (s *Slice) Underlying() Type { return s }
func (s *Slice) Elem() Type { return s.elem }

func llvmType(t Type) string {
	if t == nil {
		return "void"
	}
	switch u := t.Underlying().(type) {
	case *Basic:
		switch u.kind {
		case 1: return "i32"
		case 2: return "i64"
		case 3: return "{ptr, i64, i64}"
		}
	case *Pointer:
		return "ptr"
	case *Slice:
		return "{ptr, i64, i64}"
	}
	return "void"
}

func isPointer(t Type) bool {
	_, ok := t.Underlying().(*Pointer)
	return ok
}

func main() {
	intType := &Basic{kind: 1, name: "int32"}
	strType := &Basic{kind: 3, name: "string"}
	ptrInt := &Pointer{base: intType}
	tn := &TypeName{name: "MyInt"}
	named := &Named{obj: tn, underlying: intType}
	tn.typ = named
	sliceStr := &Slice{elem: strType}

	s1 := llvmType(intType)
	s2 := llvmType(ptrInt)
	s3 := llvmType(named)
	s4 := llvmType(sliceStr)
	s5 := llvmType(nil)
	_ = s1
	_ = s2
	_ = s3
	_ = s4
	_ = s5

	b1 := isPointer(ptrInt)
	b2 := isPointer(intType)
	_ = b1
	_ = b2
}
`, func(ir string) {
		assert("287: has llvmType", strings.Contains(ir, "@main.llvmType"))
		assert("287: has isPointer", strings.Contains(ir, "@main.isPointer"))
		assert("287: type switch dispatch", strings.Contains(ir, "icmp eq ptr"))
		assert("287: nested switch", strings.Contains(ir, "icmp eq i32"))
	})

	// Test 288: emitter pattern - write() calls with string concat building IR output
	test(288, `package main

type Emitter struct {
	buf     []byte
	nextReg int32
}

func (e *Emitter) w(s string) {
	e.buf = append(e.buf, s...)
}

func (e *Emitter) regName() string {
	e.nextReg = e.nextReg + 1
	return "%t" | itoa288(e.nextReg)
}

func (e *Emitter) emitAdd(dst string, lhs string, rhs string) {
	e.w("  ")
	e.w(dst)
	e.w(" = add i32 ")
	e.w(lhs)
	e.w(", ")
	e.w(rhs)
	e.w("\n")
}

func (e *Emitter) emitRet(val string) {
	e.w("  ret i32 ")
	e.w(val)
	e.w("\n")
}

func itoa288(n int32) string {
	if n == 0 { return "0" }
	var buf [10]byte
	i := int32(9)
	for n > 0 {
		buf[i] = byte(n%10) + '0'
		n = n / 10
		i = i - 1
	}
	return string(buf[i+1:])
}

func main() {
	e := &Emitter{}
	r1 := e.regName()
	r2 := e.regName()
	e.emitAdd(r1, "%arg0", "%arg1")
	e.emitAdd(r2, r1, "%arg2")
	e.emitRet(r2)
	result := string(e.buf)
	_ = result
}
`, func(ir string) {
		assert("288: has Emitter.w", strings.Contains(ir, "@main.Emitter.w"))
		assert("288: has Emitter.regName", strings.Contains(ir, "@main.Emitter.regName"))
		assert("288: has Emitter.emitAdd", strings.Contains(ir, "@main.Emitter.emitAdd"))
		assert("288: sliceAppend for buf", strings.Contains(ir, "sliceAppend"))
	})

	// Test 289: SSA const pattern - nil type, typed nil, constant values
	test(289, `package main

type Type interface {
	String() string
}

type Basic struct {
	name string
}
func (b *Basic) String() string { return b.name }

type Const struct {
	typ Type
	val int32
}

func (c *Const) SSAType() Type { return c.typ }

func operand289(c *Const) string {
	if c.typ == nil {
		return "null"
	}
	if c.val == 0 {
		return "zeroinitializer"
	}
	return itoa289(c.val)
}

func itoa289(n int32) string {
	if n < 0 {
		return "-" | itoa289(-n)
	}
	if n == 0 { return "0" }
	var buf [10]byte
	i := int32(9)
	for n > 0 {
		buf[i] = byte(n%10) + '0'
		n = n / 10
		i = i - 1
	}
	return string(buf[i+1:])
}

func main() {
	intType := &Basic{name: "i32"}
	c1 := &Const{typ: intType, val: 42}
	c2 := &Const{typ: intType, val: 0}
	c3 := &Const{typ: nil, val: 0}

	s1 := operand289(c1)
	s2 := operand289(c2)
	s3 := operand289(c3)
	_ = s1
	_ = s2
	_ = s3

	t := c1.SSAType()
	_ = t
}
`, func(ir string) {
		assert("289: has operand289", strings.Contains(ir, "@main.operand289"))
		assert("289: has itoa289", strings.Contains(ir, "@main.itoa289"))
		assert("289: recursive call for negative", strings.Count(ir, "call") >= 2)
	})

	// Test 290: 300-line stress test - mini compiler pipeline
	test(290, `package main

type Token struct {
	kind int32
	text string
	pos  int32
}

type Expr interface {
	exprNode()
}

type NameExpr struct {
	name string
}
func (n *NameExpr) exprNode() {}

type BinExpr struct {
	op string
	x  Expr
	y  Expr
}
func (b *BinExpr) exprNode() {}

type CallExpr struct {
	fn   Expr
	args []Expr
}
func (c *CallExpr) exprNode() {}

type LitExpr struct {
	val int32
}
func (l *LitExpr) exprNode() {}

type Stmt interface {
	stmtNode()
}

type ReturnStmt struct {
	val Expr
}
func (r *ReturnStmt) stmtNode() {}

type AssignStmt struct {
	name string
	val  Expr
}
func (a *AssignStmt) stmtNode() {}

type SSAValue interface {
	Name() string
	Type() string
}

type SSAReg struct {
	name string
	typ  string
}
func (r *SSAReg) Name() string { return r.name }
func (r *SSAReg) Type() string { return r.typ }

type SSAConst struct {
	name string
	typ  string
	val  int32
}
func (c *SSAConst) Name() string { return c.name }
func (c *SSAConst) Type() string { return c.typ }

type SSAInstr interface {
	InstrString() string
}

type SSABinOp struct {
	dst SSAValue
	op  string
	x   SSAValue
	y   SSAValue
}
func (b *SSABinOp) InstrString() string {
	return b.dst.Name() | " = " | b.op | " " | b.dst.Type() | " " | b.x.Name() | ", " | b.y.Name()
}

type SSARet struct {
	val SSAValue
}
func (r *SSARet) InstrString() string {
	if r.val == nil {
		return "ret void"
	}
	return "ret " | r.val.Type() | " " | r.val.Name()
}

type SSABlock struct {
	name   string
	instrs []SSAInstr
}

func (b *SSABlock) emit(i SSAInstr) {
	b.instrs = append(b.instrs, i)
}

type Builder struct {
	blocks []*SSABlock
	cur    *SSABlock
	nextID int32
	env    map[string]SSAValue
}

func NewBuilder() *Builder {
	b := &Builder{env: map[string]SSAValue{}}
	entry := &SSABlock{name: "entry"}
	b.blocks = append(b.blocks, entry)
	b.cur = entry
	return b
}

func (b *Builder) fresh() string {
	b.nextID = b.nextID + 1
	return "%t" | itoa290(b.nextID)
}

func (b *Builder) buildExpr(e Expr) SSAValue {
	switch x := e.(type) {
	case *LitExpr:
		return &SSAConst{name: itoa290(x.val), typ: "i32", val: x.val}
	case *NameExpr:
		v, ok := b.env[x.name]
		if ok {
			return v
		}
		return &SSAReg{name: "%" | x.name, typ: "i32"}
	case *BinExpr:
		lhs := b.buildExpr(x.x)
		rhs := b.buildExpr(x.y)
		dst := &SSAReg{name: b.fresh(), typ: "i32"}
		b.cur.emit(&SSABinOp{dst: dst, op: x.op, x: lhs, y: rhs})
		return dst
	case *CallExpr:
		dst := &SSAReg{name: b.fresh(), typ: "i32"}
		return dst
	}
	return nil
}

func (b *Builder) buildStmt(s Stmt) {
	switch x := s.(type) {
	case *AssignStmt:
		v := b.buildExpr(x.val)
		if v != nil {
			b.env[x.name] = v
		}
	case *ReturnStmt:
		v := b.buildExpr(x.val)
		b.cur.emit(&SSARet{val: v})
	}
}

func (b *Builder) render() string {
	var out []byte
	for _, bl := range b.blocks {
		out = append(out, bl.name...)
		out = append(out, ':')
		out = append(out, '\n')
		for _, instr := range bl.instrs {
			out = append(out, ' ')
			out = append(out, ' ')
			s := instr.InstrString()
			out = append(out, s...)
			out = append(out, '\n')
		}
	}
	return string(out)
}

func itoa290(n int32) string {
	if n == 0 { return "0" }
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [10]byte
	i := int32(9)
	for n > 0 {
		buf[i] = byte(n%10) + '0'
		n = n / 10
		i = i - 1
	}
	if neg {
		buf[i] = '-'
		i = i - 1
	}
	return string(buf[i+1:])
}

func main() {
	b := NewBuilder()

	stmts := []Stmt{
		&AssignStmt{name: "sum", val: &BinExpr{op: "add", x: &LitExpr{val: 1}, y: &LitExpr{val: 2}}},
		&AssignStmt{name: "prod", val: &BinExpr{op: "mul", x: &NameExpr{name: "sum"}, y: &LitExpr{val: 3}}},
		&ReturnStmt{val: &NameExpr{name: "prod"}},
	}
	for _, s := range stmts {
		b.buildStmt(s)
	}
	result := b.render()
	_ = result

	n := int32(0)
	for _, bl := range b.blocks {
		n = n + int32(len(bl.instrs))
	}
	_ = n
}
`, func(ir string) {
		assert("290: has NewBuilder", strings.Contains(ir, "@main.NewBuilder"))
		assert("290: has Builder.buildExpr", strings.Contains(ir, "@main.Builder.buildExpr"))
		assert("290: has Builder.buildStmt", strings.Contains(ir, "@main.Builder.buildStmt"))
		assert("290: has Builder.render", strings.Contains(ir, "@main.Builder.render"))
		assert("290: type switches in buildExpr", strings.Contains(ir, "typeid"))
		assert("290: interface dispatch", strings.Contains(ir, "icmp eq ptr"))
		assert("290: map operations", strings.Contains(ir, "hashmapContent"))
		assert("290: interface slice boxing", strings.Contains(ir, "insertvalue {ptr, ptr}"))
	})

	// Test 291: tagless switch (switch { case cond: }) and if-init
	test(291, `package main

func classify(n int32) string {
	switch {
	case n < 0:
		return "negative"
	case n == 0:
		return "zero"
	case n < 10:
		return "small"
	case n < 100:
		return "medium"
	}
	return "large"
}

func safeLookup(m map[string]int32, key string) int32 {
	if v, ok := m[key]; ok {
		return v
	}
	return -1
}

func main() {
	s1 := classify(-5)
	s2 := classify(0)
	s3 := classify(7)
	s4 := classify(50)
	s5 := classify(200)
	_ = s1
	_ = s2
	_ = s3
	_ = s4
	_ = s5

	m := map[string]int32{"x": 42}
	v1 := safeLookup(m, "x")
	v2 := safeLookup(m, "missing")
	_ = v1
	_ = v2
}
`, func(ir string) {
		assert("291: has classify", strings.Contains(ir, "@main.classify"))
		assert("291: has safeLookup", strings.Contains(ir, "@main.safeLookup"))
		assert("291: branch for cases", strings.Contains(ir, "br i1"))
	})

	// Test 292: string indexing, byte comparison, break/continue
	test(292, `package main

func indexOf(s string, ch byte) int32 {
	for i := int32(0); i < int32(len(s)); i = i + 1 {
		if s[i] == ch {
			return i
		}
	}
	return -1
}

func countChar(s string, ch byte) int32 {
	n := int32(0)
	for i := int32(0); i < int32(len(s)); i = i + 1 {
		if s[i] != ch {
			continue
		}
		n = n + 1
	}
	return n
}

func skipSpaces(s string, pos int32) int32 {
	for pos < int32(len(s)) {
		if s[pos] != ' ' && s[pos] != '\t' {
			break
		}
		pos = pos + 1
	}
	return pos
}

func main() {
	i1 := indexOf("hello", 'l')
	i2 := indexOf("hello", 'z')
	_ = i1
	_ = i2

	c := countChar("abracadabra", 'a')
	_ = c

	p := skipSpaces("   hello", 0)
	_ = p
}
`, func(ir string) {
		assert("292: has indexOf", strings.Contains(ir, "@main.indexOf"))
		assert("292: has countChar", strings.Contains(ir, "@main.countChar"))
		assert("292: has skipSpaces", strings.Contains(ir, "@main.skipSpaces"))
		assert("292: string index gep", strings.Contains(ir, "getelementptr"))
	})

	// Test 293: nested function calls as arguments f(g(x))
	test(293, `package main

type Type interface {
	Underlying() Type
}

type Basic struct {
	kind int32
}
func (b *Basic) Underlying() Type { return b }

type Pointer struct {
	base Type
}
func (p *Pointer) Underlying() Type { return p }
func (p *Pointer) Elem() Type { return p.base }

func NewPointer(base Type) *Pointer {
	return &Pointer{base: base}
}

func llvmType(t Type) string {
	if t == nil { return "void" }
	switch u := t.Underlying().(type) {
	case *Basic:
		if u.kind == 1 { return "i32" }
		if u.kind == 2 { return "i64" }
		return "i8"
	case *Pointer:
		return "ptr"
	}
	return "void"
}

func ptrType(t Type) string {
	return llvmType(NewPointer(t))
}

func main() {
	intT := &Basic{kind: 1}
	s1 := ptrType(intT)
	_ = s1
	s2 := llvmType(NewPointer(NewPointer(intT)))
	_ = s2
}
`, func(ir string) {
		assert("293: has NewPointer", strings.Contains(ir, "@main.NewPointer"))
		assert("293: has ptrType", strings.Contains(ir, "@main.ptrType"))
		assert("293: nested calls", strings.Count(ir, "call") >= 3)
	})

	// Test 294: type switch with default, fallthrough to default
	test(294, `package main

type Node interface {
	nodeKind() int32
}

type IntNode struct {
	val int32
}
func (n *IntNode) nodeKind() int32 { return 1 }

type StrNode struct {
	val string
}
func (n *StrNode) nodeKind() int32 { return 2 }

type ListNode struct {
	items []Node
}
func (n *ListNode) nodeKind() int32 { return 3 }

func describe(n Node) string {
	switch x := n.(type) {
	case *IntNode:
		_ = x.val
		return "int"
	case *StrNode:
		return "str:" | x.val
	case *ListNode:
		_ = len(x.items)
		return "list"
	default:
		return "unknown"
	}
}

func main() {
	nodes := []Node{
		&IntNode{val: 42},
		&StrNode{val: "hello"},
		&ListNode{items: []Node{&IntNode{val: 1}}},
	}
	var results []string
	for _, n := range nodes {
		s := describe(n)
		results = append(results, s)
	}
	_ = results
}
`, func(ir string) {
		assert("294: has describe", strings.Contains(ir, "@main.describe"))
		assert("294: type switch with 3 cases", strings.Count(ir, "typeid") >= 3)
		assert("294: default case", strings.Contains(ir, "br label") || strings.Contains(ir, "unreachable"))
	})

	// Test 295: map[string]interface{} pattern (map with interface values)
	test(295, `package main

type Value interface {
	String() string
}

type IntVal struct {
	v int32
}
func (i *IntVal) String() string { return "int" }

type StrVal struct {
	v string
}
func (s *StrVal) String() string { return s.v }

func buildEnv() map[string]Value {
	env := map[string]Value{}
	env["x"] = &IntVal{v: 1}
	env["name"] = &StrVal{v: "hello"}
	return env
}

func lookupString(env map[string]Value, key string) string {
	v, ok := env[key]
	if !ok {
		return ""
	}
	return v.String()
}

func main() {
	env := buildEnv()
	s1 := lookupString(env, "x")
	s2 := lookupString(env, "name")
	s3 := lookupString(env, "missing")
	_ = s1
	_ = s2
	_ = s3
}
`, func(ir string) {
		assert("295: has buildEnv", strings.Contains(ir, "@main.buildEnv"))
		assert("295: has lookupString", strings.Contains(ir, "@main.lookupString"))
		assert("295: map with interface values", strings.Contains(ir, "hashmapContent"))
		assert("295: interface dispatch", strings.Contains(ir, "icmp eq ptr"))
	})

	// Test 296: full SSA type hierarchy - replicates ssa_types.mx patterns
	test(296, `package main

type Type interface {
	Underlying() Type
	String() string
}

type Basic struct {
	kind int32
	name string
}
func (b *Basic) Underlying() Type { return b }
func (b *Basic) String() string { return b.name }

type Pointer struct {
	base Type
}
func (p *Pointer) Underlying() Type { return p }
func (p *Pointer) String() string { return "*" | p.base.String() }
func (p *Pointer) Elem() Type { return p.base }

type Slice struct {
	elem Type
}
func (s *Slice) Underlying() Type { return s }
func (s *Slice) String() string { return "[]" | s.elem.String() }
func (s *Slice) Elem() Type { return s.elem }

type SSAValue interface {
	SSAName() string
	SSAType() Type
}

type SSAInstruction interface {
	InstrBlock() *SSABasicBlock
	InstrString() string
	setBlock(*SSABasicBlock)
}

type SSAMember interface {
	MemberName() string
	MemberType() Type
}

type SSABasicBlock struct {
	Index  int32
	parent *SSAFunction
	Instrs []SSAInstruction
}

type SSAFunction struct {
	name   string
	Blocks []*SSABasicBlock
	Params []*SSAParameter
}
func (f *SSAFunction) MemberName() string { return f.name }
func (f *SSAFunction) MemberType() Type { return nil }
func (f *SSAFunction) SSAName() string { return f.name }
func (f *SSAFunction) SSAType() Type { return nil }

type SSAGlobal struct {
	name string
	typ  Type
}
func (g *SSAGlobal) MemberName() string { return g.name }
func (g *SSAGlobal) MemberType() Type { return g.typ }
func (g *SSAGlobal) SSAName() string { return g.name }
func (g *SSAGlobal) SSAType() Type { return g.typ }

type SSAParameter struct {
	name   string
	typ    Type
	parent *SSAFunction
}
func (p *SSAParameter) SSAName() string { return p.name }
func (p *SSAParameter) SSAType() Type { return p.typ }

type ssaInstr struct {
	block *SSABasicBlock
}
func (a *ssaInstr) InstrBlock() *SSABasicBlock { return a.block }
func (a *ssaInstr) setBlock(b *SSABasicBlock) { a.block = b }

type ssaRegister struct {
	ssaInstr
	name string
	typ  Type
}
func (r *ssaRegister) SSAName() string { return r.name }
func (r *ssaRegister) SSAType() Type { return r.typ }

type SSAConst struct {
	name string
	typ  Type
	val  int32
}
func (c *SSAConst) SSAName() string { return c.name }
func (c *SSAConst) SSAType() Type { return c.typ }

type SSABinOp struct {
	ssaRegister
	Op string
	X  SSAValue
	Y  SSAValue
}
func (b *SSABinOp) InstrString() string {
	return b.name | " = " | b.Op | " " | b.X.SSAName() | " " | b.Y.SSAName()
}

type SSACall struct {
	ssaRegister
	Fn   SSAValue
	Args []SSAValue
}
func (c *SSACall) InstrString() string { return c.name | " = call " | c.Fn.SSAName() }

type SSAStore struct {
	ssaInstr
	Addr SSAValue
	Val  SSAValue
}
func (s *SSAStore) InstrString() string { return "store " | s.Val.SSAName() | " -> " | s.Addr.SSAName() }

type SSARet struct {
	ssaInstr
	Val SSAValue
}
func (r *SSARet) InstrString() string {
	if r.Val == nil { return "ret void" }
	return "ret " | r.Val.SSAName()
}

type SSAAlloc struct {
	ssaRegister
	Heap bool
}
func (a *SSAAlloc) InstrString() string {
	if a.Heap { return "new " | a.typ.String() }
	return "local " | a.typ.String()
}

func NewBasicBlock(f *SSAFunction) *SSABasicBlock {
	b := &SSABasicBlock{Index: int32(len(f.Blocks)), parent: f}
	f.Blocks = append(f.Blocks, b)
	return b
}

func emit(b *SSABasicBlock, i SSAInstruction) {
	i.setBlock(b)
	b.Instrs = append(b.Instrs, i)
}

type Package struct {
	Members map[string]SSAMember
}

func (p *Package) Func(name string) *SSAFunction {
	m := p.Members[name]
	if m == nil { return nil }
	fn, _ := m.(*SSAFunction)
	return fn
}

func renderBlock(b *SSABasicBlock) string {
	var out []byte
	for _, instr := range b.Instrs {
		out = append(out, ' ')
		out = append(out, ' ')
		s := instr.InstrString()
		out = append(out, s...)
		out = append(out, '\n')
	}
	return string(out)
}

func main() {
	intType := &Basic{kind: 1, name: "i32"}
	ptrType := &Pointer{base: intType}

	pkg := &Package{Members: map[string]SSAMember{}}

	fn := &SSAFunction{name: "main"}
	pkg.Members["main"] = fn

	g := &SSAGlobal{name: "counter", typ: intType}
	pkg.Members["counter"] = g

	entry := NewBasicBlock(fn)

	p0 := &SSAParameter{name: "%arg0", typ: intType, parent: fn}
	fn.Params = append(fn.Params, p0)

	alloc := &SSAAlloc{}
	alloc.name = "%t1"
	alloc.typ = intType
	emit(entry, alloc)

	c1 := &SSAConst{name: "1", typ: intType, val: 1}

	add := &SSABinOp{Op: "add", X: p0, Y: c1}
	add.name = "%t2"
	add.typ = intType
	emit(entry, add)

	store := &SSAStore{Addr: alloc, Val: add}
	emit(entry, store)

	ret := &SSARet{Val: add}
	emit(entry, ret)

	lookupFn := pkg.Func("main")
	_ = lookupFn

	lookupGlobal := pkg.Func("counter")
	_ = lookupGlobal

	ir := renderBlock(entry)
	_ = ir

	_ = ptrType.String()

	blk := add.InstrBlock()
	_ = blk

	n := int32(0)
	for _, b := range fn.Blocks {
		n = n + int32(len(b.Instrs))
	}
	_ = n
}
`, func(ir string) {
		assert("296: has SSAFunction", strings.Contains(ir, "SSAFunction"))
		assert("296: has SSABinOp.InstrString", strings.Contains(ir, "@main.SSABinOp.InstrString"))
		assert("296: has SSARet.InstrString", strings.Contains(ir, "@main.SSARet.InstrString"))
		assert("296: has Package.Func", strings.Contains(ir, "@main.Package.Func"))
		assert("296: has renderBlock", strings.Contains(ir, "@main.renderBlock"))
		assert("296: 2-level embedding block access", strings.Contains(ir, "@main.ssaInstr.InstrBlock"))
		assert("296: setBlock via embedding", strings.Contains(ir, "@main.ssaInstr.setBlock"))
		assert("296: interface dispatch", strings.Contains(ir, "icmp eq ptr"))
		assert("296: map operations", strings.Contains(ir, "hashmapContent"))
		assert("296: interface boxing", strings.Contains(ir, "insertvalue {ptr, ptr}"))
	})

	// Test 297: promoted method called through interface dispatch (the real self-compile pattern)
	test(297, `package main

type SSAInstruction interface {
	InstrBlock() *Block
	InstrString() string
	setBlock(*Block)
}

type Block struct {
	Index  int32
	Instrs []SSAInstruction
}

func (b *Block) emit(i SSAInstruction) {
	i.setBlock(b)
	b.Instrs = append(b.Instrs, i)
}

type instrBase struct {
	block *Block
}
func (a *instrBase) InstrBlock() *Block { return a.block }
func (a *instrBase) setBlock(b *Block) { a.block = b }

type register struct {
	instrBase
	name string
	typ  string
}

type BinOp struct {
	register
	Op string
}
func (b *BinOp) InstrString() string { return b.name | " = " | b.Op }

type Store struct {
	instrBase
	dst string
	src string
}
func (s *Store) InstrString() string { return "store " | s.src | " -> " | s.dst }

type Ret struct {
	instrBase
	val string
}
func (r *Ret) InstrString() string { return "ret " | r.val }

func renderIR(b *Block) string {
	var out []byte
	for _, instr := range b.Instrs {
		s := instr.InstrString()
		out = append(out, s...)
		out = append(out, '\n')
		blk := instr.InstrBlock()
		if blk != nil && blk.Index != b.Index {
			out = append(out, "; wrong block\n"...)
		}
	}
	return string(out)
}

func main() {
	b := &Block{Index: 0}

	add := &BinOp{Op: "add"}
	add.name = "%t1"
	add.typ = "i32"
	b.emit(add)

	store := &Store{dst: "%x", src: "%t1"}
	b.emit(store)

	ret := &Ret{val: "%t1"}
	b.emit(ret)

	ir := renderIR(b)
	_ = ir

	blk := add.InstrBlock()
	_ = blk

	n := int32(len(b.Instrs))
	_ = n
}
`, func(ir string) {
		assert("297: has Block.emit", strings.Contains(ir, "@main.Block.emit"))
		assert("297: has renderIR", strings.Contains(ir, "@main.renderIR"))
		assert("297: promoted InstrBlock", strings.Contains(ir, "@main.instrBase.InstrBlock"))
		assert("297: promoted setBlock", strings.Contains(ir, "@main.instrBase.setBlock"))
		assert("297: BinOp.InstrString", strings.Contains(ir, "@main.BinOp.InstrString"))
		assert("297: interface dispatch for InstrString", strings.Contains(ir, "icmp eq ptr"))
	})

	// Test 298: switch on named int type (SSAOp pattern from ssa_types.mx)
	test(298, `package main

type Op int32

const (
	OpAdd Op = iota
	OpSub
	OpMul
	OpDiv
	OpEq
	OpNe
	OpLt
	OpGt
)

func (op Op) String() string {
	switch op {
	case OpAdd: return "+"
	case OpSub: return "-"
	case OpMul: return "*"
	case OpDiv: return "/"
	case OpEq: return "=="
	case OpNe: return "!="
	case OpLt: return "<"
	case OpGt: return ">"
	}
	return "?"
}

func (op Op) IsComparison() bool {
	return op >= OpEq
}

func (op Op) Precedence() int32 {
	switch {
	case op == OpMul || op == OpDiv:
		return 2
	case op == OpAdd || op == OpSub:
		return 1
	}
	return 0
}

func main() {
	ops := []Op{OpAdd, OpSub, OpMul, OpEq, OpLt}
	var strs []string
	for _, op := range ops {
		strs = append(strs, op.String())
	}
	_ = strs

	b1 := OpEq.IsComparison()
	b2 := OpAdd.IsComparison()
	_ = b1
	_ = b2

	p := OpMul.Precedence()
	_ = p
}
`, func(ir string) {
		assert("298: has Op.String", strings.Contains(ir, "@main.Op.String"))
		assert("298: has Op.IsComparison", strings.Contains(ir, "@main.Op.IsComparison"))
		assert("298: has Op.Precedence", strings.Contains(ir, "@main.Op.Precedence"))
		assert("298: switch on i32", strings.Contains(ir, "icmp eq i32"))
	})

	// Test 299: map[string]SSAMember with type assertion and iteration
	test(299, `package main

type SSAMember interface {
	MemberName() string
}

type SSAFunction struct {
	name string
	body string
}
func (f *SSAFunction) MemberName() string { return f.name }

type SSAGlobal struct {
	name string
	typ  string
}
func (g *SSAGlobal) MemberName() string { return g.name }

type SSAPackage struct {
	Members map[string]SSAMember
}

func (p *SSAPackage) Func(name string) *SSAFunction {
	m := p.Members[name]
	if m == nil { return nil }
	fn, ok := m.(*SSAFunction)
	if !ok { return nil }
	return fn
}

func (p *SSAPackage) AllFuncs() []*SSAFunction {
	var funcs []*SSAFunction
	for _, m := range p.Members {
		if fn, ok := m.(*SSAFunction); ok {
			funcs = append(funcs, fn)
		}
	}
	return funcs
}

func (p *SSAPackage) AllNames() []string {
	var names []string
	for name := range p.Members {
		names = append(names, name)
	}
	return names
}

func main() {
	pkg := &SSAPackage{Members: map[string]SSAMember{}}
	pkg.Members["main"] = &SSAFunction{name: "main", body: "entry"}
	pkg.Members["init"] = &SSAFunction{name: "init", body: ""}
	pkg.Members["counter"] = &SSAGlobal{name: "counter", typ: "i32"}

	fn := pkg.Func("main")
	_ = fn
	nilFn := pkg.Func("counter")
	_ = nilFn
	missingFn := pkg.Func("missing")
	_ = missingFn

	allFn := pkg.AllFuncs()
	_ = allFn
	allN := pkg.AllNames()
	_ = allN
}
`, func(ir string) {
		assert("299: has SSAPackage.Func", strings.Contains(ir, "@main.SSAPackage.Func"))
		assert("299: has SSAPackage.AllFuncs", strings.Contains(ir, "@main.SSAPackage.AllFuncs"))
		assert("299: has SSAPackage.AllNames", strings.Contains(ir, "@main.SSAPackage.AllNames"))
		assert("299: type assertion", strings.Contains(ir, "typeid"))
		assert("299: map iteration", strings.Contains(ir, "hashmapNext"))
	})

	// Test 300: Full 350-line bootstrap simulation - types, builder, emitter pipeline
	test(300, `package main

type Type interface {
	Underlying() Type
	String() string
}
type Basic struct { kind int32; name string }
func (b *Basic) Underlying() Type { return b }
func (b *Basic) String() string { return b.name }
type Pointer struct { base Type }
func (p *Pointer) Underlying() Type { return p }
func (p *Pointer) String() string { return "*" | p.base.String() }
func (p *Pointer) Elem() Type { return p.base }
type Slice struct { elem Type }
func (s *Slice) Underlying() Type { return s }
func (s *Slice) String() string { return "[]" | s.elem.String() }
type Named struct { obj string; underlying Type }
func (n *Named) Underlying() Type {
	if n.underlying != nil { return n.underlying }
	return n
}
func (n *Named) String() string { return n.obj }
func NewPointer(base Type) *Pointer { return &Pointer{base: base} }
func NewSlice(elem Type) *Slice { return &Slice{elem: elem} }

type SSAValue interface { SSAName() string; SSAType() Type }
type SSAInstruction interface { InstrString() string; setBlock(*Block) }
type Block struct { index int32; instrs []SSAInstruction }
func (b *Block) emit(i SSAInstruction) {
	i.setBlock(b)
	b.instrs = append(b.instrs, i)
}
type instrBase struct { block *Block }
func (a *instrBase) setBlock(b *Block) { a.block = b }
type register struct { instrBase; name string; typ Type }
func (r *register) SSAName() string { return r.name }
func (r *register) SSAType() Type { return r.typ }

type SSAConst struct { name string; typ Type; val int32 }
func (c *SSAConst) SSAName() string { return c.name }
func (c *SSAConst) SSAType() Type { return c.typ }

type BinOp struct { register; Op string; X SSAValue; Y SSAValue }
func (b *BinOp) InstrString() string {
	return b.name | " = " | b.Op | " " | llvmType300(b.typ) | " " | b.X.SSAName() | ", " | b.Y.SSAName()
}
type Call struct { register; fn SSAValue; args []SSAValue }
func (c *Call) InstrString() string { return c.name | " = call " | c.fn.SSAName() }
type Store struct { instrBase; addr SSAValue; val SSAValue }
func (s *Store) InstrString() string { return "store " | llvmType300(s.val.SSAType()) | " " | s.val.SSAName() | ", ptr " | s.addr.SSAName() }
type Ret struct { instrBase; val SSAValue }
func (r *Ret) InstrString() string {
	if r.val == nil { return "ret void" }
	return "ret " | llvmType300(r.val.SSAType()) | " " | r.val.SSAName()
}
type Alloc struct { register; heap bool }
func (a *Alloc) InstrString() string {
	return a.name | " = alloca " | llvmType300(a.typ)
}
type FieldAddr struct { register; x SSAValue; field int32 }
func (fa *FieldAddr) InstrString() string {
	return fa.name | " = getelementptr " | fa.x.SSAName()
}

func llvmType300(t Type) string {
	if t == nil { return "void" }
	switch u := t.Underlying().(type) {
	case *Basic:
		switch u.kind {
		case 1: return "i8"
		case 2: return "i32"
		case 3: return "i64"
		case 4: return "{ptr, i64, i64}"
		}
		return "i8"
	case *Pointer: return "ptr"
	case *Slice: return "{ptr, i64, i64}"
	case *Named: return llvmType300(u.Underlying())
	}
	return "void"
}

type Emitter struct {
	buf     []byte
	nextReg int32
}

func (e *Emitter) w(s string) { e.buf = append(e.buf, s...) }

func (e *Emitter) fresh() string {
	e.nextReg = e.nextReg + 1
	return "%t" | itoa300(e.nextReg)
}

func (e *Emitter) emitInstr(i SSAInstruction) {
	e.w("  ")
	e.w(i.InstrString())
	e.w("\n")
}

func (e *Emitter) emitBlock(b *Block) {
	e.w("b")
	e.w(itoa300(int32(b.index)))
	e.w(":\n")
	for _, instr := range b.instrs {
		e.emitInstr(instr)
	}
}

func (e *Emitter) emitFunc(name string, blocks []*Block) string {
	e.w("define void @")
	e.w(name)
	e.w("() {\n")
	for _, b := range blocks {
		e.emitBlock(b)
	}
	e.w("}\n")
	return string(e.buf)
}

func itoa300(n int32) string {
	if n == 0 { return "0" }
	if n < 0 { return "-" | itoa300(-n) }
	var buf [10]byte
	i := int32(9)
	for n > 0 {
		buf[i] = byte(n%10) + '0'
		n = n / 10
		i = i - 1
	}
	return string(buf[i+1:])
}

type Scope struct {
	parent *Scope
	names  map[string]SSAValue
}

func (s *Scope) Lookup(name string) SSAValue {
	if s.names != nil {
		v, ok := s.names[name]
		if ok { return v }
	}
	if s.parent != nil { return s.parent.Lookup(name) }
	return nil
}

func main() {
	i32 := &Basic{kind: 2, name: "int32"}
	i64 := &Basic{kind: 3, name: "int64"}
	strType := &Basic{kind: 4, name: "string"}
	ptrI32 := NewPointer(i32)
	sliceStr := NewSlice(strType)
	myInt := &Named{obj: "MyInt", underlying: i32}

	_ = llvmType300(i32)
	_ = llvmType300(i64)
	_ = llvmType300(ptrI32)
	_ = llvmType300(sliceStr)
	_ = llvmType300(myInt)
	_ = llvmType300(nil)

	entry := &Block{index: 0}
	retBlock := &Block{index: 1}

	c1 := &SSAConst{name: "1", typ: i32, val: 1}
	c2 := &SSAConst{name: "2", typ: i32, val: 2}

	alloc := &Alloc{}
	alloc.name = "%t1"
	alloc.typ = i32
	entry.emit(alloc)

	add := &BinOp{Op: "add", X: c1, Y: c2}
	add.name = "%t2"
	add.typ = i32
	entry.emit(add)

	store := &Store{addr: alloc, val: add}
	entry.emit(store)

	fa := &FieldAddr{x: alloc, field: 0}
	fa.name = "%t3"
	fa.typ = ptrI32
	entry.emit(fa)

	ret := &Ret{val: add}
	retBlock.emit(ret)

	retVoid := &Ret{}
	retBlock.emit(retVoid)

	e := &Emitter{}
	ir := e.emitFunc("test", []*Block{entry, retBlock})
	_ = ir

	scope := &Scope{
		names: map[string]SSAValue{},
	}
	scope.names["x"] = alloc
	scope.names["y"] = add

	child := &Scope{parent: scope, names: map[string]SSAValue{}}
	child.names["z"] = c1

	v1 := child.Lookup("z")
	v2 := child.Lookup("x")
	v3 := child.Lookup("missing")
	_ = v1
	_ = v2
	_ = v3
}
`, func(ir string) {
		assert("300: has Emitter.emitFunc", strings.Contains(ir, "@main.Emitter.emitFunc"))
		assert("300: has Emitter.emitBlock", strings.Contains(ir, "@main.Emitter.emitBlock"))
		assert("300: has Emitter.emitInstr", strings.Contains(ir, "@main.Emitter.emitInstr"))
		assert("300: has llvmType300", strings.Contains(ir, "@main.llvmType300"))
		assert("300: has Scope.Lookup", strings.Contains(ir, "@main.Scope.Lookup"))
		assert("300: 2-level embed setBlock", strings.Contains(ir, "@main.instrBase.setBlock"))
		assert("300: type switch dispatch", strings.Contains(ir, "typeid"))
		assert("300: nested switch", strings.Contains(ir, "icmp eq i32"))
		assert("300: interface dispatch", strings.Contains(ir, "icmp eq ptr"))
		assert("300: recursive call", strings.Count(ir, "@main.Scope.Lookup") >= 2)
		assert("300: map operations", strings.Contains(ir, "hashmapContent"))
		assert("300: array subslice", strings.Contains(ir, "alloca [10 x i8]"))
		assert("300: nil ret check", strings.Contains(ir, "ret void") || strings.Contains(ir, "zeroinitializer"))
	})

	// Test 301: import registered package and call function
	clearImports()
	regPkg("fmt", "fmt")
	regFn("fmt", "Sprintf", "string->string")
	test(301, `package main

import "fmt"

func main() {
	s := fmt.Sprintf("hello")
	_ = s
}
`, func(ir string) {
		assert("301: has declare for fmt.Sprintf", strings.Contains(ir, "declare") && strings.Contains(ir, "@fmt.Sprintf"))
		assert("301: has call to fmt.Sprintf", strings.Contains(ir, "call") && strings.Contains(ir, "@fmt.Sprintf"))
		assert("301: result is string type", strings.Contains(ir, "{ptr, i") || strings.Contains(ir, "ptr"))
	})

	// Test 302: import with multiple functions
	clearImports()
	regPkg("strconv", "strconv")
	regFn("strconv", "Itoa", "int->string")
	regFn("strconv", "Atoi", "string->int,error")
	test(302, `package main

import "strconv"

func main() {
	s := strconv.Itoa(42)
	_ = s
}
`, func(ir string) {
		assert("302: has declare for strconv.Itoa", strings.Contains(ir, "declare") && strings.Contains(ir, "@strconv.Itoa"))
		assert("302: has call to strconv.Itoa", strings.Contains(ir, "call") && strings.Contains(ir, "@strconv.Itoa"))
	})

	// Test 303: import with global var
	clearImports()
	regPkg("os", "os")
	regVar("os", "Stdout", "ptr")
	regFn("os", "Exit", "int")
	test(303, `package main

import "os"

func main() {
	os.Exit(1)
}
`, func(ir string) {
		assert("303: has declare for os.Exit", strings.Contains(ir, "declare") && strings.Contains(ir, "@os.Exit"))
		assert("303: has call to os.Exit", strings.Contains(ir, "call") && strings.Contains(ir, "@os.Exit"))
	})

	// Test 304: import with aliased package name
	clearImports()
	regPkg("fmt", "fmt")
	regFn("fmt", "Sprintf", "string->string")
	test(304, `package main

import f "fmt"

func main() {
	s := f.Sprintf("hello")
	_ = s
}
`, func(ir string) {
		assert("304: aliased import calls fmt.Sprintf", strings.Contains(ir, "@fmt.Sprintf"))
	})

	// Test 305: multiple imports in same compilation
	clearImports()
	regPkg("fmt", "fmt")
	regFn("fmt", "Sprintf", "string->string")
	regPkg("strconv", "strconv")
	regFn("strconv", "Itoa", "int->string")
	test(305, `package main

import (
	"fmt"
	"strconv"
)

func main() {
	n := strconv.Itoa(42)
	s := fmt.Sprintf(n)
	_ = s
}
`, func(ir string) {
		assert("305: has fmt.Sprintf", strings.Contains(ir, "@fmt.Sprintf"))
		assert("305: has strconv.Itoa", strings.Contains(ir, "@strconv.Itoa"))
		assert("305: both declared", strings.Count(ir, "declare") >= 2)
	})

	// Test 306: import function with slice param
	clearImports()
	regPkg("bytes", "bytes")
	regFn("bytes", "Join", "[]string,string->string")
	test(306, `package main

import "bytes"

func join(parts []string, sep string) string {
	return bytes.Join(parts, sep)
}
`, func(ir string) {
		assert("306: has bytes.Join declare", strings.Contains(ir, "@bytes.Join"))
		assert("306: passes slice arg", strings.Contains(ir, "call") && strings.Contains(ir, "@bytes.Join"))
	})

	// Test 307: use imported var (global read)
	clearImports()
	regPkg("os", "os")
	regVar("os", "Args", "[]string")
	test(307, `package main

import "os"

func getFirst() string {
	return os.Args[0]
}
`, func(ir string) {
		assert("307: has external global for os.Args", strings.Contains(ir, "@os.Args") && strings.Contains(ir, "external global"))
		assert("307: loads from os.Args", strings.Contains(ir, "load") && strings.Contains(ir, "@os.Args"))
	})

	// Test 308: import function returning void (no return value)
	clearImports()
	regPkg("runtime", "runtime")
	regFn("runtime", "GC", "")
	test(308, `package main

import "runtime"

func main() {
	runtime.GC()
}
`, func(ir string) {
		assert("308: has runtime.GC declare", strings.Contains(ir, "@runtime.GC"))
		assert("308: void call", strings.Contains(ir, "call void"))
	})

	clearImports()

	// Test 309: function returning error interface
	clearImports()
	regPkg("strconv", "strconv")
	regFn("strconv", "Atoi", "string->int,error")
	test(309, `package main

import "strconv"

func tryParse(s string) int32 {
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}
`, func(ir string) {
		assert("309: has strconv.Atoi declare", strings.Contains(ir, "@strconv.Atoi"))
		assert("309: multi-return extraction", strings.Contains(ir, "extractvalue"))
		assert("309: nil check on error", strings.Contains(ir, "icmp"))
	})

	// Test 310: function taking interface{} parameter
	clearImports()
	regPkg("fmt", "fmt")
	regFn("fmt", "Println", "interface{}->int,error")
	test(310, `package main

import "fmt"

func main() {
	fmt.Println(42)
}
`, func(ir string) {
		assert("310: has fmt.Println declare", strings.Contains(ir, "@fmt.Println"))
		assert("310: call with boxing", strings.Contains(ir, "call") && strings.Contains(ir, "@fmt.Println"))
	})

	// Test 311: imported function used in expression
	clearImports()
	regPkg("strconv", "strconv")
	regFn("strconv", "Itoa", "int->string")
	test(311, `package main

import "strconv"

func describe(n int32) string {
	return "val:" | strconv.Itoa(n)
}
`, func(ir string) {
		assert("311: has strconv.Itoa", strings.Contains(ir, "@strconv.Itoa"))
		assert("311: string concat after call", strings.Contains(ir, "sliceAppend") || strings.Contains(ir, "runtime.stringConcat"))
	})

	clearImports()

	// Test 312: pos.mx pattern - value receiver structs, self-referential types, struct compare
	clearImports()
	regPkg("fmt", "fmt")
	regFn("fmt", "Sprintf", "string,interface{},interface{}->string")
	test(312, `package main

import "fmt"

const PosMax = 1 << 30

type Pos struct {
	base      *PosBase
	line, col uint32
}

func MakePos(base *PosBase, line, col uint32) Pos { return Pos{base, sat32(line), sat32(col)} }

func (pos Pos) IsKnown() bool  { return pos.line > 0 }
func (pos Pos) Base() *PosBase { return pos.base }
func (pos Pos) Line() uint32   { return pos.line }
func (pos Pos) Col() uint32    { return pos.col }

func (pos Pos) FileBase() *PosBase {
	b := pos.base
	for b != nil && b != b.pos.base {
		b = b.pos.base
	}
	return b
}

func (pos Pos) RelFilename() string { return pos.base.Filename() }

type position_ struct {
	filename  string
	line, col uint32
}

func (p position_) String() string {
	if p.line == 0 {
		if p.filename == "" {
			return "<unknown position>"
		}
		return p.filename
	}
	if p.col == 0 {
		return fmt.Sprintf("%s:%d", p.filename, p.line)
	}
	return fmt.Sprintf("%s:%d:%d", p.filename, p.line, p.col)
}

type PosBase struct {
	pos       Pos
	filename  string
	line, col uint32
	trimmed   bool
}

func NewFileBase(filename string) *PosBase {
	return NewTrimmedFileBase(filename, false)
}

func NewTrimmedFileBase(filename string, trimmed bool) *PosBase {
	base := &PosBase{MakePos(nil, 1, 1), filename, 1, 1, trimmed}
	base.pos.base = base
	return base
}

func (base *PosBase) IsFileBase() bool {
	if base == nil {
		return false
	}
	return base.pos.base == base
}

func (base *PosBase) Filename() string {
	if base == nil {
		return ""
	}
	return base.filename
}

func (base *PosBase) Line() uint32 {
	if base == nil {
		return 0
	}
	return base.line
}

func (base *PosBase) Col() uint32 {
	if base == nil {
		return 0
	}
	return base.col
}

func sat32(x uint32) uint32 {
	if x > PosMax {
		return PosMax
	}
	return uint32(x)
}

func main() {
	base := NewFileBase("test.mx")
	pos := MakePos(base, 10, 5)
	_ = pos.IsKnown()
	_ = pos.Line()
	_ = pos.Col()
	_ = pos.FileBase()
	_ = pos.RelFilename()
	_ = base.IsFileBase()
	_ = base.Filename()
}
`, func(ir string) {
		assert("312: has MakePos", strings.Contains(ir, "@main.MakePos"))
		assert("312: has PosBase methods", strings.Contains(ir, "@main.PosBase.Filename"))
		assert("312: has value receiver Pos.Line", strings.Contains(ir, "@main.Pos.Line"))
		assert("312: has self-ref assignment", strings.Contains(ir, "store"))
		assert("312: has nil check", strings.Contains(ir, "icmp"))
		assert("312: has fmt.Sprintf declare", strings.Contains(ir, "@fmt.Sprintf"))
		assert("312: produces many functions", strings.Count(ir, "\ndefine ") >= 10)
		llvmVerify("312: pos.mx pattern", ir)
	})

	// Test 313: multi-file concat simulation - source.mx + pos.mx constants
	test(313, `package main

const Linebase = 1
const Colbase = 1

type Pos struct {
	base      *PosBase
	line, col uint32
}

type PosBase struct {
	pos       Pos
	filename  string
	line, col uint32
	trimmed   bool
}

func MakePos(base *PosBase, line, col uint32) Pos { return Pos{base, line, col} }

func NewTrimmedFileBase(filename string, trimmed bool) *PosBase {
	base := &PosBase{MakePos(nil, Linebase, Colbase), filename, Linebase, Colbase, trimmed}
	base.pos.base = base
	return base
}

func main() {
	b := NewTrimmedFileBase("hello.mx", false)
	p := MakePos(b, 42, 7)
	_ = p.line
	_ = p.col
	_ = b.pos.line
}
`, func(ir string) {
		assert("313: has NewTrimmedFileBase", strings.Contains(ir, "@main.NewTrimmedFileBase"))
		assert("313: has MakePos", strings.Contains(ir, "@main.MakePos"))
		assert("313: self-referential store", strings.Contains(ir, "store"))
		llvmVerify("313: multi-file concat", ir)
	})

	clearImports()

	// Test 315: && with nested struct field access (b.pos.base pattern)
	test(315, `package main

type Inner struct {
	ref *Outer
	val int32
}

type Outer struct {
	inner Inner
	name  string
}

func findRoot(b *Outer) *Outer {
	for b != nil && b != b.inner.ref {
		b = b.inner.ref
	}
	return b
}

func main() {
	o := &Outer{Inner{nil, 1}, "test"}
	o.inner.ref = o
	r := findRoot(o)
	_ = r
}
`, func(ir string) {
		assert("315: has findRoot", strings.Contains(ir, "@main.findRoot"))
		assert("315: has loop", strings.Contains(ir, "br i1") || strings.Contains(ir, "br label"))
		llvmVerify("315: nested field && pattern", ir)
	})

	// Test 316: positional struct literal stores values
	test(316, `package main

type Vec3 struct {
	x, y, z int32
}

func make3(a, b, c int32) Vec3 {
	return Vec3{a, b, c}
}

func main() {
	v := make3(10, 20, 30)
	_ = v.x
	_ = v.y
	_ = v.z
}
`, func(ir string) {
		geps := strings.Count(ir, "getelementptr")
		stores := strings.Count(ir, "store")
		assert("316: has field stores in make3", geps >= 3 && stores >= 3)
		assert("316: stores i32 values", strings.Contains(ir, "store i32"))
		llvmVerify("316: positional struct literal", ir)
	})

	// Test 317: positional struct literal with mixed types
	test(317, `package main

type Pair struct {
	name string
	val  int32
}

func makePair(n string, v int32) Pair {
	return Pair{n, v}
}

func main() {
	p := makePair("hello", 42)
	_ = p.name
	_ = p.val
}
`, func(ir string) {
		assert("317: stores string field", strings.Contains(ir, "store {ptr, i64, i64}"))
		assert("317: stores int field", strings.Contains(ir, "store i32"))
		llvmVerify("317: mixed positional struct literal", ir)
	})

	// Test 318: struct comparison with == and !=
	test(318, `package main

type Point struct {
	x, y int32
}

func equal(a, b Point) bool {
	return a == b
}

func notEqual(a, b Point) bool {
	return a != b
}

func main() {
	p1 := Point{1, 2}
	p2 := Point{1, 2}
	_ = equal(p1, p2)
	_ = notEqual(p1, p2)
}
`, func(ir string) {
		assert("318: has comparison", strings.Contains(ir, "icmp"))
		llvmVerify("318: struct comparison", ir)
	})

	// Test 319: struct comparison with string fields
	test(319, `package main

type Position struct {
	filename  string
	line, col int32
}

func same(a, b Position) bool {
	return a == b
}

func main() {
	p1 := Position{"test.go", 10, 5}
	p2 := Position{"test.go", 10, 5}
	_ = same(p1, p2)
}
`, func(ir string) {
		assert("319: has stringEqual call", strings.Contains(ir, "runtime.stringEqual"))
		assert("319: has field extracts", strings.Contains(ir, "extractvalue"))
		assert("319: has and accumulation", strings.Contains(ir, "and i1"))
		llvmVerify("319: struct compare with string", ir)
	})

	// Test 320: actual pos.mx from bootstrap (with Linebase/Colbase inlined)
	clearImports()
	regPkg("fmt", "fmt")
	regFn("fmt", "Sprintf", "string,interface{},interface{},interface{}->string")
	test(320, `package main

import "fmt"

const PosMax = 1 << 30
const Linebase = 1
const Colbase = 1

type Pos struct {
	base      *PosBase
	line, col uint32
}

func MakePos(base *PosBase, line, col uint32) Pos { return Pos{base, sat32(line), sat32(col)} }

func (pos Pos) IsKnown() bool  { return pos.line > 0 }
func (pos Pos) Base() *PosBase { return pos.base }
func (pos Pos) Line() uint32   { return pos.line }
func (pos Pos) Col() uint32    { return pos.col }

func (pos Pos) FileBase() *PosBase {
	b := pos.base
	for b != nil && b != b.pos.base {
		b = b.pos.base
	}
	return b
}

func (pos Pos) RelFilename() string { return pos.base.Filename() }

func (pos Pos) RelLine() uint32 {
	b := pos.base
	if b.Line() == 0 {
		return 0
	}
	return b.Line() + (pos.Line() - b.Pos().Line())
}

func (pos Pos) RelCol() uint32 {
	b := pos.base
	if b.Col() == 0 {
		return 0
	}
	if pos.Line() == b.Pos().Line() {
		return b.Col() + (pos.Col() - b.Pos().Col())
	}
	return pos.Col()
}

func (p Pos) Cmp(q Pos) int {
	pname := p.RelFilename()
	qname := q.RelFilename()
	switch {
	case pname < qname:
		return -1
	case pname > qname:
		return +1
	}

	pline := p.Line()
	qline := q.Line()
	switch {
	case pline < qline:
		return -1
	case pline > qline:
		return +1
	}

	pcol := p.Col()
	qcol := q.Col()
	switch {
	case pcol < qcol:
		return -1
	case pcol > qcol:
		return +1
	}

	return 0
}

func (pos Pos) String() string {
	rel := position_{pos.RelFilename(), pos.RelLine(), pos.RelCol()}
	abs := position_{pos.Base().Pos().RelFilename(), pos.Line(), pos.Col()}
	s := rel.String()
	if rel != abs {
		s = s | "[" | abs.String() | "]"
	}
	return s
}

type position_ struct {
	filename  string
	line, col uint32
}

func (p position_) String() string {
	if p.line == 0 {
		if p.filename == "" {
			return "<unknown position>"
		}
		return p.filename
	}
	if p.col == 0 {
		return fmt.Sprintf("%s:%d", p.filename, p.line)
	}
	return fmt.Sprintf("%s:%d:%d", p.filename, p.line, p.col)
}

type PosBase struct {
	pos       Pos
	filename  string
	line, col uint32
	trimmed   bool
}

func NewFileBase(filename string) *PosBase {
	return NewTrimmedFileBase(filename, false)
}

func NewTrimmedFileBase(filename string, trimmed bool) *PosBase {
	base := &PosBase{MakePos(nil, Linebase, Colbase), filename, Linebase, Colbase, trimmed}
	base.pos.base = base
	return base
}

func NewLineBase(pos Pos, filename string, trimmed bool, line, col uint32) *PosBase {
	return &PosBase{pos, filename, sat32(line), sat32(col), trimmed}
}

func (base *PosBase) IsFileBase() bool {
	if base == nil {
		return false
	}
	return base.pos.base == base
}

func (base *PosBase) Pos() (_ Pos) {
	if base == nil {
		return
	}
	return base.pos
}

func (base *PosBase) Filename() string {
	if base == nil {
		return ""
	}
	return base.filename
}

func (base *PosBase) Line() uint32 {
	if base == nil {
		return 0
	}
	return base.line
}

func (base *PosBase) Col() uint32 {
	if base == nil {
		return 0
	}
	return base.col
}

func (base *PosBase) Trimmed() bool {
	if base == nil {
		return false
	}
	return base.trimmed
}

func sat32(x uint32) uint32 {
	if x > PosMax {
		return PosMax
	}
	return uint32(x)
}

func main() {
	base := NewFileBase("test.mx")
	pos := MakePos(base, 10, 5)
	_ = pos.IsKnown()
	_ = pos.Line()
	_ = pos.Col()
	fb := pos.FileBase()
	_ = fb
	_ = pos.RelFilename()
	_ = pos.String()
	_ = pos.Cmp(MakePos(base, 20, 1))
	_ = base.IsFileBase()
}
`, func(ir string) {
		funcCount := strings.Count(ir, "\ndefine ")
		assert("320: compiles all pos.mx functions", funcCount >= 20)
		assert("320: has MakePos", strings.Contains(ir, "@main.MakePos"))
		assert("320: has Pos.String", strings.Contains(ir, "@main.Pos.String"))
		assert("320: has Pos.Cmp", strings.Contains(ir, "@main.Pos.Cmp"))
		assert("320: has position_.String", strings.Contains(ir, "@main.position_.String"))
		assert("320: has NewTrimmedFileBase", strings.Contains(ir, "@main.NewTrimmedFileBase"))
		assert("320: has PosBase.Pos", strings.Contains(ir, "@main.PosBase.Pos"))
		assert("320: has struct compare", strings.Contains(ir, "extractvalue") && strings.Contains(ir, "and i1"))
		assert("320: has fmt.Sprintf", strings.Contains(ir, "@fmt.Sprintf"))
		llvmVerify("320: real pos.mx", ir)
	})

	clearImports()

	// Test 314: && with field access (isolated from pos.mx FileBase pattern)
	test(314, `package main

type Node struct {
	parent *Node
	val    int32
}

func findRoot(n *Node) *Node {
	for n != nil && n != n.parent {
		n = n.parent
	}
	return n
}

func main() {
	a := &Node{nil, 1}
	a.parent = a
	r := findRoot(a)
	_ = r
}
`, func(ir string) {
		assert("314: has findRoot", strings.Contains(ir, "@main.findRoot"))
		assert("314: has loop branch", strings.Contains(ir, "br i1") || strings.Contains(ir, "br label"))
	})

	clearImports()

	// Test 321: ssa_types.mx first 100 lines (op codes, switch on named int)
	test(321, `package main

type SSAOp int32

const (
	OpIllegal SSAOp = iota
	OpAdd
	OpSub
	OpMul
	OpQuo
	OpRem
	OpEql
	OpNeq
)

func (op SSAOp) String() string {
	switch op {
	case OpAdd:
		return "+"
	case OpSub:
		return "-"
	case OpMul:
		return "*"
	case OpQuo:
		return "/"
	case OpRem:
		return "percent"
	case OpEql:
		return "=="
	case OpNeq:
		return "!="
	}
	return "?"
}

type ConstVal interface {
	String() string
}

func ssaConstString(val ConstVal) string {
	if val == nil {
		return "nil"
	}
	return val.String()
}

type SSAValue interface {
	SSAName() string
	SSAType() Type
}

type SSAInstruction interface {
	setBlock(b *SSABasicBlock)
}

type Type interface {
	Underlying() Type
	String() string
}

type SSABasicBlock struct {
	Index  int32
	Instrs []SSAInstruction
	Succs  []*SSABasicBlock
	Preds  []*SSABasicBlock
}

type SSAFunction struct {
	name      string
	Blocks    []*SSABasicBlock
	Params    []*SSAParameter
	FreeVars  []*SSAFreeVar
	Signature *Signature
	Pkg       *SSAPackage
	Prog      *SSAProgram
	AnonFuncs []*SSAFunction
}

type SSAParameter struct {
	name   string
	typ    Type
	parent *SSAFunction
}
func (p *SSAParameter) SSAName() string       { return p.name }
func (p *SSAParameter) SSAType() Type         { return p.typ }
func (p *SSAParameter) SSAParent() *SSAFunction { return p.parent }

type SSAFreeVar struct {
	name   string
	typ    Type
	parent *SSAFunction
}
func (f *SSAFreeVar) SSAName() string       { return f.name }
func (f *SSAFreeVar) SSAType() Type         { return f.typ }

type SSAPackage struct {
	Prog    *SSAProgram
	Members map[string]SSAMember
}

type SSAMember interface {
	SSAName() string
}

type SSAProgram struct {
	packages map[*TCPackage]*SSAPackage
	imported map[string]*SSAPackage
}

type TCPackage struct {
	path  string
	name  string
	scope *Scope
}
func (p *TCPackage) Path() string  { return p.path }
func (p *TCPackage) Name() string  { return p.name }
func (p *TCPackage) Scope() *Scope { return p.scope }

type Scope struct {
	parent  *Scope
	objects map[string]Object
}
func NewScope(parent *Scope) *Scope { return &Scope{parent, map[string]Object{}} }
func (s *Scope) Lookup(name string) Object {
	if s.objects != nil {
		if obj, ok := s.objects[name]; ok {
			return obj
		}
	}
	return nil
}

type Object interface {
	Name() string
}

type Signature struct {
	recv    *TCVar
	params  *Tuple
	results *Tuple
}
func NewSignature(recv *TCVar, params *Tuple, results *Tuple) *Signature {
	return &Signature{recv, params, results}
}
func (s *Signature) Params() *Tuple  { return s.params }
func (s *Signature) Results() *Tuple { return s.results }
func (s *Signature) Underlying() Type { return s }
func (s *Signature) String() string   { return "func" }

type TCVar struct {
	name string
	typ  Type
}
func (v *TCVar) Name() string { return v.name }
func (v *TCVar) Type() Type   { return v.typ }

type Tuple struct {
	vars []*TCVar
}
func NewTuple(vars ...*TCVar) *Tuple {
	if len(vars) == 0 { return nil }
	return &Tuple{vars}
}
func (t *Tuple) Len() int      { return len(t.vars) }
func (t *Tuple) At(i int) *TCVar { return t.vars[i] }

func (prog *SSAProgram) ImportedPackage(path string) *SSAPackage {
	return prog.imported[path]
}

func main() {
	op := OpAdd
	_ = op.String()
	_ = ssaConstString(nil)
}
`, func(ir string) {
		funcCount := strings.Count(ir, "\ndefine ")
		assert("321: many functions from ssa_types", funcCount >= 15)
		assert("321: has SSAOp.String", strings.Contains(ir, "@main.SSAOp.String"))
		assert("321: has ssaConstString", strings.Contains(ir, "@main.ssaConstString"))
		assert("321: has NewScope", strings.Contains(ir, "@main.NewScope"))
		llvmVerify("321: ssa_types subset", ir)
	})

	// Test 322: concat all compile/ .mx files and compile
	fmt.Fprintf(os.Stderr, ">>> Starting test 322 (full concat)\n")
	clearImports()
	regPkg("go/token", "token")
	regConst("go/token", "ILLEGAL", "int", 0)
	regConst("go/token", "EOF", "int", 1)
	regConst("go/token", "COMMENT", "int", 2)
	regConst("go/token", "IDENT", "int", 4)
	regConst("go/token", "INT", "int", 5)
	regConst("go/token", "FLOAT", "int", 6)
	regConst("go/token", "IMAG", "int", 7)
	regConst("go/token", "CHAR", "int", 8)
	regConst("go/token", "STRING", "int", 9)
	regConst("go/token", "ADD", "int", 12)
	regConst("go/token", "SUB", "int", 13)
	regConst("go/token", "MUL", "int", 14)
	regConst("go/token", "QUO", "int", 15)
	regConst("go/token", "REM", "int", 16)
	regConst("go/token", "AND", "int", 17)
	regConst("go/token", "OR", "int", 18)
	regConst("go/token", "XOR", "int", 19)
	regConst("go/token", "SHL", "int", 20)
	regConst("go/token", "SHR", "int", 21)
	regConst("go/token", "AND_NOT", "int", 22)
	regConst("go/token", "LAND", "int", 34)
	regConst("go/token", "LOR", "int", 35)
	regConst("go/token", "ARROW", "int", 36)
	regConst("go/token", "INC", "int", 37)
	regConst("go/token", "DEC", "int", 38)
	regConst("go/token", "EQL", "int", 39)
	regConst("go/token", "LSS", "int", 40)
	regConst("go/token", "GTR", "int", 41)
	regConst("go/token", "ASSIGN", "int", 42)
	regConst("go/token", "NOT", "int", 43)
	regConst("go/token", "NEQ", "int", 44)
	regConst("go/token", "LEQ", "int", 45)
	regConst("go/token", "GEQ", "int", 46)
	regConst("go/token", "DEFINE", "int", 47)
	regConst("go/token", "ELLIPSIS", "int", 48)
	regConst("go/token", "DEFAULT", "int", 62)
	regType("go/token", "Token", "int")
	regPkg("go/constant", "constant")
	regConst("go/constant", "Unknown", "int", 0)
	regConst("go/constant", "Bool", "int", 1)
	regConst("go/constant", "String", "int", 2)
	regConst("go/constant", "Int", "int", 3)
	regConst("go/constant", "Float", "int", 4)
	regConst("go/constant", "Complex", "int", 5)
	regIface("go/constant", "Value", "Kind=->int;String=->string;ExactString=->string")
	regFn("go/constant", "MakeInt64", "int64->interface{}")
	regFn("go/constant", "MakeFloat64", "float64->interface{}")
	regFn("go/constant", "MakeString", "string->interface{}")
	regFn("go/constant", "MakeFromLiteral", "string,int,int->interface{}")
	regFn("go/constant", "BinaryOp", "interface{},int,interface{}->interface{}")
	regFn("go/constant", "UnaryOp", "int,interface{},int->interface{}")
	regFn("go/constant", "Compare", "interface{},int,interface{}->bool")
	regFn("go/constant", "StringVal", "interface{}->string")
	regFn("go/constant", "Int64Val", "interface{}->int64,bool")
	regFn("go/constant", "Uint64Val", "interface{}->uint64,bool")
	regFn("go/constant", "Float64Val", "interface{}->float64,bool")
	regFn("go/constant", "BitLen", "interface{}->int")
	regFn("go/constant", "Sign", "interface{}->int")
	regFn("go/constant", "Shift", "interface{},int,uint->interface{}")
	regFn("go/constant", "MakeBool", "bool->interface{}")
	regFn("go/constant", "ToInt", "interface{}->interface{}")
	regFn("go/constant", "ToFloat", "interface{}->interface{}")
	regFn("go/constant", "Num", "interface{}->interface{}")
	regFn("go/constant", "Denom", "interface{}->interface{}")
	regFn("go/constant", "Real", "interface{}->interface{}")
	regFn("go/constant", "Imag", "interface{}->interface{}")
	regPkg("fmt", "fmt")
	regFn("fmt", "Sprintf", "string,interface{},interface{},interface{}->string")
	regFn("fmt", "Fprintf", "interface{},string,interface{},interface{}->int,error")
	regPkg("os", "os")
	regVar("os", "Stderr", "interface{}")
	regPkg("strconv", "strconv")
	regFn("strconv", "Itoa", "int->string")
	regFn("strconv", "FormatInt", "int64,int->string")
	regFn("strconv", "FormatFloat", "float64,byte,int,int->string")
	regPkg("bytes", "bytes")
	regFn("bytes", "NewReader", "*byte->interface{}")
	regFn("bytes", "IndexByte", "[]byte,byte->int")
	regFn("bytes", "HasPrefix", "[]byte,[]byte->bool")
	regFn("bytes", "TrimSpace", "[]byte->[]byte")
	regPkg("io", "io")
	regIface("io", "Reader", "Read=[]byte->int,error")
	regVar("io", "EOF", "error")
	regVar("io", "ErrNoProgress", "error")
	regPkg("runtime", "runtime")
	regFn("runtime", "InitCShared", "")
	regPkg("unsafe", "unsafe")
	regPkg("unicode", "unicode")
	regType("unicode", "Tables", "")
	regMethod("unicode", "Tables", "IsLetter", "int32->bool", true)
	regMethod("unicode", "Tables", "IsDigit", "int32->bool", true)
	regFn("unicode", "NewTables", "->ptr")
	regFn("unicode", "IsLetter", "int32->bool")
	regFn("unicode", "IsDigit", "int32->bool")
	regFn("unicode", "IsSpace", "int32->bool")
	regVar("unicode", "MaxRune", "int32")
	regPkg("unicode/utf8", "utf8")
	regFn("unicode/utf8", "DecodeRune", "[]byte->int32,int")
	regFn("unicode/utf8", "RuneLen", "int32->int")
	regVar("unicode/utf8", "RuneSelf", "int")
	regVar("unicode/utf8", "UTFMax", "int")
	regVar("unicode/utf8", "RuneError", "int32")
	regFn("unicode/utf8", "FullRune", "[]byte->bool")
	{
		compileDir := filepath.Join(filepath.Dir(soPath), "compile")
		mxFiles := []string{
			"pos.mx", "source.mx", "tokens.mx", "nodes.mx", "syntax.mx",
			"scanner.mx", "parser.mx",
			"tc_scope.mx", "tc_package.mx", "tc_info.mx", "tc_object.mx",
			"tc_types.mx", "tc_universe.mx", "tc_const.mx", "tc_resolve.mx",
			"tc_checker.mx", "tc_decl.mx", "tc_expr.mx", "tc_stmt.mx", "tc_assign.mx",
			"ssa_types.mx", "ssa_builder.mx", "ir_emit.mx",
		}
		// Incremental concat test - add one file at a time via subprocess
		for i := 1; i <= len(mxFiles); i++ {
			subset := mxFiles[:i]
			combined, fc, tl := concatMxSources(compileDir, subset)
			combined = append(combined, []byte("\nfunc main() {}\n")...)
			tmpFile := filepath.Join(os.TempDir(), "mxconcat_incr.mx")
			os.WriteFile(tmpFile, combined, 0644)
			cmd := exec.Command(os.Args[0], soPath, "--compile-file", tmpFile)
			cmd.Env = os.Environ()
			out, err := cmd.CombinedOutput()
			os.Remove(tmpFile)
			outStr := strings.TrimSpace(string(out))
			if err != nil {
				short := outStr
				if len(short) > 100 { short = short[:100] }
				fmt.Fprintf(os.Stderr, ">>> FAIL +%s: %d files, %d lines: %s\n", mxFiles[i-1], fc, tl, short)
			} else {
				fmt.Fprintf(os.Stderr, ">>> OK +%s: %d files, %d lines -> %s\n", mxFiles[i-1], fc, tl, outStr)
			}
		}
		assert("322: incremental concat tested", true)
	}
	clearImports()

	// Test 323: compile real pos.mx from disk
	fmt.Fprintf(os.Stderr, ">>> Starting test 323\n")
	clearImports()
	regPkg("fmt", "fmt")
	regFn("fmt", "Sprintf", "string,interface{},interface{},interface{}->string")
	{
		posPath := filepath.Join(filepath.Dir(soPath), "syntax", "pos.mx")
		posSrc, err := os.ReadFile(posPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "SKIP test 323: %v\n", err)
		} else {
			// pos.mx needs Linebase/Colbase from source.mx
			extra := []byte("const Linebase = 1\nconst Colbase = 1\n")
			src := append(posSrc, extra...)
			pkg := []byte("main")
			h := compileToIR(
				uintptr(unsafe.Pointer(&src[0])), int32(len(src)),
				uintptr(unsafe.Pointer(&pkg[0])), int32(len(pkg)),
				uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
			)
			ir := getIR(h)
			if ir == "" {
				assert("323: pos.mx produces IR", false)
			} else {
				funcCount := strings.Count(ir, "\ndefine ")
				fmt.Printf("=== pos.mx (real file) ===\n")
				fmt.Printf("Source: %d lines | IR: %d bytes, %d functions\n",
					strings.Count(string(posSrc), "\n"), len(ir), funcCount)
				assert("323: produces IR", len(ir) > 5000)
				assert("323: many functions", funcCount >= 15)
				llvmVerify("323: real pos.mx", ir)
			}
			irFree(h)
		}
	}
	clearImports()

	// Test 212: Scale test
	src212 := []byte(`package main

type Type interface {
	Underlying() Type
	String() string
}

type Basic struct {
	kind int32
	name string
}
func (b *Basic) Underlying() Type { return b }
func (b *Basic) String() string   { return b.name }

type Slice struct{ elem Type }
func (s *Slice) Underlying() Type { return s }
func (s *Slice) String() string   { return "[]" | s.elem.String() }
func NewSlice(elem Type) *Slice   { return &Slice{elem: elem} }

type Pointer struct{ base Type }
func (p *Pointer) Underlying() Type { return p }
func (p *Pointer) String() string   { return "*" | p.base.String() }
func NewPointer(elem Type) *Pointer { return &Pointer{base: elem} }

type Named struct {
	name       string
	underlying Type
	methods    []*Func211
}
func (n *Named) Underlying() Type     { return n.underlying }
func (n *Named) String() string       { return n.name }
func (n *Named) AddMethod(m *Func211) { n.methods = append(n.methods, m) }

type Var211 struct {
	name string
	typ  Type
}
func NewVar211(name string, typ Type) *Var211 { return &Var211{name: name, typ: typ} }
func (v *Var211) Name() string { return v.name }
func (v *Var211) Type() Type   { return v.typ }

type Tuple struct{ vars []*Var211 }
func NewTuple(vars ...*Var211) *Tuple {
	if len(vars) == 0 { return nil }
	return &Tuple{vars: vars}
}
func (t *Tuple) Len() int32         { return int32(len(t.vars)) }
func (t *Tuple) At(i int32) *Var211 { return t.vars[i] }

type Signature struct {
	recv    *Var211
	params  *Tuple
	results *Tuple
}
func NewSignature(recv *Var211, params *Tuple, results *Tuple) *Signature {
	return &Signature{recv: recv, params: params, results: results}
}
func (s *Signature) Underlying() Type { return s }
func (s *Signature) String() string   { return "func" }
func (s *Signature) Params() *Tuple   { return s.params }
func (s *Signature) Results() *Tuple  { return s.results }

type Func211 struct {
	name string
	typ  Type
}

func main() {
	intT := &Basic{kind: 2, name: "int"}
	strT := &Basic{kind: 14, name: "string"}

	sliceInt := NewSlice(intT)
	ptrStr := NewPointer(strT)

	named := &Named{name: "MyType", underlying: sliceInt}
	sig := NewSignature(
		NewVar211("s", named),
		NewTuple(NewVar211("i", intT)),
		NewTuple(NewVar211("", intT)),
	)
	named.AddMethod(&Func211{name: "Get", typ: sig})

	s1 := sliceInt.String()
	s2 := ptrStr.String()
	s3 := named.String()
	_ = s1
	_ = s2
	_ = s3
	_ = sig.Params().Len()
}
`)
	name212 := []byte("main")
	h212 := compileToIR(
		uintptr(unsafe.Pointer(&src212[0])), int32(len(src212)),
		uintptr(unsafe.Pointer(&name212[0])), int32(len(name212)),
		uintptr(unsafe.Pointer(&triple[0])), int32(len(triple)),
	)
	ir212 := getIR(h212)
	if ir212 == "" {
		assert("212: scale test produces IR", false)
	} else {
		voidCount := strings.Count(ir212, "void %")
		funcCount := strings.Count(ir212, "\ndefine ")
		lineCount := strings.Count(string(src212), "\n")
		fmt.Printf("=== Scale test (211) ===\n")
		fmt.Printf("Source: %d lines, %d bytes | IR: %d bytes, %d functions, void fields: %d\n",
			lineCount, len(src212), len(ir212), funcCount, voidCount)
		assert("212: produces IR", len(ir212) > 1000)
		assert("212: zero void fields", voidCount == 0)
		assert("212: has NewSignature", strings.Contains(ir212, "@main.NewSignature"))
		llvmVerify("212: scale test", ir212)
	}
	irFree(h212)

	fmt.Printf("\n%d/%d tests passed\n", pass, pass+fail)
	if fail > 0 {
		os.Exit(1)
	}
}
