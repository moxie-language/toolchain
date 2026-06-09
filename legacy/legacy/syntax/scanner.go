// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file implements scanner, a lexical tokenizer for
// Go source. After initialization, consecutive calls of
// next advance the scanner one token at a time.
//
// This file, source.go, tokens.go, and token_string.go are self-contained
// (`go tool compile scanner.go source.go tokens.go token_string.go` compiles)
// and thus could be made into their own package.

package syntax

import (
	"fmt"
	"io"
	"unicode"
	"unicode/utf8"
)

// The mode flags below control which comments are reported
// by calling the error handler. If no flag is set, comments
// are ignored.
const (
	comments   uint = 1 << iota // call handler for all comments
	directives                  // call handler for directives only
)

type Scanner struct {
	Source
	mode   uint
	nlsemi bool // if set '\n' and EOF translate to ';'

	// current token, valid after calling next()
	Line, Col uint32
	Blank     bool // line is blank up to col
	Tok       Token
	Lit       string   // valid if tok is _Name, _Literal, or _Semi ("semicolon", "newline", or "EOF"); may be malformed if bad is true
	Bad       bool     // valid if tok is _Literal, true if a syntax error occurred, lit may be malformed
	Kind      LitKind  // valid if tok is _Literal
	Op        Operator // valid if tok is _Operator, _Star, _AssignOp, or _IncOp
	Prec      int32      // valid if tok is _Operator, _Star, _AssignOp, or _IncOp
}

func (s *Scanner) Init(src io.Reader, errh func(line, col uint32, msg string), mode uint) {
	s.Source.init(src, errh)
	s.mode = mode
	s.nlsemi = false
}

// errorf reports an error at the most recently read character position.
func (s *Scanner) Errorf(format string, args ...interface{}) {
	s.error(fmt.Sprintf(format, args...))
}

// errorAtf reports an error at a byte column offset relative to the current token start.
func (s *Scanner) ErrorAtf(offset int32, format string, args ...interface{}) {
	s.errh(s.line, s.col+uint32(offset), fmt.Sprintf(format, args...))
}

// setLit sets the scanner state for a recognized _Literal token.
func (s *Scanner) SetLit(kind LitKind, ok bool) {
	s.nlsemi = true
	s.Tok = Literal
	s.Lit = string(s.segment())
	s.Bad = !ok
	s.Kind = kind
}

