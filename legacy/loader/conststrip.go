package loader

// Pre-parse dead constant elimination. Mirrors stage4's stripUnusedConsts in
// ir_typecheck.mx: a package-level single-line const declaration whose name is
// referenced nowhere else in the package is blanked before the parser sees it.
// Generated tables (zerrors, zsysnum) are thousands of unreferenced constants
// and there is no reason to parse or type-check them.

// collectConstRefs records every identifier in src except the name each
// "const NAME" declaration introduces. The declared name is located by
// position: the identifier at that exact span is the declaration, not a
// reference. The whole source is scanned, not just function bodies, because a
// const used solely by a var initializer or a type declaration is still used.
func collectConstRefs(src []byte, refs map[string]bool) {
	i := 0
	for i < len(src) {
		// The name a single-line "const NAME" declares is not a reference.
		declStart, declEnd := -1, -1
		ls := i
		for ls < len(src) && (src[ls] == ' ' || src[ls] == '\t') {
			ls++
		}
		if ls+6 <= len(src) && src[ls] == 'c' && src[ls+1] == 'o' && src[ls+2] == 'n' && src[ls+3] == 's' && src[ls+4] == 't' && src[ls+5] == ' ' {
			ns := ls + 6
			for ns < len(src) && (src[ns] == ' ' || src[ns] == '\t') {
				ns++
			}
			ne := ns
			for ne < len(src) && isIdentCont(src[ne]) {
				ne++
			}
			if ne > ns {
				declStart, declEnd = ns, ne
			}
		}
		for i < len(src) && src[i] != '\n' {
			if isIdentStart(src[i]) {
				j := i + 1
				for j < len(src) && isIdentCont(src[j]) {
					j++
				}
				if i != declStart || j != declEnd {
					refs[string(src[i:j])] = true
				}
				i = j
				continue
			}
			i++
		}
		if i < len(src) {
			i++
		}
	}
}

// stripUnusedConsts returns src with every unreferenced, unexported,
// single-line const declaration blanked. Only individual const lines are
// stripped; grouped "const (" blocks are untouched.
func stripUnusedConsts(src []byte, refs map[string]bool) []byte {
	out := make([]byte, 0, len(src))
	i := 0
	for i < len(src) {
		// Look for "const " at line start (after optional whitespace/tab).
		ls := i
		for ls < len(src) && (src[ls] == ' ' || src[ls] == '\t') {
			ls++
		}
		if ls+6 < len(src) && src[ls] == 'c' && src[ls+1] == 'o' && src[ls+2] == 'n' && src[ls+3] == 's' && src[ls+4] == 't' && src[ls+5] == ' ' {
			ns := ls + 6
			for ns < len(src) && (src[ns] == ' ' || src[ns] == '\t') {
				ns++
			}
			ne := ns
			for ne < len(src) && isIdentCont(src[ne]) {
				ne++
			}
			if ne > ns {
				cname := string(src[ns:ne])
				// Exported consts are kept: an importer may use one, e.g.
				// sha256.BlockSize as an array length, and that use is not
				// visible in this package's source.
				exported := cname[0] >= 'A' && cname[0] <= 'Z'
				if !refs[cname] && !exported && constLineIsComplete(src, ls, ne) {
					// Blank the line rather than deleting it. stage4 parses a
					// concatenated package body and maps line numbers back to
					// file:line, so dropping a line moved every later position
					// and a parse error was reported against the wrong file
					// entirely. Keeping the '\n' keeps that mapping and both
					// compilers identical.
					out = append(out, '/', '/')
					for i < len(src) && src[i] != '\n' {
						i++
					}
					if i < len(src) {
						out = append(out, '\n')
						i++
					}
					continue
				}
			}
		}
		for i < len(src) && src[i] != '\n' {
			out = append(out, src[i])
			i++
		}
		if i < len(src) {
			out = append(out, '\n')
			i++
		}
	}
	return out
}

// constLineIsComplete reports whether a single-line "const NAME = value"
// declaration is wholly contained in its line. The stripper blanks one LINE per
// unreferenced constant, so a declaration whose value continues on the next
// line must be left alone: blanking "const c =" leaves the value behind and the
// parser reports "non-declaration statement outside function body" (x/net/idna
// has 525 such generator-table lines). A line is complete when an '=' follows
// the name and the last significant character can end an expression rather than
// continue one.
func constLineIsComplete(src []byte, lineStart, nameEnd int) bool {
	le := lineStart
	for le < len(src) && src[le] != '\n' {
		le++
	}
	// A trailing line comment is not part of the declaration: x/net/idna's
	// 'const mappings = "" + // Size: 6704 bytes' would otherwise look like it
	// ends in the word "bytes" and a 525-line declaration would be stripped.
	end := le
	inStr := false
	var quote byte
	depth := 0
	for k := lineStart; k+1 < le; k++ {
		c := src[k]
		if inStr {
			if c == '\\' {
				k++
				continue
			}
			if c == quote {
				inStr = false
			}
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			inStr = true
			quote = c
			continue
		}
		if c == '/' && src[k+1] == '/' {
			end = k
			break
		}
		if c == '(' || c == '[' || c == '{' {
			depth++
		} else if c == ')' || c == ']' || c == '}' {
			if depth > 0 {
				depth--
			}
		}
	}
	// A bracket or string left open at end of line continues on the next line
	// (const x = []T{ / foo( ...); blanking its first line left the rest behind.
	if depth != 0 || inStr {
		return false
	}
	last := end
	for last > nameEnd && (src[last-1] == ' ' || src[last-1] == '\t' || src[last-1] == '\r') {
		last--
	}
	if last <= nameEnd {
		return false
	}
	sawAssign := false
	for k := nameEnd; k < last; k++ {
		if src[k] == '=' {
			sawAssign = true
			break
		}
	}
	if !sawAssign {
		return false
	}
	switch src[last-1] {
	case '=', '+', '-', '*', '/', '%', '|', '&', '^', '<', '>', '(', '[', '{', ',', '.', ':', '!':
		return false
	}
	return true
}

func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isIdentCont(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}
