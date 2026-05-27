package typecheck

// Package represents a compiled Moxie package.
type Package struct {
	path     string
	name     string
	scope    *Scope
	imports  []*Package
	complete bool
}

func NewPackage(path, name string) *Package {
	return &Package{
		path:  path,
		name:  name,
		scope: NewScope(nil),
	}
}

func (p *Package) Path() string    { return p.path }
func (p *Package) Name() string    { return p.name }
func (p *Package) Scope() *Scope   { return p.scope }
func (p *Package) Complete() bool  { return p.complete }
func (p *Package) MarkComplete()   { p.complete = true }
func (p *Package) String() string  { return "package " + p.name + " (" + p.path + ")" }

func (p *Package) Imports() []*Package { return p.imports }
func (p *Package) SetImports(imps []*Package) { p.imports = imps }
