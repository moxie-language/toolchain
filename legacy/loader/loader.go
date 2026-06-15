package loader

import (
	"crypto/sha512"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"moxie/syntax"
	"moxie/compileopts"
	"moxie/goenv"
)

var initFileVersions = func(info *types.Info) {}

// Program holds all packages and some metadata about the program as a whole.
type Program struct {
	config      *compileopts.Config
	typeChecker types.Config
	goroot      string // synthetic GOROOT
	workingDir  string

	Packages        map[string]*Package
	sorted          []*Package
	fset            *token.FileSet
	keepSyntaxFiles bool // set by EnableSyntaxFiles; only mxcheck needs this

	// MXHPackages is the set of import paths loaded from .mxh cache files.
	// The compiler uses this to identify cross-binary spawn targets.
	MXHPackages map[string]bool

	// SkipMainNameCheck disables the "main package must be named main" check.
	// Used by moxie header which extracts types from non-main packages.
	SkipMainNameCheck bool

	// Information obtained during parsing.
	LDFlags []string
}

// EnableSyntaxFiles instructs the loader to retain *syntax.File ASTs in
// Package.SyntaxFiles. Off by default to avoid doubling AST memory during
// normal builds (the compiler does not use syntax files today).
func (p *Program) EnableSyntaxFiles() { p.keepSyntaxFiles = true }

// PackageJSON is a subset of the JSON struct returned from `moxie list`.
type PackageJSON struct {
	Dir        string
	ImportPath string
	Name       string
	ForTest    string
	Root       string
	Module     struct {
		Path      string
		Main      bool
		Dir       string
		GoMod     string
		GoVersion string
	}

	// Source files
	GoFiles  []string // TODO: rename to MxFiles when moxie list is implemented
	CgoFiles []string
	CFiles   []string

	// Embedded files
	EmbedFiles []string

	// Dependency information
	Imports   []string
	ImportMap map[string]string

	// Error information
	Error *struct {
		ImportStack []string
		Pos         string
		Err         string
	}

	// IsMXH marks this as a synthetic package loaded from a .mxh file.
	// Packages with this flag skip Parse/Check and have Pkg set directly.
	IsMXH bool
}

// Package holds a loaded package, its imports, and its parsed files.
type Package struct {
	PackageJSON

	program      *Program
	Files        []*ast.File
	SyntaxFiles  []*syntax.File // bootstrap-parser AST, used by the native type checker (B1)
	FileHashes   map[string][]byte
	CFlags       []string
	EmbedGlobals map[string][]*EmbedFile
	Pkg          *types.Package
	info         types.Info
	hasBuiltins  bool

	// MakeExemptOffsets maps filenames to byte offsets of loader-generated
	// make() calls. The restriction check uses this to distinguish
	// user-written make() (illegal) from literal-syntax lowerings.
	MakeExemptOffsets map[string][]int
}

type EmbedFile struct {
	Name      string
	Size      uint64
	Hash      string // hash of the file (as a hex string)
	NeedsData bool   // true if this file is embedded as a byte slice
	Data      []byte // contents of this file (only if NeedsData is set)
}

