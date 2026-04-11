// rewrite.go — Moxie literal syntax preprocessing for moxiejs.
//
// Rewrites moxie-specific syntax to valid Go before parser.ParseFile:
//   []T{:len}      → make([]T, len)
//   []T{:len:cap}  → make([]T, len, cap)
//   chan T{}        → make(chan T)
//   chan T{expr}    → make(chan T, expr)
//
// Ported from moxie/loader/mxrewrite.go for the JS backend.

package main

import (
	"bytes"
	"go/scanner"
	"go/token"
	"strings"
)

// rewriteMoxieLiterals applies all text-level rewrites to moxie source.
func rewriteMoxieLiterals(src []byte) []byte {
	fset := token.NewFileSet()
	src = rewritePipeConcat(src, fset)
	src = rewriteChanLiterals(src, fset)
	src = rewriteSliceLiterals(src, fset)
	return src
}

// rewritePipeConcat converts `|` used as string concatenation to `+`.
// In Moxie, `|` is the text concat operator, but Go's type checker only
// accepts `+` for string concatenation. This rewrites `|` to `+` when
// the operands are string-typed (not bitwise integer OR).
//
// Heuristic: a `|` token is pipe-concat when it is NOT between two
// expressions that are clearly integer-typed. We detect integer context by
// checking if both adjacent tokens are integer literals, or if the `|` is
// inside a clearly bitwise context (e.g. `flags |= mask`).
//
// Conservative rule: rewrite `|` to `+` unless:
//   - it's `|=` (compound assign — always bitwise)
//   - both sides are integer literals
//   - it's inside a const block (const expressions are numeric)
func rewritePipeConcat(src []byte, fset *token.FileSet) []byte {
	toks := scanTokens(src, fset)
	if len(toks) == 0 {
		return src
	}

	// Identify const blocks to skip (| in const is always bitwise).
	constRanges := findConstRanges(toks)

	var result bytes.Buffer
	lastEnd := 0

	for i, t := range toks {
		if t.tok != token.OR {
			continue
		}
		// Skip |= (compound assign).
		if i+1 < len(toks) && toks[i+1].tok == token.ASSIGN && toks[i+1].pos == t.end {
			continue
		}
		// Skip if inside a const block.
		if inConstRange(t.pos, constRanges) {
			continue
		}
		// Skip if both neighbors are integer literals.
		if i > 0 && i+1 < len(toks) &&
			toks[i-1].tok == token.INT && toks[i+1].tok == token.INT {
			continue
		}
		// Rewrite | → +
		result.Write(src[lastEnd:t.pos])
		result.WriteByte('+')
		lastEnd = t.end
	}

	if lastEnd == 0 {
		return src
	}
	result.Write(src[lastEnd:])
	return result.Bytes()
}

// constRange marks byte offsets of const(...) blocks.
type constRange struct{ start, end int }

func findConstRanges(toks []tok) []constRange {
	var ranges []constRange
	for i := 0; i < len(toks); i++ {
		if toks[i].tok != token.CONST {
			continue
		}
		// const ( ... ) or const name = expr
		if i+1 < len(toks) && toks[i+1].tok == token.LPAREN {
			depth := 1
			start := toks[i].pos
			for j := i + 2; j < len(toks); j++ {
				if toks[j].tok == token.LPAREN {
					depth++
				}
				if toks[j].tok == token.RPAREN {
					depth--
					if depth == 0 {
						ranges = append(ranges, constRange{start, toks[j].end})
						i = j
						break
					}
				}
			}
		}
	}
	return ranges
}

func inConstRange(pos int, ranges []constRange) bool {
	for _, r := range ranges {
		if pos >= r.start && pos < r.end {
			return true
		}
	}
	return false
}

// tok is a scanned token with byte offsets.
type tok struct {
	pos int
	end int
	tok token.Token
	lit string
}

// scanTokens tokenizes src into a slice of tok.
func scanTokens(src []byte, fset *token.FileSet) []tok {
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, nil, scanner.ScanComments)

	var toks []tok
	for {
		pos, t, lit := s.Scan()
		if t == token.EOF {
			break
		}
		offset := file.Offset(pos)
		end := offset + len(lit)
		if lit == "" {
			end = offset + len(t.String())
		}
		toks = append(toks, tok{pos: offset, end: end, tok: t, lit: lit})
	}
	return toks
}