// next advances the scanner by reading the next token.
//
// If a read, source encoding, or lexical error occurs, next calls
// the installed error handler with the respective error position
// and message. The error message is guaranteed to be non-empty and
// never starts with a '/'. The error handler must exist.
//
// If the scanner mode includes the comments flag and a comment
// (including comments containing directives) is encountered, the
// error handler is also called with each comment position and text
// (including opening /* or // and closing */, but without a newline
// at the end of line comments). Comment text always starts with a /
// which can be used to distinguish these handler calls from errors.
//
// If the scanner mode includes the directives (but not the comments)
// flag, only comments containing a //line, /*line, or //go: directive
// are reported, in the same way as regular comments.
func (s *Scanner) Next() {
	nlsemi := s.nlsemi
	s.nlsemi = false

redo:
	// skip white space
	s.stop()
	startLine, startCol := s.pos()
	for s.ch == ' ' || s.ch == '\t' || s.ch == '\n' && !nlsemi || s.ch == '\r' {
		s.nextch()
	}

	// token start
	s.Line, s.Col = s.pos()
	s.Blank = s.line > startLine || startCol == Colbase
	s.start()
	if IsLetter(s.ch) || s.ch >= utf8.RuneSelf && s.AtIdentChar(true) {
		s.nextch()
		s.Ident()
		return
	}

	switch s.ch {
	case -1:
		if nlsemi {
			s.Lit = "EOF"
			s.Tok = Semi
			break
		}
		s.Tok =EOF

	case '\n':
		s.nextch()
		s.Lit = "newline"
		s.Tok = Semi

	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		s.Number(false)

	case '"':
		s.stdString()

	case '`':
		s.rawString()

	case '\'':
		s.rune()

	case '(':
		s.nextch()
		s.Tok = Lparen

	case '[':
		s.nextch()
		s.Tok = Lbrack

	case '{':
		s.nextch()
		s.Tok = Lbrace

	case ',':
		s.nextch()
		s.Tok = Comma

	case ';':
		s.nextch()
		s.Lit = "semicolon"
		s.Tok = Semi

	case ')':
		s.nextch()
		s.nlsemi = true
		s.Tok = Rparen

	case ']':
		s.nextch()
		s.nlsemi = true
		s.Tok = Rbrack

	case '}':
		s.nextch()
		s.nlsemi = true
		s.Tok = Rbrace

	case ':':
		s.nextch()
		if s.ch == '=' {
			s.nextch()
			s.Tok = Define
			break
		}
		s.Tok = Colon

	case '.':
		s.nextch()
		if IsDecimal(s.ch) {
			s.Number(true)
			break
		}
		if s.ch == '.' {
			s.nextch()
			if s.ch == '.' {
				s.nextch()
				s.Tok = DotDotDot
				break
			}
			s.rewind() // now s.ch holds 1st '.'
			s.nextch() // consume 1st '.' again
		}
		s.Tok = Dot

	case '+':
		s.nextch()
		s.Op, s.Prec = Add, PrecAdd
		if s.ch != '+' {
			goto assignop
		}
		s.nextch()
		s.nlsemi = true
		s.Tok = IncOp

	case '-':
		s.nextch()
		s.Op, s.Prec = Sub, PrecAdd
		if s.ch != '-' {
			goto assignop
		}
		s.nextch()
		s.nlsemi = true
		s.Tok = IncOp

	case '*':
		s.nextch()
		s.Op, s.Prec = Mul, PrecMul
		// don't goto assignop - want _Star token
		if s.ch == '=' {
			s.nextch()
			s.Tok = AssignOp
			break
		}
		s.Tok = Star

	case '/':
		s.nextch()
		if s.ch == '/' {
			s.nextch()
			s.lineComment()
			goto redo
		}
		if s.ch == '*' {
			s.nextch()
			s.fullComment()
			if line, _ := s.pos(); line > s.Line && nlsemi {
				// A multi-line comment acts like a newline;
				// it translates to a ';' if nlsemi is set.
				s.Lit = "newline"
				s.Tok = Semi
				break
			}
			goto redo
		}
		s.Op, s.Prec = Div, PrecMul
		goto assignop

	case '%':
		s.nextch()
		s.Op, s.Prec = Rem, PrecMul
		goto assignop

	case '&':
		s.nextch()
		if s.ch == '&' {
			s.nextch()
			s.Op, s.Prec = AndAnd, PrecAndAnd
			s.Tok = OperatorType
			break
		}
		s.Op, s.Prec = And, PrecMul
		if s.ch == '^' {
			s.nextch()
			s.Op = AndNot
		}
		goto assignop

	case '|':
		s.nextch()
		if s.ch == '|' {
			s.nextch()
			s.Op, s.Prec = OrOr, PrecOrOr
			s.Tok = OperatorType
			break
		}
		s.Op, s.Prec = Or, PrecAdd
		goto assignop

	case '^':
		s.nextch()
		s.Op, s.Prec = Xor, PrecAdd
		goto assignop

	case '<':
		s.nextch()
		if s.ch == '=' {
			s.nextch()
			s.Op, s.Prec = Leq, PrecCmp
			s.Tok = OperatorType
			break
		}
		if s.ch == '<' {
			s.nextch()
			s.Op, s.Prec = Shl, PrecMul
			goto assignop
		}
		if s.ch == '-' {
			s.nextch()
			s.Tok = Arrow
			break
		}
		s.Op, s.Prec = Lss, PrecCmp
		s.Tok = OperatorType

	case '>':
		s.nextch()
		if s.ch == '=' {
			s.nextch()
			s.Op, s.Prec = Geq, PrecCmp
			s.Tok =OperatorType
			break
		}
		if s.ch == '>' {
			s.nextch()
			s.Op, s.Prec = Shr, PrecMul
			goto assignop
		}
		s.Op, s.Prec = Gtr, PrecCmp
		s.Tok = OperatorType

	case '=':
		s.nextch()
		if s.ch == '=' {
			s.nextch()
			s.Op, s.Prec = Eql, PrecCmp
			s.Tok = OperatorType
			break
		}
		s.Tok = Assign

	case '!':
		s.nextch()
		if s.ch == '=' {
			s.nextch()
			s.Op, s.Prec = Neq, PrecCmp
			s.Tok = OperatorType
			break
		}
		s.Op, s.Prec = Not, 0
		s.Tok = OperatorType

	case '~':
		s.nextch()
		s.Op, s.Prec = Tilde, 0
		s.Tok = OperatorType

	default:
		s.Errorf("invalid character %#U", s.ch)
		s.nextch()
		goto redo
	}

	return

assignop:
	if s.ch == '=' {
		s.nextch()
		s.Tok = AssignOp
		return
	}
	s.Tok = OperatorType
}