// Load loads the given package with all dependencies (including the runtime
// package). Call .Parse() afterwards to parse all Go files (including CGo
// processing, if necessary).
func Load(config *compileopts.Config, inputPkg string, typeChecker types.Config) (*Program, error) {
	// Make int≡int32 and uint≡uint32 on all targets.
	patchIntTypes()

	goroot, err := GetCachedGoroot(config)
	if err != nil {
		return nil, err
	}
	var wd string
	if config.Options.Directory != "" {
		wd = config.Options.Directory
	} else {
		wd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	p := &Program{
		config:      config,
		typeChecker: typeChecker,
		goroot:      goroot,
		workingDir:  wd,
		Packages:    make(map[string]*Package),
		MXHPackages: make(map[string]bool),
		fset:        token.NewFileSet(),
	}

	// Discover packages and dependencies using the internal mxlist
	// package scanner. This replaces the external `go list` command and
	// natively understands .mx source files.
	var pkgJSONs []*PackageJSON
	if config.TestConfig.CompileTestBinary {
		pkgJSONs, err = mxListPackagesTest(config, goroot, inputPkg)
	} else {
		pkgJSONs, err = mxListPackages(config, goroot, inputPkg)
	}
	if err != nil {
		return nil, fmt.Errorf("mxlist: %w", err)
	}

	for _, pj := range pkgJSONs {
		pkg := &Package{
			PackageJSON: *pj,
			program:     p,
			FileHashes:  make(map[string][]byte),
			EmbedGlobals: make(map[string][]*EmbedFile),
			info: types.Info{
				Types:      make(map[ast.Expr]types.TypeAndValue),
				Instances:  make(map[*ast.Ident]types.Instance),
				Defs:       make(map[*ast.Ident]types.Object),
				Uses:       make(map[*ast.Ident]types.Object),
				Implicits:  make(map[ast.Node]types.Object),
				Scopes:     make(map[ast.Node]*types.Scope),
				Selections: make(map[*ast.SelectorExpr]*types.Selection),
			},
		}
		if pj.IsMXH {
			// Synthetic package from .mxh cache: build types.Package directly.
			mxhPath := filepath.Join(MXHProtocolsDir(), pj.ImportPath+".mxh")
			synth, err := synthMXHPackage(pj.ImportPath, mxhPath)
			if err != nil {
				return nil, fmt.Errorf("loading .mxh for %s: %w", pj.ImportPath, err)
			}
			pkg.Pkg = synth
			p.MXHPackages[pj.ImportPath] = true
		}
		p.sorted = append(p.sorted, pkg)
		p.Packages[pkg.ImportPath] = pkg
	}

	return p, nil
}

// getOriginalPath looks whether this path is in the generated GOROOT and if so,
// replaces the path with the original path (in GOROOT or MOXIEROOT). Otherwise
// the input path is returned.
func (p *Program) getOriginalPath(path string) string {
	originalPath := path
	if strings.HasPrefix(path, p.goroot+string(filepath.Separator)) {
		// If this file is part of the synthetic GOROOT, try to infer the
		// original path.
		relpath := path[len(filepath.Join(p.goroot, "src"))+1:]
		realgorootPath := filepath.Join(goenv.Get("GOROOT"), "src", relpath)
		if _, err := os.Stat(realgorootPath); err == nil {
			originalPath = realgorootPath
		}
		maybeInMoxieRoot := false
		for prefix := range pathsToOverride(p.config.GoMinorVersion, needsSyscallPackage(p.config.BuildTags())) {
			if runtime.GOOS == "windows" {
				prefix = strings.ReplaceAll(prefix, "/", "\\")
			}
			if !strings.HasPrefix(relpath, prefix) {
				continue
			}
			maybeInMoxieRoot = true
		}
		if maybeInMoxieRoot {
			moxiePath := filepath.Join(goenv.Get("MOXIEROOT"), "src", relpath)
			if _, err := os.Stat(moxiePath); err == nil {
				originalPath = moxiePath
			}
		}
	}
	return originalPath
}

// FileSet returns the token.FileSet used across all packages.
func (p *Program) FileSet() *token.FileSet {
	return p.fset
}

// Sorted returns a list of all packages, sorted in a way that no packages come
// before the packages they depend upon.
func (p *Program) Sorted() []*Package {
	return p.sorted
}

// MainPkg returns the last package in the Sorted() slice. This is the main
// package of the program.
func (p *Program) MainPkg() *Package {
	return p.sorted[len(p.sorted)-1]
}

// Parse parses all packages and typechecks them.
//
// The returned error may be an Errors error, which contains a list of errors.
//
// Idempotent.
func (p *Program) Parse() error {
	// Parse all packages.
	// TODO: do this in parallel.
	for _, pkg := range p.sorted {
		if pkg.IsMXH {
			continue // synthetic package; types already set by Load
		}
		err := pkg.Parse()
		if err != nil {
			return err
		}
	}

	// spawn is a true builtin (patched into go/types universe scope),
	// no injection needed — available in all packages like make/append.

	// Moxie AST rewrite: string literals -> []byte() in user and moxie-pure packages.
	for _, pkg := range p.sorted {
		if isMoxieStringTarget(pkg.ImportPath) {
			for _, file := range pkg.Files {
				rewriteStringLiterals(file)
			}
			// Stdlib/vendor packages may still use `+` for text concat
			// (predating the v1.1.1 enforcement). Syntactically rewrite
			// `+` to `|` so the patched go/types accepts them on the
			// first pass. User code still gets CheckPlusOnText to flag
			// `+` on text as an error.
			if !pkg.Module.Main {
				needsBuiltin := false
				for _, file := range pkg.Files {
					if rewriteAddToPipe(file) {
						needsBuiltin = true
					}
				}
				if needsBuiltin {
					if f := injectMoxieByteBuiltins(p.fset, pkg.Name); f != nil {
						pkg.Files = append(pkg.Files, f)
						pkg.hasBuiltins = true
					}
				}
			}
		}
	}

	// Pre-typecheck: constant-pipe and secalloc rewrites need no type info.
	// Injecting builtins here lets the first checker.Check() resolve
	// __moxie_secalloc without a re-typecheck pass for packages that use
	// secure allocation but no pipe/byte-compare rewrites.
	for _, pkg := range p.sorted {
		if isMoxieStringTarget(pkg.ImportPath) {
			rewriteConstPipes(pkg.Files)
			if !pkg.hasBuiltins && hasMoxieSecallocRefs(pkg.Files) {
				if f := injectMoxieByteBuiltins(p.fset, pkg.Name); f != nil {
					pkg.Files = append(pkg.Files, f)
					pkg.hasBuiltins = true
				}
			}
		}
	}

	// Typecheck all packages.
	for _, pkg := range p.sorted {
		if pkg.IsMXH {
			continue // synthetic package; types already set by Load
		}
		err := pkg.Check()
		if err != nil {
			return err
		}
	}

	return nil
}

// OriginalDir returns the real directory name. It is the same as p.Dir except
// that if it is part of the cached GOROOT, its real location is returned.
func (p *Package) OriginalDir() string {
	return strings.TrimSuffix(p.program.getOriginalPath(p.Dir+string(os.PathSeparator)), string(os.PathSeparator))
}

// TypeInfo returns the go/types type-checking info for this package.
func (p *Package) TypeInfo() *types.Info {
	return &p.info
}

// parseFile parses a source file using the bootstrap parser (syntax package)
// and converts the result to go/ast.File for downstream compatibility.
// The *syntax.File is stored in p.SyntaxFiles for the native type checker (B1).
func (p *Package) parseFile(path string, mode parser.Mode) (*ast.File, error) {
	originalPath := p.program.getOriginalPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum := sha512.Sum512_224(data)
	p.FileHashes[originalPath] = sum[:]

	// Rewrite Moxie pragmas (//:linkname, //:inline, etc) to //go: form
	// so the Go parser associates them correctly with function declarations.
	data = rewriteMoxiePragmas(data)
	data = rewritePkgMainToInit(data)

	chanResult := rewriteChanLiterals(data, p.program.fset)
	sliceResult := rewriteSliceLiterals(chanResult.Src, p.program.fset, chanResult.MakeOffsets)
	data = sliceResult.Src

	allOffsets := append(sliceResult.PriorOffsets, sliceResult.MakeOffsets...)
	if len(allOffsets) > 0 {
		if p.MakeExemptOffsets == nil {
			p.MakeExemptOffsets = make(map[string][]int)
		}
		p.MakeExemptOffsets[originalPath] = allOffsets
	}

	if p.program.keepSyntaxFiles {
		af, sf, err := syntax.ConvertAndKeep(p.program.fset, originalPath, data)
		if err != nil {
			return nil, err
		}
		if sf != nil {
			p.SyntaxFiles = append(p.SyntaxFiles, sf)
		}
		return af, nil
	}
	af, err := syntax.Convert(p.program.fset, originalPath, data)
	if err != nil {
		return nil, err
	}
	return af, nil
}

// Parse parses and typechecks this package.
//
// Idempotent.
func (p *Package) Parse() error {
	if len(p.Files) != 0 {
		return nil // nothing to do (?)
	}

	// Load the AST.
	if p.ImportPath == "unsafe" {
		// Special case for the unsafe package, which is defined internally by
		// the types package.
		p.Pkg = types.Unsafe
		return nil
	}

	files, err := p.parseFiles()
	if err != nil {
		return err
	}
	p.Files = files

	return nil
}

// Check runs the package through the typechecker. The package must already be
// loaded and all dependencies must have been checked already.
//
// Idempotent.
func (p *Package) Check() error {
	if p.Pkg != nil {
		return nil // already typechecked
	}

	// Prepare some state used during type checking.
	var typeErrors []error
	checker := p.program.typeChecker // make a copy, because it will be modified
	checker.Error = func(err error) {
		typeErrors = append(typeErrors, err)
	}
	checker.Importer = p
	if p.Module.GoVersion != "" {
		// Setting the Go version for a module makes sure the type checker
		// errors out on language features not supported in that particular
		// version.
		checker.GoVersion = "go" + p.Module.GoVersion
	} else {
		// Version is not known, so use the currently installed Go version.
		// This is needed for `moxie run` for example.
		// Normally we'd use goenv.GorootVersionString(), but for compatibility
		// with Go 1.20 and below we need a version in the form of "go1.12" (no
		// patch version).
		major, minor, err := goenv.GetGorootVersion()
		if err != nil {
			return err
		}
		checker.GoVersion = fmt.Sprintf("go%d.%d", major, minor)
	}
	initFileVersions(&p.info)

	// Do typechecking of the package.
	packageName := p.ImportPath
	if p == p.program.MainPkg() {
		if p.Name != "main" && !p.program.SkipMainNameCheck {
			var errPos token.Position
			if len(p.Files) > 0 {
				errPos = p.program.fset.Position(p.Files[0].Name.Pos())
			} else {
				errPos = token.Position{Filename: p.ImportPath}
			}
			return Errors{p, []error{
				scanner.Error{
					Pos: errPos,
					Msg: fmt.Sprintf("expected main package to have name \"main\", not %#v", p.Name),
				},
			}}
		}
		packageName = "main"
	}
	typesPkg, err := checker.Check(packageName, p.program.fset, p.Files, &p.info)

	// Filter Moxie-specific type errors from the first pass.
	// string/[]byte unification: these are the same type in Moxie.
	// no-new-var errors: string==[]byte makes some := seem redundant.
	typeErrors = filterStringByteMismatch(typeErrors)
	typeErrors = filterNoNewVarErrors(typeErrors)
	if err != nil && len(typeErrors) == 0 {
		err = nil
	}

	// Two-pass Moxie rewrite: pipe concat (|), byte comparisons, switches.
	needsRecheck := false
	if isMoxieStringTarget(p.ImportPath) {
		if p.Module.Main {
			if plusErrs := checkPlusOnText(p.Files, &p.info, p.program.fset); len(plusErrs) > 0 {
				return Errors{p, plusErrs}
			}
		}
		pipeRewrites := findPipeConcat(p.Files, &p.info)
		cmpExprs := findByteComparisons(p.Files, &p.info)
		byteSwitches := findByteSwitches(p.Files, &p.info)
		addAssignCount := rewriteAddAssign(p.Files, &p.info)

		if len(pipeRewrites) > 0 || len(cmpExprs) > 0 || len(byteSwitches) > 0 || addAssignCount > 0 {
			applyPipeRewrites(p.Files, pipeRewrites)
			applyByteComparisonRewrites(p.Files, cmpExprs)
			applyByteSwitchRewrites(byteSwitches)
			typeErrors = filterPipeErrors(typeErrors)
			typeErrors = filterByteCompareErrors(typeErrors)

			if !p.hasBuiltins {
				if f := injectMoxieByteBuiltins(p.program.fset, p.Name); f != nil {
					p.Files = append(p.Files, f)
				}
			}
			needsRecheck = true
		}
	}

	// Re-typecheck after AST rewrites (pipe concat, byte comparisons,
	// switches, builtins injection). The rewrites modify the AST, so
	// the first pass type info is stale. Only needed when rewrites occurred.
	if needsRecheck {
		p.info = types.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Instances:  make(map[*ast.Ident]types.Instance),
			Defs:       make(map[*ast.Ident]types.Object),
			Uses:       make(map[*ast.Ident]types.Object),
			Implicits:  make(map[ast.Node]types.Object),
			Scopes:     make(map[ast.Node]*types.Scope),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
		}
		initFileVersions(&p.info)
		typeErrors = nil
		checker2 := p.program.typeChecker
		checker2.Error = func(e error) {
			typeErrors = append(typeErrors, e)
		}
		checker2.Importer = p
		if p.Module.GoVersion != "" {
			checker2.GoVersion = "go" + p.Module.GoVersion
		} else {
			major, minor, verr := goenv.GetGorootVersion()
			if verr != nil {
				return verr
			}
			checker2.GoVersion = fmt.Sprintf("go%d.%d", major, minor)
		}
		typesPkg, err = checker2.Check(packageName, p.program.fset, p.Files, &p.info)
		typeErrors = filterPipeErrors(typeErrors)
		typeErrors = filterByteCompareErrors(typeErrors)
		typeErrors = filterNoNewVarErrors(typeErrors)
		typeErrors = filterStringByteMismatch(typeErrors)
		if err != nil && len(typeErrors) == 0 {
			err = nil
		}
	}

	if err != nil {
		if err, ok := err.(Errors); ok {
			return err
		}
		if len(typeErrors) != 0 {
			return Errors{p, typeErrors}
		}
		// This can happen in some weird cases.
		// The only case I know is when compiling a Go 1.23 program, with a
		// Moxie version that supports Go 1.23 but is compiled using Go 1.22.
		// So this should be pretty rare.
		return Errors{p, []error{err}}
	}
	p.Pkg = typesPkg

	p.extractEmbedLines(checker.Error)
	if len(typeErrors) != 0 {
		return Errors{p, typeErrors}
	}

	return nil
}

