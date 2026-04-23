// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package syntax

type Token uint32

//go:generate stringer -type token -linecomment tokens.go

const (
	_    Token = iota
	EOF       // EOF

	// names and literals
	NameType    // name
	Literal // literal

	// operators and operations
	// Operator is excluding '*' (Star)
	OperatorType // op
	AssignOp // op=
	IncOp    // opop
	Assign   // =
	Define   // :=
	Arrow    // <-
	Star     // *

	// delimiters
	Lparen    // (
	Lbrack    // [
	Lbrace    // {
	Rparen    // )
	Rbrack    // ]
	Rbrace    // }
	Comma     // ,
	Semi      // ;
	Colon     // :
	Dot       // .
	DotDotDot // ...

	// keywords
	Break       // break
	Case        // case
	Chan        // chan
	Const       // const
	Continue    // continue
	Default     // default
	Defer       // defer
	Else        // else
	Fallthrough // fallthrough
	For         // for
	Func        // func
	Go          // go
	Goto        // goto
	If          // if
	Import      // import
	Interface   // interface
	Map         // map
	Package     // package
	Range       // range
	Return      // return
	Select      // select
	Struct      // struct
	Switch      // switch
	TypeType        // type
	Var         // var

    // empty line comment to exclude it from .String
	tokenCount //
)

// Make sure we have at most 64 tokens so we can use them in a set.
const _ uint64 = 1 << (tokenCount - 1)

// contains reports whether tok is in tokset.
func contains(tokset uint64, tok Token) bool {
	return tokset&(1<<tok) != 0
}

type LitKind uint8

// TODO(gri) With the 'i' (imaginary) suffix now permitted on integer
// and floating-point numbers, having a single ImagLit does
// not represent the literal kind well anymore. Remove it?
const (
	IntLit LitKind = iota
	FloatLit
	ImagLit
	RuneLit
	StringLit
)

type Operator uint32

const (
	_ Operator = iota

	// Def is the : in :=
	Def   // :
	Not   // !
	Recv  // <-
	Tilde // ~

	// precOrOr
	OrOr // ||

	// precAndAnd
	AndAnd // &&

	// precCmp
	Eql // ==
	Neq // !=
	Lss // <
	Leq // <=
	Gtr // >
	Geq // >=

	// precAdd
	Add // +
	Sub // -
	Or  // |
	Xor // ^

	// precMul
	Mul    // *
	Div    // /
	Rem    // %
	And    // &
	AndNot // &^
	Shl    // <<
	Shr    // >>
)

// OperatorType precedences
const (
	_ = iota
	PrecOrOr
	PrecAndAnd
	PrecCmp
	PrecAdd
	PrecMul
)
