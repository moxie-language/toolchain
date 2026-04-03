package gotypes

// godebug stub — replaces internal/godebug for the forked go/types.
type godebugSetting struct{ name string }

func godebugNew(name string) *godebugSetting { return &godebugSetting{name: name} }
func (s *godebugSetting) Value() string       { return "" }
func (s *godebugSetting) IncNonDefault()      {}