// parseFiles parses the loaded list of files and returns this list.
func (p *Package) parseFiles() ([]*ast.File, error) {
	var files []*ast.File
	var fileErrs []error

	parseFile := func(file string) {
		if !filepath.IsAbs(file) {
			file = filepath.Join(p.Dir, file)
		}
		f, err := p.parseFile(file, parser.ParseComments)
		if err != nil {
			fileErrs = append(fileErrs, err)
			return
		}
		files = append(files, f)
	}
	for _, file := range p.GoFiles {
		parseFile(file)
	}

	if len(fileErrs) != 0 {
		return nil, Errors{p, fileErrs}
	}

	return files, nil
}

// extractEmbedLines finds all //go:embed lines in the package and matches them
// against EmbedFiles from `moxie list`.
func (p *Package) extractEmbedLines(addError func(error)) {
	for _, file := range p.Files {
		// Check for an `import "embed"` line at the start of the file.
		// //go:embed lines are only valid if the given file itself imports the
		// embed package. It is not valid if it is only imported in a separate
		// Go file.
		hasEmbed := false
		for _, importSpec := range file.Imports {
			if importSpec.Path.Value == `"embed"` {
				hasEmbed = true
			}
		}

		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				if decl.Tok != token.VAR {
					continue
				}
				for _, spec := range decl.Specs {
					spec := spec.(*ast.ValueSpec)
					var doc *ast.CommentGroup
					if decl.Lparen == token.NoPos {
						// Plain 'var' declaration, like:
						//   //go:embed hello.txt
						//   var hello string
						doc = decl.Doc
					} else {
						// Bigger 'var' declaration like:
						//   var (
						//       //go:embed hello.txt
						//       hello string
						//   )
						doc = spec.Doc
					}
					if doc == nil {
						continue
					}

					// Look for //go:embed comments.
					var allPatterns []string
					for _, comment := range doc.List {
						if comment.Text != "//go:embed" && !strings.HasPrefix(comment.Text, "//go:embed ") {
							continue
						}
						if !hasEmbed {
							addError(types.Error{
								Fset: p.program.fset,
								Pos:  comment.Pos() + 2,
								Msg:  "//go:embed only allowed in Go files that import \"embed\"",
							})
							// Continue, because otherwise we might run into
							// issues below.
							continue
						}
						patterns, err := p.parseGoEmbed(comment.Text[len("//go:embed"):], comment.Slash)
						if err != nil {
							addError(err)
							continue
						}
						if len(patterns) == 0 {
							addError(types.Error{
								Fset: p.program.fset,
								Pos:  comment.Pos() + 2,
								Msg:  "usage: //go:embed pattern...",
							})
							continue
						}
						for _, pattern := range patterns {
							// Check that the pattern is well-formed.
							// It must be valid: the Go toolchain has already
							// checked for invalid patterns. But let's check
							// anyway to be sure.
							if _, err := path.Match(pattern, ""); err != nil {
								addError(types.Error{
									Fset: p.program.fset,
									Pos:  comment.Pos(),
									Msg:  "invalid pattern syntax",
								})
								continue
							}
							allPatterns = append(allPatterns, pattern)
						}
					}

					if len(allPatterns) != 0 {
						// This is a //go:embed global. Do a few more checks.
						if len(spec.Names) != 1 {
							addError(types.Error{
								Fset: p.program.fset,
								Pos:  spec.Names[1].NamePos,
								Msg:  "//go:embed cannot apply to multiple vars",
							})
						}
						if spec.Values != nil {
							addError(types.Error{
								Fset: p.program.fset,
								Pos:  spec.Values[0].Pos(),
								Msg:  "//go:embed cannot apply to var with initializer",
							})
						}
						globalName := spec.Names[0].Name
						globalType := p.Pkg.Scope().Lookup(globalName).Type()
						valid, byteSlice := isValidEmbedType(globalType)
						if !valid {
							addError(types.Error{
								Fset: p.program.fset,
								Pos:  spec.Type.Pos(),
								Msg:  "//go:embed cannot apply to var of type " + globalType.String(),
							})
						}

						// Match all //go:embed patterns against the embed files
						// provided by `go list`.
						for _, name := range p.EmbedFiles {
							for _, pattern := range allPatterns {
								if matchPattern(pattern, name) {
									p.EmbedGlobals[globalName] = append(p.EmbedGlobals[globalName], &EmbedFile{
										Name:      name,
										NeedsData: byteSlice,
									})
									break
								}
							}
						}
					}
				}
			}
		}
	}
}

