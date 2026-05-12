package loader

import (
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"moxie/goenv"
	"moxie/parse"
)

// MXHProtocolsDir returns the path to the target-independent protocols
// cache where .mxh files are stored.
func MXHProtocolsDir() string {
	return filepath.Join(goenv.Get("GOCACHE"), "mxinstall", "protocols")
}

// synthMXHPackage reads a .mxh file from path and returns a synthetic
// *types.Package containing the declared struct types. Methods are not
// declared; the compiler uses IsExternalMXH to identify these types.
func synthMXHPackage(importPath, mxhPath string) (*types.Package, error) {
	f, err := os.Open(mxhPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	mxh, err := parse.ParseMXH(f)
	if err != nil {
		return nil, fmt.Errorf("mxh %s: %w", mxhPath, err)
	}

	// Derive package name from the last segment of the import path.
	pkgName := importPath
	if i := strings.LastIndexByte(pkgName, '/'); i >= 0 {
		pkgName = pkgName[i+1:]
	}

	pkg := types.NewPackage(importPath, pkgName)

	for _, td := range mxh.Types {
		var vars []*types.Var
		for _, fd := range td.Fields {
			ft, err := parseTypeString(fd.Type)
			if err != nil {
				return nil, fmt.Errorf("mxh %s type %s field %s: %w", mxhPath, td.Name, fd.Name, err)
			}
			vars = append(vars, types.NewVar(token.NoPos, pkg, fd.Name, ft))
		}

		underlying := types.NewStruct(vars, nil)
		typeName := types.NewTypeName(token.NoPos, pkg, td.Name, nil)
		types.NewNamed(typeName, underlying, nil)

		// Export the type name into the package scope.
		pkg.Scope().Insert(typeName)
	}

	pkg.MarkComplete()
	return pkg, nil
}

// parseTypeString maps a type string from a .mxh file to a types.Type.
// Supports all basic types and fixed-size arrays like [20]byte.
func parseTypeString(s string) (types.Type, error) {
	switch s {
	case "bool":
		return types.Typ[types.Bool], nil
	case "int8":
		return types.Typ[types.Int8], nil
	case "int16":
		return types.Typ[types.Int16], nil
	case "int32", "int":
		return types.Typ[types.Int32], nil
	case "int64":
		return types.Typ[types.Int64], nil
	case "uint8", "byte":
		return types.Typ[types.Uint8], nil
	case "uint16":
		return types.Typ[types.Uint16], nil
	case "uint32", "uint":
		return types.Typ[types.Uint32], nil
	case "uint64":
		return types.Typ[types.Uint64], nil
	case "float32":
		return types.Typ[types.Float32], nil
	case "float64":
		return types.Typ[types.Float64], nil
	case "string":
		return types.Typ[types.String], nil
	}

	// Array types: [N]elem
	if strings.HasPrefix(s, "[") {
		bracket := strings.Index(s, "]")
		if bracket < 0 {
			return nil, fmt.Errorf("invalid array type %q", s)
		}
		nStr := s[1:bracket]
		n, err := strconv.ParseInt(nStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid array length in %q: %w", s, err)
		}
		elem, err := parseTypeString(s[bracket+1:])
		if err != nil {
			return nil, fmt.Errorf("invalid array element in %q: %w", s, err)
		}
		return types.NewArray(elem, n), nil
	}

	return nil, fmt.Errorf("unsupported type %q in .mxh (only fixed-width scalar and array types are allowed)", s)
}

// mxhHashForImport returns the hash from the cached .mxh for importPath,
// or an empty string if no .mxh is cached.
func MXHHashForImport(importPath string) string {
	mxhPath := filepath.Join(MXHProtocolsDir(), importPath+".mxh")
	f, err := os.Open(mxhPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	mxh, err := parse.ParseMXH(f)
	if err != nil {
		return ""
	}
	return mxh.Hash
}
