// Package parse provides a parser for .mxh protocol header files.
package parse

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// MXH is the parsed content of a .mxh file.
type MXH struct {
	Hash    string // FNV-1a hex hash of the body (from header line)
	Version string // module version from moxie.mod (e.g. "1.2.0"), may be empty
	Library string // shared library path (e.g. "libfoo.so") for FFI packages
	Types   []ExternalTypeDef
	Funcs   []ExternalFuncDef
}

// ExternalTypeDef is a single codec type exported in a .mxh.
type ExternalTypeDef struct {
	Name          string
	Fields        []FieldDef
	HasEncodeTo   bool
	HasDecodeFrom bool
}

// FieldDef is a single exported fixed-width struct field.
type FieldDef struct {
	Name string
	Type string // e.g. "uint32", "[20]byte"
}

// ExternalFuncDef declares an FFI function from a shared library.
type ExternalFuncDef struct {
	Name    string
	Symbol  string // C symbol name (defaults to Name if empty)
	Params  []ParamDef
	Results []ParamDef
}

// ParamDef is a parameter or result in a function signature.
type ParamDef struct {
	Name string
	Type string
}

// ParseMXH reads a .mxh file from r and returns the parsed header.
func ParseMXH(r io.Reader) (*MXH, error) {
	sc := bufio.NewScanner(r)

	// First line must be the hash header.
	if !sc.Scan() {
		return nil, fmt.Errorf("mxh: empty file")
	}
	header := sc.Text()
	hash, version, err := parseHashLine(header)
	if err != nil {
		return nil, err
	}

	mxh := &MXH{Hash: hash, Version: version}
	var cur *ExternalTypeDef

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		switch {
		case strings.HasPrefix(line, "library "):
			mxh.Library = parseLibraryLine(line)

		case strings.HasPrefix(line, "func ") && !strings.HasPrefix(line, "func (") && !strings.HasPrefix(line, "func (*"):
			if fd, err := parseFuncLine(line); err == nil {
				mxh.Funcs = append(mxh.Funcs, fd)
			}

		case strings.HasPrefix(line, "type ") && strings.HasSuffix(line, " struct {"):
			name := line[len("type ") : len(line)-len(" struct {")]
			name = strings.TrimSpace(name)
			mxh.Types = append(mxh.Types, ExternalTypeDef{Name: name})
			cur = &mxh.Types[len(mxh.Types)-1]

		case line == "}":
			cur = nil

		case strings.HasPrefix(line, "func (") && strings.Contains(line, ") EncodeTo("):
			if cur == nil {
				cur = findTypeBySig(mxh, line)
			}
			if cur != nil {
				cur.HasEncodeTo = true
				cur = nil
			}

		case strings.HasPrefix(line, "func (*") && strings.Contains(line, ") DecodeFrom("):
			name := extractMethodReceiverName(line)
			if t := findTypeByName(mxh, name); t != nil {
				t.HasDecodeFrom = true
			}

		case cur != nil:
			parts := strings.Fields(line)
			if len(parts) == 2 {
				cur.Fields = append(cur.Fields, FieldDef{Name: parts[0], Type: parts[1]})
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("mxh: scan error: %w", err)
	}
	return mxh, nil
}

func parseHashLine(line string) (hash, version string, err error) {
	// Expected: "// mxh v1 <hex>" or "// mxh v1 <hex> <version>"
	const prefix = "// mxh v1 "
	if !strings.HasPrefix(line, prefix) {
		return "", "", fmt.Errorf("mxh: invalid header line %q (expected %q...)", line, prefix)
	}
	rest := strings.TrimSpace(line[len(prefix):])
	if rest == "" {
		return "", "", fmt.Errorf("mxh: empty hash in header line")
	}
	parts := strings.Fields(rest)
	hash = parts[0]
	if len(parts) > 1 {
		version = parts[1]
	}
	return hash, version, nil
}

// findTypeBySig extracts the receiver type name from a value-receiver
// EncodeTo signature: "func (Foo) EncodeTo(..."
func findTypeBySig(mxh *MXH, line string) *ExternalTypeDef {
	// "func (Name) EncodeTo..."
	start := strings.Index(line, "(")
	end := strings.Index(line, ")")
	if start < 0 || end < 0 || end <= start+1 {
		return nil
	}
	name := strings.TrimSpace(line[start+1 : end])
	return findTypeByName(mxh, name)
}

// extractMethodReceiverName extracts the type name from a pointer-receiver
// signature: "func (*Foo) DecodeFrom(..."
func extractMethodReceiverName(line string) string {
	start := strings.Index(line, "(*")
	end := strings.Index(line, ")")
	if start < 0 || end < 0 || end <= start+2 {
		return ""
	}
	return strings.TrimSpace(line[start+2 : end])
}

func findTypeByName(mxh *MXH, name string) *ExternalTypeDef {
	for i := range mxh.Types {
		if mxh.Types[i].Name == name {
			return &mxh.Types[i]
		}
	}
	return nil
}

func parseLibraryLine(line string) string {
	s := strings.TrimPrefix(line, "library ")
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// parseFuncLine parses: func Name(a int32, b int32) int32
// or: func Name(a int32, b int32) (int32, error)
// or: func Name(a int32) as "c_symbol_name"
func parseFuncLine(line string) (ExternalFuncDef, error) {
	line = strings.TrimPrefix(line, "func ")
	line = strings.TrimSpace(line)

	// Extract function name.
	paren := strings.IndexByte(line, '(')
	if paren < 0 {
		return ExternalFuncDef{}, fmt.Errorf("mxh func: no opening paren")
	}
	name := strings.TrimSpace(line[:paren])
	rest := line[paren:]

	// Find matching closing paren for params.
	params, after, err := extractParenGroup(rest)
	if err != nil {
		return ExternalFuncDef{}, err
	}

	fd := ExternalFuncDef{Name: name}
	fd.Params = parseParamList(params)

	after = strings.TrimSpace(after)

	// Check for "as" clause: as "c_name"
	if asIdx := strings.Index(after, " as "); asIdx >= 0 {
		sym := strings.TrimSpace(after[asIdx+4:])
		if len(sym) >= 2 && sym[0] == '"' && sym[len(sym)-1] == '"' {
			fd.Symbol = sym[1 : len(sym)-1]
		}
		after = strings.TrimSpace(after[:asIdx])
	}

	// Parse return types.
	if after != "" {
		if after[0] == '(' {
			results, _, err := extractParenGroup(after)
			if err == nil {
				fd.Results = parseParamList(results)
			}
		} else {
			fd.Results = []ParamDef{{Type: after}}
		}
	}

	if fd.Symbol == "" {
		fd.Symbol = fd.Name
	}
	return fd, nil
}

func extractParenGroup(s string) (string, string, error) {
	if len(s) == 0 || s[0] != '(' {
		return "", s, fmt.Errorf("expected '('")
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '(' {
			depth++
		} else if s[i] == ')' {
			depth--
			if depth == 0 {
				return s[1:i], s[i+1:], nil
			}
		}
	}
	return "", "", fmt.Errorf("unmatched parenthesis")
}

func parseParamList(s string) []ParamDef {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var params []ParamDef
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Fields(part)
		switch len(fields) {
		case 1:
			params = append(params, ParamDef{Type: fields[0]})
		case 2:
			params = append(params, ParamDef{Name: fields[0], Type: fields[1]})
		}
	}
	return params
}