// rewriteChanLiterals rewrites chan T{} → make(chan T) and chan T{N} → make(chan T, N).
func rewriteChanLiterals(src []byte, fset *token.FileSet) []byte {
	toks := scanTokens(src, fset)

	var result bytes.Buffer
	lastEnd := 0

	for i := 0; i < len(toks); i++ {
		if toks[i].tok != token.CHAN {
			continue
		}

		chanIdx := i
		braceIdx := -1

		depth := 0
		for j := i + 1; j < len(toks); j++ {
			switch toks[j].tok {
			case token.LBRACE:
				if depth == 0 {
					braceIdx = j
				}
				depth++
			case token.RBRACE:
				depth--
			case token.LPAREN:
				depth++
			case token.RPAREN:
				depth--
			}
			if braceIdx >= 0 {
				break
			}
			if toks[j].tok == token.SEMICOLON || toks[j].tok == token.ASSIGN ||
				toks[j].tok == token.DEFINE || toks[j].tok == token.COMMA ||
				toks[j].tok == token.RPAREN {
				break
			}
		}

		if braceIdx < 0 || braceIdx <= chanIdx+1 {
			continue
		}

		// Check expression context.
		inExprContext := false
		if chanIdx > 0 {
			prev := toks[chanIdx-1].tok
			switch prev {
			case token.ASSIGN, token.DEFINE,
				token.COLON, token.COMMA,
				token.LPAREN, token.LBRACK,
				token.LBRACE, token.RETURN,
				token.SEMICOLON:
				inExprContext = true
			}
		} else {
			inExprContext = true
		}

		if !inExprContext {
			continue
		}

		// Find closing brace.
		closeIdx := -1
		depth = 1
		for j := braceIdx + 1; j < len(toks); j++ {
			switch toks[j].tok {
			case token.LBRACE:
				depth++
			case token.RBRACE:
				depth--
				if depth == 0 {
					closeIdx = j
				}
			}
			if closeIdx >= 0 {
				break
			}
		}

		if closeIdx < 0 {
			continue
		}

		// Extract type text.
		typeStart := toks[chanIdx+1].pos
		typeEnd := toks[braceIdx].pos
		typeText := strings.TrimSpace(string(src[typeStart:typeEnd]))

		if typeText == "" {
			continue
		}

		// Handle chan struct{}{} and chan interface{}{}.
		if typeText == "struct" || typeText == "interface" {
			if closeIdx+1 >= len(toks) || toks[closeIdx+1].tok != token.LBRACE {
				continue
			}
			typeText = typeText + "{}"
			braceIdx = closeIdx + 1
			closeIdx = -1
			depth = 1
			for j := braceIdx + 1; j < len(toks); j++ {
				switch toks[j].tok {
				case token.LBRACE:
					depth++
				case token.RBRACE:
					depth--
					if depth == 0 {
						closeIdx = j
					}
				}
				if closeIdx >= 0 {
					break
				}
			}
			if closeIdx < 0 {
				continue
			}
		}

		// Extract buffer size expression.
		var bufExpr string
		if closeIdx > braceIdx+1 {
			bufStart := toks[braceIdx+1].pos
			bufEnd := toks[closeIdx].pos
			bufExpr = strings.TrimSpace(string(src[bufStart:bufEnd]))
		}

		result.Write(src[lastEnd:toks[chanIdx].pos])
		result.WriteString("make(chan ")
		result.WriteString(typeText)
		if bufExpr != "" {
			result.WriteString(", ")
			result.WriteString(bufExpr)
		}
		result.WriteString(")")

		lastEnd = toks[closeIdx].end
		i = closeIdx
	}

	if lastEnd == 0 {
		return src
	}
	result.Write(src[lastEnd:])
	return result.Bytes()
}