// matchPattern returns true if (and only if) the given pattern would match the
// filename. The pattern could also match a parent directory of name, in which
// case hidden files do not match.
func matchPattern(pattern, name string) bool {
	// Match this file.
	matched, _ := path.Match(pattern, name)
	if matched {
		return true
	}

	// Match parent directories.
	dir := name
	for {
		dir, _ = path.Split(dir)
		if dir == "" {
			return false
		}
		dir = path.Clean(dir)
		if matched, _ := path.Match(pattern, dir); matched {
			// Pattern matches the directory.
			suffix := name[len(dir):]
			if strings.Contains(suffix, "/_") || strings.Contains(suffix, "/.") {
				// Pattern matches a hidden file.
				// Hidden files are included when listed directly as a
				// pattern, but not when they are part of a directory tree.
				// Source:
				// > If a pattern names a directory, all files in the
				// > subtree rooted at that directory are embedded
				// > (recursively), except that files with names beginning
				// > with ‘.’ or ‘_’ are excluded.
				return false
			}
			return true
		}
	}
}

// parseGoEmbed is like strings.Fields but for a //go:embed line. It parses
// regular fields and quoted fields (that may contain spaces).
func (p *Package) parseGoEmbed(args string, pos token.Pos) (patterns []string, err error) {
	args = strings.TrimSpace(args)
	initialLen := len(args)
	for args != "" {
		patternPos := pos + token.Pos(initialLen-len(args))
		switch args[0] {
		case '`', '"', '\\':
			// Parse the next pattern using the Go scanner.
			// This is perhaps a bit overkill, but it does correctly implement
			// parsing of the various Go strings.
			var sc scanner.Scanner
			fset := &token.FileSet{}
			file := fset.AddFile("", 0, len(args))
			sc.Init(file, []byte(args), nil, 0)
			_, tok, lit := sc.Scan()
			if tok != token.STRING || sc.ErrorCount != 0 {
				// Calculate start of token
				return nil, types.Error{
					Fset: p.program.fset,
					Pos:  patternPos,
					Msg:  "invalid quoted string in //go:embed",
				}
			}
			pattern := constant.StringVal(constant.MakeFromLiteral(lit, tok, 0))
			patterns = append(patterns, pattern)
			args = strings.TrimLeftFunc(args[len(lit):], unicode.IsSpace)
		default:
			// The value is just a regular value.
			// Split it at the first white space.
			index := strings.IndexFunc(args, unicode.IsSpace)
			if index < 0 {
				index = len(args)
			}
			pattern := args[:index]
			patterns = append(patterns, pattern)
			args = strings.TrimLeftFunc(args[len(pattern):], unicode.IsSpace)
		}
		if _, err := path.Match(patterns[len(patterns)-1], ""); err != nil {
			return nil, types.Error{
				Fset: p.program.fset,
				Pos:  patternPos,
				Msg:  "invalid pattern syntax",
			}
		}
	}
	return patterns, nil
}

