package loader

import "go/types"

// ImplementsCodec reports whether t (or *t) satisfies the moxie.Codec
// interface as defined in the loaded program. Returns false if the moxie
// package is not present in the program.
func ImplementsCodec(t types.Type, prog *Program) bool {
	moxiePkg, ok := prog.Packages["moxie"]
	if !ok {
		return false
	}
	codecObj := moxiePkg.Pkg.Scope().Lookup("Codec")
	if codecObj == nil {
		return false
	}
	codecIface, ok := codecObj.Type().Underlying().(*types.Interface)
	if !ok {
		return false
	}
	return implementsCodec(t, codecIface)
}

// ImplementsCodecIface checks whether t or *t satisfies the given Codec
// interface value. Used by the compiler when it already has the interface
// resolved from the SSA program.
func ImplementsCodecIface(t types.Type, codec *types.Interface) bool {
	return types.Implements(t, codec) || types.Implements(types.NewPointer(t), codec)
}

// implementsCodec is the package-private alias for internal use.
func implementsCodec(t types.Type, codec *types.Interface) bool {
	return ImplementsCodecIface(t, codec)
}
