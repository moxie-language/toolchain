package typecheck

import (
	"fmt"
	"go/constant"
	"moxie/syntax"
)

// Importer resolves import paths to Packages.
type Importer interface {
	Import(path string) (*Package, error)
}

// Config controls type checker behavior.
type Config struct {
	Importer Importer
	Error    func(err error) // if nil, first error is returned and checking stops
}

// Checker is the Moxie type checker.
// Create one with NewChecker, then call Files to type-check a package.
type Checker struct {
	conf         *Config
	pkg          *Package
	info         *Info
	files        []*syntax.File
	iota         int64  // current iota value
	errors       []error
	localScope   *Scope // innermost scope during function body checking; nil at pkg level
}

// NewChecker creates a checker for pkg. info receives the type information.
func NewChecker(conf *Config, pkg *Package, info *Info) *Checker {
	return &Checker{conf: conf, pkg: pkg, info: info}
}

// Check type-checks the given files as package pkg.
// It returns an error if type checking fails.
func Check(path string, files []*syntax.File, importer Importer) (*Package, *Info, error) {
	pkg := NewPackage(path, packageName(files))
	info := newInfo()
	conf := &Config{Importer: importer}
	c := NewChecker(conf, pkg, info)
	if err := c.Files(files); err != nil {
		return pkg, info, err
	}
	return pkg, info, nil
}

// packageName returns the package name declared in the first file, or "main".
func packageName(files []*syntax.File) string {
	for _, f := range files {
		if f.PkgName != nil {
			return f.PkgName.Value
		}
	}
	return "main"
}

// Files type-checks the given files as the checker's package.
func (c *Checker) Files(files []*syntax.File) error {
	c.files = files

	// Pass 1: collect all package-level declarations into pkg.scope.
	for _, f := range files {
		c.collectDecls(f)
	}

	// Pass 2a: resolve named types first (TypeDecl only) so that method
	// registration in 2b can always find the Named type regardless of
	// declaration order.
	for _, f := range files {
		for _, decl := range f.DeclList {
			if d, ok := decl.(*syntax.TypeDecl); ok {
				c.checkTypeDecl(d)
			}
		}
	}

	// Pass 2b: resolve vars and functions (including method registration).
	for _, f := range files {
		for _, decl := range f.DeclList {
			switch decl.(type) {
			case *syntax.TypeDecl:
				// already done
			default:
				c.checkDecl(decl)
			}
		}
	}

	// Pass 3: check function bodies.
	for _, f := range files {
		for _, decl := range f.DeclList {
			if fd, ok := decl.(*syntax.FuncDecl); ok {
				c.checkFuncBody(fd)
			}
		}
	}

	if len(c.errors) > 0 {
		return c.errors[0]
	}
	return nil
}

// error records a type error at pos.
func (c *Checker) errorf(pos syntax.Pos, format string, args ...interface{}) {
	err := &TypeError{Pos: pos, Msg: fmt.Sprintf(format, args...)}
	if c.conf.Error != nil {
		c.conf.Error(err)
		c.errors = append(c.errors, err)
	} else {
		c.errors = append(c.errors, err)
	}
}

// TypeError is a type-checking error with position info.
type TypeError struct {
	Pos syntax.Pos
	Msg string
}

func (e *TypeError) Error() string {
	return fmt.Sprintf("%s: %s", e.Pos, e.Msg)
}

// record stores type info for an expression.
func (c *Checker) record(e syntax.Expr, tv TypeAndValue) {
	if c.info != nil {
		c.info.Types[e] = tv
	}
	stv := syntax.TypeAndValue{Type: tv.Type}
	if tv.Value != nil {
		if cv, ok := tv.Value.(constant.Value); ok {
			stv.Value = cv
		}
	}
	e.SetTypeInfo(stv)
}

// openScope creates a child scope of s and records it for node n.
func (c *Checker) openScope(n syntax.Node, parent *Scope) *Scope {
	s := NewScope(parent)
	if c.info != nil {
		c.info.Scopes[n] = s
	}
	return s
}

// lookup finds a named object starting from s, then universe.
func (c *Checker) lookup(name string, s *Scope) (*Scope, Object) {
	if found, obj := s.LookupParent(name); obj != nil {
		return found, obj
	}
	if obj := Universe.Lookup(name); obj != nil {
		return Universe, obj
	}
	return nil, nil
}

// lookupType finds a type name in localScope (if set), then package scope, then universe.
// Used by resolveTypeName to find locally-defined types (e.g. inside function bodies).
func (c *Checker) lookupType(name string) (*Scope, Object) {
	if c.localScope != nil {
		if sc, obj := c.localScope.LookupParent(name); obj != nil {
			return sc, obj
		}
	}
	return c.lookup(name, c.pkg.scope)
}