// isValidEmbedType returns whether the given Go type can be used as a
// //go:embed type. This is only true for embed.FS, strings, and byte slices.
// The second return value indicates that this is a byte slice, and therefore
// the contents of the file needs to be passed to the compiler.
func isValidEmbedType(typ types.Type) (valid, byteSlice bool) {
	if typ.Underlying() == types.Typ[types.String] {
		// string type
		return true, false
	}
	if sliceType, ok := typ.Underlying().(*types.Slice); ok {
		if elemType, ok := sliceType.Elem().Underlying().(*types.Basic); ok && elemType.Kind() == types.Byte {
			// byte slice type
			return true, true
		}
	}
	if namedType, ok := typ.(*types.Named); ok && namedType.String() == "embed.FS" {
		// embed.FS type
		return true, false
	}
	return false, false
}

// Import implements types.Importer. It loads and parses packages it encounters
// along the way, if needed.
func (p *Package) Import(to string) (*types.Package, error) {
	if to == "unsafe" {
		return types.Unsafe, nil
	}
	if newTo, ok := p.ImportMap[to]; ok && !strings.HasSuffix(newTo, ".test]") {
		to = newTo
	}
	if imported, ok := p.program.Packages[to]; ok {
		return imported.Pkg, nil
	} else {
		return nil, errors.New("package not imported: " + to)
	}
}