func (s *Scanner) Ident() {
	// accelerate common case (7bit ASCII)
	for IsLetter(s.ch) || IsDecimal(s.ch) {
		s.nextch()
	}

	// general case
	if s.ch >= utf8.RuneSelf {
		for s.AtIdentChar(false) {
			s.nextch()
		}
	}

	// possibly a keyword
	lit := s.segment()
	if len(lit) >= 2 {
		if tok := keywordMap[Hash(lit)]; tok != 0 && TokStrFast(tok) == string(lit) {
			s.nlsemi = contains(1<<Break|1<<Continue|1<<Fallthrough|1<<Return, tok)
			s.Tok = tok
			return
		}
	}

	s.nlsemi = true
	s.Lit = string(lit)
	s.Tok = NameType
}

// tokStrFast is a faster version of token.String, which assumes that tok
// is one of the valid tokens - and can thus skip bounds checks.
func TokStrFast(tok Token) string {
	return token_name[token_index[tok-1]:token_index[tok]]
}

func (s *Scanner) AtIdentChar(first bool) bool {
	switch {
	case unicode.IsLetter(s.ch) || s.ch == '_':
		// ok
	case unicode.IsDigit(s.ch):
		if first {
			s.Errorf("identifier cannot begin with digit %#U", s.ch)
		}
	case s.ch >= utf8.RuneSelf:
		s.Errorf("invalid character %#U in identifier", s.ch)
	default:
		return false
	}
	return true
}

// hash is a perfect hash function for keywords.
// It assumes that s has at least length 2.
func Hash(s []byte) uint {
	return (uint(s[0])<<4 ^ uint(s[1]) + uint(len(s))) & uint(len(keywordMap)-1)
}

var keywordMap [1 << 6]Token // size must be power of two

func init() { Init() }

func Init() {
	// populate keywordMap
	for tok := Break; tok <= Var; tok++ {
		h := Hash([]byte(tok.String()))
		if keywordMap[h] != 0 {
			panic("imperfect hash")
		}
		keywordMap[h] = tok
	}
}

func Lower(ch rune) rune     { return ('a' - 'A') | ch } // returns lower-case ch iff ch is ASCII letter
func IsLetter(ch rune) bool  { return 'a' <= Lower(ch) && Lower(ch) <= 'z' || ch == '_' }
func IsDecimal(ch rune) bool { return '0' <= ch && ch <= '9' }
func IsHex(ch rune) bool     { return '0' <= ch && ch <= '9' || 'a' <= Lower(ch) && Lower(ch) <= 'f' }

// Digits accepts the sequence { digit | '_' }.
// If base <= 10, Digits accepts any decimal digit but records
// the index (relative to the literal start) of a digit >= base
// in *invalid, if *invalid < 0.
// Digits returns a bitset describing whether the sequence contained
// Digits (bit 0 is set), or separators '_' (bit 1 is set).
func (s *Scanner) Digits(base int32, invalid *int32) (digsep int32) {
	if base <= 10 {
		max := rune('0' + base)
		for IsDecimal(s.ch) || s.ch == '_' {
			ds := int32(1)
			if s.ch == '_' {
				ds = 2
			} else if s.ch >= max && *invalid < 0 {
				_, col := s.pos()
				*invalid = int32(col - s.col)
			}
			digsep |= ds
			s.nextch()
		}
	} else {
		for IsHex(s.ch) || s.ch == '_' {
			ds := int32(1)
			if s.ch == '_' {
				ds = 2
			}
			digsep |= ds
			s.nextch()
		}
	}
	return
}