// rewriteSliceLiterals rewrites []T{:len} → make([]T, len) and []T{:len:cap} → make([]T, len, cap).
func rewriteSliceLiterals(src []byte, fset *token.FileSet) []byte {
	toks := scanTokens(src, fset)

	var result bytes.Buffer
	lastEnd := 0

	for i := 0; i < len(toks); i++ {
		if toks[i].tok != token.LBRACK {
			continue
		}
		if i+1 >= len(toks) || toks[i+1].tok != token.RBRACK {
			continue
		}

		lbrackIdx := i

		// Scan forward past element type to find LBRACE.
		braceIdx := -1
		depth := 0
		for j := i + 2; j < len(toks); j++ {
			switch toks[j].tok {
			case token.LBRACK:
				depth++
			case token.RBRACK:
				depth--
			case token.LPAREN:
				depth++
			case token.RPAREN:
				depth--
			case token.LBRACE:
				if depth == 0 {
					braceIdx = j
				}
			}
			if braceIdx >= 0 {
				break
			}
			if depth == 0 && (toks[j].tok == token.SEMICOLON ||
				toks[j].tok == token.ASSIGN ||
				toks[j].tok == token.DEFINE ||
				toks[j].tok == token.COMMA) {
				break
			}
		}

		if braceIdx < 0 || braceIdx <= lbrackIdx+2 {
			continue
		}

		// Discriminator: token after { must be COLON.
		if braceIdx+1 >= len(toks) || toks[braceIdx+1].tok != token.COLON {
			continue
		}

		// Find closing brace, collecting colon positions.
		closeIdx := -1
		colonPositions := []int{braceIdx + 1}
		depth = 1
		bracketDepth := 0
		parenDepth := 0
		for j := braceIdx + 2; j < len(toks); j++ {
			switch toks[j].tok {
			case token.LBRACE:
				depth++
			case token.RBRACE:
				depth--
				if depth == 0 {
					closeIdx = j
				}
			case token.LBRACK:
				bracketDepth++
			case token.RBRACK:
				bracketDepth--
			case token.LPAREN:
				parenDepth++
			case token.RPAREN:
				parenDepth--
			case token.COLON:
				if depth == 1 && bracketDepth == 0 && parenDepth == 0 {
					colonPositions = append(colonPositions, j)
				}
			}
			if closeIdx >= 0 {
				break
			}
		}

		if closeIdx < 0 {
			continue
		}

		// Extract type text (from [ through to {, not including {).
		typeText := strings.TrimSpace(string(src[toks[lbrackIdx].pos:toks[braceIdx].pos]))

		// Detect the secure-allocator marker: a trailing `, secure` IDENT
		// before the closing brace. On the JS target the allocator is
		// best-effort — there is no mmap, no guard pages, no kernel
		// residency control — so the marker silently degrades to a plain
		// allocation. The active defence on JS is runtime.SecureClear,
		// which overwrites the buffer when the caller is done with it.
		secureMarker := false
		secureExprEnd := closeIdx
		if len(colonPositions) == 1 && closeIdx-2 > colonPositions[0] {
			lastTok := toks[closeIdx-1]
			prevTok := toks[closeIdx-2]
			if lastTok.tok == token.IDENT && lastTok.lit == "secure" &&
				prevTok.tok == token.COMMA {
				secureMarker = true
				secureExprEnd = closeIdx - 2
			}
		}

		if secureMarker {
			// []byte{:len, secure} → (make)([]byte, len)
			// Only []byte is supported, matching native. Parenthesised
			// make bypasses the restriction checker on named slice types.
			if typeText != "[]byte" {
				continue
			}
			lenStart := toks[colonPositions[0]+1].pos
			lenEnd := toks[secureExprEnd].pos
			lenExpr := strings.TrimSpace(string(src[lenStart:lenEnd]))
			if lenExpr == "" {
				continue
			}
			result.Write(src[lastEnd:toks[lbrackIdx].pos])
			result.WriteString("(make)([]byte, ")
			result.WriteString(lenExpr)
			result.WriteString(")")
		} else if len(colonPositions) == 1 {
			// []T{:len} → make([]T, len)
			lenStart := toks[colonPositions[0]+1].pos
			lenEnd := toks[closeIdx].pos
			lenExpr := strings.TrimSpace(string(src[lenStart:lenEnd]))
			if lenExpr == "" {
				continue
			}

			result.Write(src[lastEnd:toks[lbrackIdx].pos])
			result.WriteString("make(")
			result.WriteString(typeText)
			result.WriteString(", ")
			result.WriteString(lenExpr)
			result.WriteString(")")
		} else if len(colonPositions) == 2 {
			// []T{:len:cap} → make([]T, len, cap)
			lenStart := toks[colonPositions[0]+1].pos
			lenEnd := toks[colonPositions[1]].pos
			lenExpr := strings.TrimSpace(string(src[lenStart:lenEnd]))

			capStart := toks[colonPositions[1]+1].pos
			capEnd := toks[closeIdx].pos
			capExpr := strings.TrimSpace(string(src[capStart:capEnd]))

			if lenExpr == "" || capExpr == "" {
				continue
			}

			result.Write(src[lastEnd:toks[lbrackIdx].pos])
			result.WriteString("make(")
			result.WriteString(typeText)
			result.WriteString(", ")
			result.WriteString(lenExpr)
			result.WriteString(", ")
			result.WriteString(capExpr)
			result.WriteString(")")
		} else {
			continue
		}

		lastEnd = toks[closeIdx].end
		i = closeIdx
	}

	if lastEnd == 0 {
		return src
	}
	result.Write(src[lastEnd:])
	return result.Bytes()
}
