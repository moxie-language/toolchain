package typecheck

// Scope is a mapping from names to Objects. Scopes are chained:
// a lookup that misses in the current scope walks up to the parent.
type Scope struct {
	parent   *Scope
	children []*Scope
	elems    map[string]Object
}

func NewScope(parent *Scope) *Scope {
	return &Scope{parent: parent, elems: map[string]Object{}}
}

func (s *Scope) Parent() *Scope   { return s.parent }
func (s *Scope) Len() int         { return len(s.elems) }

func (s *Scope) Lookup(name string) Object {
	return s.elems[name]
}

// LookupParent looks up name starting at s, walking the parent chain.
// Returns the scope where it was found and the object, or nil, nil.
func (s *Scope) LookupParent(name string) (*Scope, Object) {
	for sc := s; sc != nil; sc = sc.parent {
		if obj, ok := sc.elems[name]; ok {
			return sc, obj
		}
	}
	return nil, nil
}

// Insert inserts obj into s. Returns an existing object with the same name
// if there is one (caller must report the error).
func (s *Scope) Insert(obj Object) Object {
	name := obj.Name()
	if alt, ok := s.elems[name]; ok {
		return alt
	}
	s.elems[name] = obj
	return nil
}

// Names returns all names in this scope, unsorted.
func (s *Scope) Names() []string {
	names := make([]string, 0, len(s.elems))
	for n := range s.elems {
		names = append(names, n)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}