func (s *Scanner) Number(seenPoint bool) {
	ok := true
	kind := IntLit
	base := int32(10)  // number base
	prefix := rune(0)  // one of 0 (decimal), '0' (0-octal), 'x', 'o', or 'b'
	digsep := int32(0) // bit 0: digit present, bit 1: '_' present
	invalid := int32(-1) // index of invalid digit in literal, or < 0

	// integer part
	if !seenPoint {
		if s.ch == '0' {
			s.nextch()
			switch Lower(s.ch) {
			case 'x':
				s.nextch()
				base, prefix = 16, 'x'
			case 'o':
				s.nextch()
				base, prefix = 8, 'o'
			case 'b':
				s.nextch()
				base, prefix = 2, 'b'
			default:
				base, prefix = 8, '0'
				digsep = 1 // leading 0
			}
		}
		digsep |= s.Digits(base, &invalid)
		if s.ch == '.' {
			if prefix == 'o' || prefix == 'b' {
				s.Errorf("invalid radix point in %s literal", baseName(base))
				ok = false
			}
			s.nextch()
			seenPoint = true
		}
	}

	// fractional part
	if seenPoint {
		kind = FloatLit
		digsep |= s.Digits(base, &invalid)
	}

	if digsep&1 == 0 && ok {
		s.Errorf("%s literal has no digits", baseName(base))
		ok = false
	}

	// exponent
	if e := Lower(s.ch); e == 'e' || e == 'p' {
		if ok {
			switch {
			case e == 'e' && prefix != 0 && prefix != '0':
				s.Errorf("%q exponent requires decimal mantissa", s.ch)
				ok = false
			case e == 'p' && prefix != 'x':
				s.Errorf("%q exponent requires hexadecimal mantissa", s.ch)
				ok = false
			}
		}
		s.nextch()
		kind = FloatLit
		if s.ch == '+' || s.ch == '-' {
			s.nextch()
		}
		digsep = s.Digits(10, nil) | digsep&2 // don't lose sep bit
		if digsep&1 == 0 && ok {
			s.Errorf("exponent has no digits")
			ok = false
		}
	} else if prefix == 'x' && kind == FloatLit && ok {
		s.Errorf("hexadecimal mantissa requires a 'p' exponent")
		ok = false
	}

	// suffix 'i'
	if s.ch == 'i' {
		kind = ImagLit
		s.nextch()
	}

	s.SetLit(kind, ok) // do this now so we can use s.lit below

	if kind == IntLit && invalid >= 0 && ok {
		s.ErrorAtf(invalid, "invalid digit %q in %s literal", s.Lit[invalid], baseName(base))
		ok = false
	}

	if digsep&2 != 0 && ok {
		if i := invalidSep(s.Lit); i >= 0 {
			s.ErrorAtf(i, "'_' must separate successive digits")
			ok = false
		}
	}

	s.Bad = !ok // correct s.bad
}

func baseName(base int32) string {
	switch base {
	case 2:
		return "binary"
	case 8:
		return "octal"
	case 10:
		return "decimal"
	case 16:
		return "hexadecimal"
	}
	panic("invalid base")
}

// invalidSep returns the index of the first invalid separator in x, or -1.
func invalidSep(x string) int32 {
	x1 := ' ' // prefix char, we only care if it's 'x'
	d := '.'  // digit, one of '_', '0' (a digit), or '.' (anything else)
	i := int32(0)

	// a prefix counts as a digit
	if len(x) >= 2 && x[0] == '0' {
		x1 = Lower(rune(x[1]))
		if x1 == 'x' || x1 == 'o' || x1 == 'b' {
			d = '0'
			i = 2
		}
	}

	// mantissa and exponent
	for ; i < int32(len(x)); i++ {
		p := d // previous digit
		d = rune(x[i])
		switch {
		case d == '_':
			if p != '0' {
				return i
			}
		case IsDecimal(d) || x1 == 'x' && IsHex(d):
			d = '0'
		default:
			if p == '_' {
				return i - 1
			}
			d = '.'
		}
	}
	if d == '_' {
		return int32(len(x)) - 1
	}

	return -1
}

func (s *Scanner) rune() {
	ok := true
	s.nextch()

	n := 0
	for ; ; n++ {
		if s.ch == '\'' {
			if ok {
				if n == 0 {
					s.Errorf("empty rune literal or unescaped '")
					ok = false
				} else if n != 1 {
					s.ErrorAtf(0, "more than one character in rune literal")
					ok = false
				}
			}
			s.nextch()
			break
		}
		if s.ch == '\\' {
			s.nextch()
			if !s.escape('\'') {
				ok = false
			}
			continue
		}
		if s.ch == '\n' {
			if ok {
				s.Errorf("newline in rune literal")
				ok = false
			}
			break
		}
		if s.ch < 0 {
			if ok {
				s.ErrorAtf(0, "rune literal not terminated")
				ok = false
			}
			break
		}
		s.nextch()
	}

	s.SetLit(RuneLit, ok)
}

