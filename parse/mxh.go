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
	Hash  string        // FNV-1a hex hash of the body (from header line)
	Types []ExternalTypeDef
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

// ParseMXH reads a .mxh file from r and returns the parsed header.
func ParseMXH(r io.Reader) (*MXH, error) {
	sc := bufio.NewScanner(r)

	// First line must be the hash header.
	if !sc.Scan() {
		return nil, fmt.Errorf("mxh: empty file")
	}
	header := sc.Text()
	hash, err := parseHashLine(header)
	if err != nil {
		return nil, err
	}

	mxh := &MXH{Hash: hash}
	var cur *ExternalTypeDef

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		switch {
		case strings.HasPrefix(line, "type ") && strings.HasSuffix(line, " struct {"):
			// type Foo struct {
			name := line[len("type ") : len(line)-len(" struct {")]
			name = strings.TrimSpace(name)
			mxh.Types = append(mxh.Types, ExternalTypeDef{Name: name})
			cur = &mxh.Types[len(mxh.Types)-1]

		case line == "}":
			cur = nil

		case strings.HasPrefix(line, "func (") && strings.Contains(line, ") EncodeTo("):
			if cur == nil {
				// method signature for most recent type
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
			// Field line inside struct block: "    Name Type"
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

func parseHashLine(line string) (string, error) {
	// Expected: "// mxh v1 <hex>"
	const prefix = "// mxh v1 "
	if !strings.HasPrefix(line, prefix) {
		return "", fmt.Errorf("mxh: invalid header line %q (expected %q...)", line, prefix)
	}
	hash := strings.TrimSpace(line[len(prefix):])
	if hash == "" {
		return "", fmt.Errorf("mxh: empty hash in header line")
	}
	return hash, nil
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
