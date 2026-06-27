//go:build ignore

package main

import (
	"os"
	"unsafe"
)

//export mxc_run
func cRun(argv unsafe.Pointer, argc int32) int32

//export mxc_getenv
func cGetenv(name unsafe.Pointer, nameLen int32, buf unsafe.Pointer, bufCap int32) int32

//export write
func cWrite(fd int32, buf unsafe.Pointer, count uint) int

func writeStr(fd int32, s string) {
	if len(s) > 0 {
		cWrite(fd, unsafe.Pointer(&[]byte(s)[0]), uint(len(s)))
	}
}

func fatal(msg string) {
	writeStr(2, "mxc: ")
	writeStr(2, msg)
	writeStr(2, "\n")
	os.Exit(1)
}

func getenv(name string) string {
	b := []byte(name)
	buf := make([]byte, 4096)
	n := cGetenv(unsafe.Pointer(&b[0]), int32(len(b)), unsafe.Pointer(&buf[0]), int32(len(buf)))
	if n <= 0 {
		return ""
	}
	return string(buf[:n])
}

func getArgs() []string {
	data, err := os.ReadFile("/proc/self/cmdline")
	if err != nil {
		return nil
	}
	var args []string
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == 0 {
			if i > start {
				args = append(args, string(data[start:i]))
			}
			start = i + 1
		}
	}
	if start < len(data) {
		args = append(args, string(data[start:]))
	}
	return args
}

func listFiles(dir, ext string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var result []string
	for _, e := range entries {
		name := e.Name()
		if hasSuffix(name, ext) {
			result = append(result, name)
		}
	}
	return result
}

func run(args []string) int32 {
	argv := make([]unsafe.Pointer, len(args)+1)
	cstrs := make([][]byte, len(args))
	for i, a := range args {
		b := make([]byte, len(a)+1)
		copy(b, a)
		b[len(a)] = 0
		cstrs[i] = b
		argv[i] = unsafe.Pointer(&b[0])
	}
	return cRun(unsafe.Pointer(&argv[0]), int32(len(args)))
}

func splitLines(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				result = append(result, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func trimSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	j := len(s)
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

func joinPath(a, b string) string {
	if len(a) == 0 {
		return b
	}
	if a[len(a)-1] == '/' {
		return a + b
	}
	return a + "/" + b
}

func pathBase(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	n := 0
	for _, s := range ss {
		n += len(s)
	}
	n += len(sep) * (len(ss) - 1)
	buf := make([]byte, 0, n)
	for i, s := range ss {
		if i > 0 {
			buf = append(buf, sep...)
		}
		buf = append(buf, s...)
	}
	return string(buf)
}

func appendUniq(ss []string, s string) []string {
	for _, x := range ss {
		if x == s {
			return ss
		}
	}
	return append(ss, s)
}

func loadSysroot(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range splitLines(string(data)) {
		line = trimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		eq := -1
		for i := 0; i < len(line); i++ {
			if line[i] == '=' {
				eq = i
				break
			}
		}
		if eq < 0 {
			continue
		}
		key := line[:eq]
		val := line[eq+1:]
		os.Setenv(key, val)
	}
}

func splitBySpace(s string) []string {
	var result []string
	start := 0
	inWord := false
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			if inWord {
				result = append(result, s[start:i])
				inWord = false
			}
		} else {
			if !inWord {
				start = i
				inWord = true
			}
		}
	}
	if inWord {
		result = append(result, s[start:])
	}
	return result
}

func safeName(pkg string) string {
	b := make([]byte, len(pkg))
	for i := 0; i < len(pkg); i++ {
		if pkg[i] == '/' {
			b[i] = '_'
		} else {
			b[i] = pkg[i]
		}
	}
	return string(b)
}

func extractPkgName(data []byte) string {
	for i := 0; i < len(data); i++ {
		if i+8 < len(data) && string(data[i:i+8]) == "package " {
			j := i + 8
			for j < len(data) && data[j] != '\n' && data[j] != '\r' && data[j] != ' ' {
				j++
			}
			return string(data[i+8 : j])
		}
		if data[i] == '\n' {
			continue
		}
		if data[i] == '/' && i+1 < len(data) && data[i+1] == '/' {
			for i < len(data) && data[i] != '\n' {
				i++
			}
			continue
		}
		break
	}
	return "main"
}