func (s *Scanner) stdString() {
	ok := true
	s.nextch()

	for {
		if s.ch == '"' {
			s.nextch()
			break
		}
		if s.ch == '\\' {
			s.nextch()
			if !s.escape('"') {
				ok = false
			}
			continue
		}
		if s.ch == '\n' {
			s.Errorf("newline in string")
			ok = false
			break
		}
		if s.ch < 0 {
			s.ErrorAtf(0, "string not terminated")
			ok = false
			break
		}
		s.nextch()
	}

	s.SetLit(StringLit, ok)
}

func (s *Scanner) rawString() {
	ok := true
	s.nextch()

	for {
		if s.ch == '`' {
			s.nextch()
			break
		}
		if s.ch < 0 {
			s.ErrorAtf(0, "string not terminated")
			ok = false
			break
		}
		s.nextch()
	}
	// We leave CRs in the string since they are part of the
	// literal (even though they are not part of the literal
	// value).

	s.SetLit(StringLit, ok)
}

func (s *Scanner) comment(text string) {
	s.ErrorAtf(0, "%s", text)
}

func (s *Scanner) skipLine() {
	// don't consume '\n' - needed for nlsemi logic
	for s.ch >= 0 && s.ch != '\n' {
		s.nextch()
	}
}

func (s *Scanner) lineComment() {
	// opening has already been consumed

	if s.mode&comments != 0 {
		s.skipLine()
		s.comment(string(s.segment()))
		return
	}

	// are we saving directives? or is this definitely not a directive?
	if s.mode&directives == 0 || (s.ch != 'g' && s.ch != 'l') {
		s.stop()
		s.skipLine()
		return
	}

	// recognize go: or line directives
	prefix := "go:"
	if s.ch == 'l' {
		prefix = "line "
	}

	for _, r := range prefix {
		if s.ch != r { s.stop(); s.skipLine(); return }
		s.nextch()
	}
	// directive text
	s.skipLine()
	s.comment(string(s.segment()))
}

func (s *Scanner) skipComment() bool {
	for s.ch >= 0 {
		for s.ch == '*' {
			s.nextch()
			if s.ch == '/' {
				s.nextch()
				return true
			}
		}
		s.nextch()
	}
	s.ErrorAtf(0, "comment not terminated")
	return false
}

func (s *Scanner) fullComment() {
	/* opening has already been consumed */

	if s.mode&comments != 0 {
		if s.skipComment() {
			s.comment(string(s.segment()))
		}
		return
	}

	if s.mode&directives == 0 || s.ch != 'l' {
		s.stop()
		s.skipComment()
		return
	}

	// recognize line directive
	const prefix = "line "

	for _, r := range prefix {
		if s.ch != r { s.stop(); s.skipComment(); return }
		s.nextch()
	}
	// directive text
	if s.skipComment() {
		s.comment(string(s.segment()))
	}
}

func (s *Scanner) escape(quote rune) bool {
	var n int32
	var base, max uint32

	switch s.ch {
	case quote, 'a', 'b', 'f', 'n', 'r', 't', 'v', '\\':
		s.nextch()
		return true
	case '0', '1', '2', '3', '4', '5', '6', '7':
		n, base, max = 3, 8, 255
	case 'x':
		s.nextch()
		n, base, max = 2, 16, 255
	case 'u':
		s.nextch()
		n, base, max = 4, 16, unicode.MaxRune
	case 'U':
		s.nextch()
		n, base, max = 8, 16, unicode.MaxRune
	default:
		if s.ch < 0 {
			return true // complain in caller about EOF
		}
		s.Errorf("unknown escape")
		return false
	}

	var x uint32
	for i := n; i > 0; i-- {
		if s.ch < 0 {
			return true // complain in caller about EOF
		}
		d := base
		if IsDecimal(s.ch) {
			d = uint32(s.ch) - '0'
		} else if 'a' <= Lower(s.ch) && Lower(s.ch) <= 'f' {
			d = uint32(Lower(s.ch)) - 'a' + 10
		}
		if d >= base {
			s.Errorf("invalid character %q in %s escape", s.ch, baseName(int32(base)))
			return false
		}
		// d < base
		x = x*base + d
		s.nextch()
	}

	if x > max && base == 8 {
		s.Errorf("octal escape value %d > 255", x)
		return false
	}

	if x > max || 0xD800 <= x && x < 0xE000 /* surrogate range */ {
		s.Errorf("escape is invalid Unicode code point %#U", x)
		return false
	}

	return true
}