func extractImports(data []byte) []string {
	var imports []string
	lines := splitLines(string(data))
	inBlock := false
	for _, line := range lines {
		line = trimSpace(line)
		if line == "import (" {
			inBlock = true
			continue
		}
		if inBlock && line == ")" {
			inBlock = false
			continue
		}
		if inBlock {
			imp := extractQuoted(line)
			if imp != "" {
				imports = appendUniq(imports, imp)
			}
		}
		if hasPrefix(line, "import \"") {
			imp := extractQuoted(line[7:])
			if imp != "" {
				imports = appendUniq(imports, imp)
			}
		}
	}
	return imports
}

func extractQuoted(s string) string {
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			if start < 0 {
				start = i + 1
			} else {
				return s[start:i]
			}
		}
	}
	return ""
}

func concatSources(dir string, files []string) []byte {
	imports := map[string]bool{}
	var bodies [][]byte
	pkgName := ""
	for _, f := range files {
		data, err := os.ReadFile(joinPath(dir, f))
		if err != nil {
			continue
		}
		if pkgName == "" {
			pkgName = extractPkgName(data)
		}
		lines := splitLines(string(data))
		var body []string
		i := 0
		for i < len(lines) {
			line := trimSpace(lines[i])
			if hasPrefix(line, "package ") {
				i++
				continue
			}
			if line == "import (" {
				i++
				for i < len(lines) {
					imp := trimSpace(lines[i])
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
			if hasPrefix(line, "import ") && !hasPrefix(line, "import (") {
				imp := line[7:]
				imports[imp] = true
				i++
				continue
			}
			body = append(body, lines[i])
			i++
		}
		bodies = append(bodies, []byte(joinStrings(body, "\n")))
	}
	var out []byte
	out = append(out, []byte("package "+pkgName+"\n")...)
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
	return out
}

func shouldSkipFile(name string) bool {
	if hasSuffix(name, "_test.go") || hasSuffix(name, "_test.mx") {
		return true
	}
	skip := []string{"_wasm"}
	base := name
	if hasSuffix(base, ".go") {
		base = base[:len(base)-3]
	} else if hasSuffix(base, ".mx") {
		base = base[:len(base)-3]
	}
	for _, s := range skip {
		if hasSuffix(base, s) {
			return true
		}
	}
	return false
}

func hasBuildConstraint(data []byte, forbidden []string) bool {
	for i := 0; i < len(data) && i < 512; i++ {
		if data[i] == 'p' {
			return false
		}
		if i+10 < len(data) && string(data[i:i+10]) == "//go:build " {
			line := ""
			for j := i + 10; j < len(data) && data[j] != '\n'; j++ {
				line = line + string(data[j])
			}
			for _, f := range forbidden {
				if line == f || hasPrefix(line, f+" ") || hasPrefix(line, f+"\t") {
					return true
				}
			}
			return false
		}
		if data[i] == '\n' {
			continue
		}
		if data[i] == '/' && i+1 < len(data) && data[i+1] == '/' {
			for i < len(data) && data[i] != '\n' {
				i++
			}
			continue
		}
	}
	return false
}

type pkgInfo struct {
	path    string
	name    string
	dir     string
	files   []string
	cfiles  []string
	imports []string
}

func discoverPkg(pkgPath, root string) *pkgInfo {
	var dir string
	if hasPrefix(pkgPath, "/") || hasPrefix(pkgPath, "./") || hasPrefix(pkgPath, "../") {
		dir = pkgPath
	} else {
		dir = joinPath(root, joinPath("src", pkgPath))
	}
	goFiles := listFiles(dir, ".go")
	mxFiles := listFiles(dir, ".mx")
	cFiles := listFiles(dir, ".c")
	allMx := append(mxFiles, goFiles...)
	forbidden := []string{"wasm", "js"}
	var files []string
	for _, f := range allMx {
		if shouldSkipFile(f) {
			continue
		}
		data, err := os.ReadFile(joinPath(dir, f))
		if err != nil {
			continue
		}
		if hasBuildConstraint(data, forbidden) {
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil
	}
	var allImports []string
	pkgName := ""
	for _, f := range files {
		data, err := os.ReadFile(joinPath(dir, f))
		if err != nil {
			continue
		}
		if pkgName == "" {
			pkgName = extractPkgName(data)
		}
		for _, imp := range extractImports(data) {
			allImports = appendUniq(allImports, imp)
		}
	}
	return &pkgInfo{
		path:    pkgPath,
		name:    pkgName,
		dir:     dir,
		files:   files,
		cfiles:  cFiles,
		imports: allImports,
	}
}

func resolveAll(rootPkg string, root string) []*pkgInfo {
	visited := map[string]bool{}
	var order []*pkgInfo
	var walk func(string)
	walk = func(path string) {
		if visited[path] {
			return
		}
		visited[path] = true
		if path == "unsafe" || path == "moxie" || path == "C" {
			return
		}
		if hasPrefix(path, "runtime/") {
			return
		}
		pkg := discoverPkg(path, root)
		if pkg == nil {
			writeStr(2, "mxc: cannot find package: "+path+"\n")
			return
		}
		for _, imp := range pkg.imports {
			walk(imp)
		}
		order = append(order, pkg)
	}
	walk(rootPkg)
	return order
}

func registerBuiltins() {
	initUniverse()
	ensureImportRegistry()
	importRegistry = map[string]*TCPackage{}
	unsafePkg := NewTCPackage("unsafe", "unsafe")
	unsafePkg.Scope().Insert(NewTypeName(unsafePkg, "Pointer", Typ[UnsafePointer]))
	unsafePkg.Scope().Insert(NewTCFunc(unsafePkg, "Sizeof", parseSignatureDesc("interface{}->int32")))
	unsafePkg.Scope().Insert(NewTCFunc(unsafePkg, "Offsetof", parseSignatureDesc("interface{}->int32")))
	unsafePkg.Scope().Insert(NewTCFunc(unsafePkg, "Alignof", parseSignatureDesc("interface{}->int32")))
	unsafePkg.Scope().Insert(NewTCFunc(unsafePkg, "Slice", parseSignatureDesc("ptr,int32->[]uint8")))
	unsafePkg.Scope().Insert(NewTCFunc(unsafePkg, "SliceData", parseSignatureDesc("[]uint8->ptr")))
	importRegistry["unsafe"] = unsafePkg
	importRegistry["moxie"] = NewTCPackage("moxie", "moxie")
}

func compileSource(src []byte, pkgName, triple string) string {
	ir := CompileToIR(src, pkgName, triple)
	if len(ir) > 0 && ir[0] == ';' {
		writeStr(2, ir)
		fatal("compile error for " + pkgName)
	}
	if len(ir) == 0 {
		fatal("empty IR for " + pkgName)
	}
	return ir
}

func main() {
	args := getArgs()
	if len(args) >= 2 {
		cmd := args[1]
		if cmd == "version" {
			writeStr(1, "mxc 1.6.1\n")
			return
		}
		if cmd != "build" || len(args) < 3 {
			writeStr(2, "usage: mxc build [-o output] <package>\n")
			os.Exit(1)
		}
	} else {
		writeStr(2, "usage: mxc build [-o output] <package>\n")
		os.Exit(1)
	}

	outpath := ""
	pkgPath := ""
	printIR := false
	i := 2
	for i < len(args) {
		if args[i] == "-o" && i+1 < len(args) {
			outpath = args[i+1]
			i += 2
		} else if args[i] == "-print-ir" {
			printIR = true
			i++
		} else {
			pkgPath = args[i]
			i++
		}
	}
	if pkgPath == "" {
		fatal("no package specified")
	}
	if outpath == "" {
		outpath = pathBase(pkgPath)
	}

	root := getenv("MOXIEROOT")
	if root == "" {
		root = "."
	}
	triple := "x86_64-unknown-linux-musleabihf"
	clang := "clang-21"

	registerBuiltins()
	pkgs := resolveAll(pkgPath, root)
	if len(pkgs) == 0 {
		fatal("no packages to compile")
	}

	tmpdir := "/tmp/mxc-build"
	os.MkdirAll(tmpdir, 0755)

	var userBcFiles []string

	for _, pkg := range pkgs {
		writeStr(2, "  compile "+pkg.path+"\n")
		src := concatSources(pkg.dir, pkg.files)
		compilePath := pkg.path
		if hasPrefix(compilePath, "/") || hasPrefix(compilePath, "./") || hasPrefix(compilePath, "../") {
			compilePath = pkg.name
		}
		ir := compileSource(src, compilePath, triple)
		if printIR {
			writeStr(1, ir)
		}
		llFile := joinPath(tmpdir, safeName(pkg.path)+".ll")
		os.WriteFile(llFile, []byte(ir), 0644)
		bcFile := joinPath(tmpdir, safeName(pkg.path)+".bc")
		rc := run([]string{clang, "--target="+triple, "-emit-llvm", "-c", llFile, "-o", bcFile})
		if rc != 0 {
			fatal("clang failed for " + pkg.path)
		}
		userBcFiles = append(userBcFiles, bcFile)
	}

	loadSysroot(joinPath(root, "_sysroot.env"))

	runtimeBc := getenv("RUNTIME_BC")
	if runtimeBc == "" {
		runtimeBc = joinPath(root, "_runtime.bc")
	}
	muslDir := getenv("MUSL_DIR")
	if muslDir == "" {
		fatal("MUSL_DIR not set - run build-self.sh first")
	}

	writeStr(2, "  generate initAll\n")
	var initCalls string
	for _, pkg := range pkgs {
		compilePath := pkg.path
		if hasPrefix(compilePath, "/") || hasPrefix(compilePath, "./") || hasPrefix(compilePath, "../") {
			compilePath = pkg.name
		}
		initCalls = initCalls + "  call void @" + compilePath + ".init(ptr %context)\n"
	}
	initAllIR := "define void @runtime.initAll(ptr %context) {\nentry:\n" + initCalls + "  ret void\n}\n"
	for _, pkg := range pkgs {
		compilePath := pkg.path
		if hasPrefix(compilePath, "/") || hasPrefix(compilePath, "./") || hasPrefix(compilePath, "../") {
			compilePath = pkg.name
		}
		initAllIR = initAllIR + "declare void @" + compilePath + ".init(ptr)\n"
	}
	initAllFile := joinPath(tmpdir, "initall.ll")
	os.WriteFile(initAllFile, []byte(initAllIR), 0644)
	initAllBc := joinPath(tmpdir, "initall.bc")
	rc := run([]string{clang, "--target="+triple, "-emit-llvm", "-c", initAllFile, "-o", initAllBc})
	if rc != 0 {
		fatal("clang failed for initall")
	}

	writeStr(2, "  merge bitcode\n")
	llvmLink := "llvm-link-21"
	mergedBc := joinPath(tmpdir, "merged.bc")
	linkArgs := []string{llvmLink, "-o", mergedBc, runtimeBc}
	linkArgs = append(linkArgs, userBcFiles...)
	linkArgs = append(linkArgs, initAllBc)
	rc = run(linkArgs)
	if rc != 0 {
		fatal("llvm-link failed")
	}

	writeStr(2, "  link\n")
	comprtLib := getenv("COMPRT_LIB")
	bdwgcLib := getenv("BDWGC_LIB")
	muslLib := getenv("MUSL_LIB")
	objFiles := getenv("OBJ_FILES")
	linker := "ld.lld-21"
	ldArgs := []string{linker, "--gc-sections", "-o", outpath,
		joinPath(muslDir, "crt1.o"),
		mergedBc}
	for _, f := range splitBySpace(objFiles) {
		if f != "" {
			ldArgs = append(ldArgs, f)
		}
	}
	ldArgs = append(ldArgs, comprtLib, bdwgcLib, muslLib, "--lto-O0")
	rc = run(ldArgs)
	if rc != 0 {
		fatal("linker failed")
	}
	writeStr(2, "  -> "+outpath+"\n")
}
