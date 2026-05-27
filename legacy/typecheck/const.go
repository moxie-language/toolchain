package typecheck

import (
	"go/constant"
	"go/token"
	"math/big"
	"moxie/syntax"
	"strconv"
	"strings"
)

// checkConstGroup processes a sequence of ConstDecls that share a Group.
// iota advances per ConstDecl position. Inherited type/value expressions
// carry forward when a ConstDecl has no explicit values (standard Go rule).
func (c *Checker) checkConstGroup(decls []*syntax.ConstDecl) {
	var prevType syntax.Expr
	var prevValues syntax.Expr
	for i, d := range decls {
		typ := d.Type
		vals := d.Values
		if vals == nil {
			// inherit from previous spec
			typ = prevType
			vals = prevValues
		} else {
			prevType = typ
			prevValues = vals
		}
		c.checkConstDeclAt(d, typ, vals, int64(i))
	}
}

func (c *Checker) checkConstDeclAt(d *syntax.ConstDecl, typExpr syntax.Expr, valExpr syntax.Expr, iotaVal int64) {
	var typ Type
	if typExpr != nil {
		typ = c.resolveTypeExpr(typExpr)
	}

	var vals []constant.Value
	if valExpr != nil {
		vals = c.evalConstExprList(valExpr, iotaVal)
	}

	for i, name := range d.NameList {
		if name.Value == "_" {
			continue
		}
		obj, ok := c.pkg.scope.Lookup(name.Value).(*Const)
		if !ok {
			continue
		}
		var cv constant.Value
		if i < len(vals) {
			cv = vals[i]
		}
		obj.val = cv
		if typ != nil {
			obj.typ = typ
		} else if cv != nil {
			obj.typ = untypedTypeOf(cv)
		}
		if c.info != nil {
			c.info.Defs[name] = obj
		}
	}
}

// evalConstExprList evaluates a (possibly multi-value) const expression.
func (c *Checker) evalConstExprList(e syntax.Expr, iotaVal int64) []constant.Value {
	if l, ok := e.(*syntax.ListExpr); ok {
		var out []constant.Value
		for _, el := range l.ElemList {
			v := c.evalConst(el, iotaVal)
			out = append(out, v)
		}
		return out
	}
	v := c.evalConst(e, iotaVal)
	return []constant.Value{v}
}

// evalConst evaluates a constant expression, returning a constant.Value.
// Returns nil if the expression is not a constant.
func (c *Checker) evalConst(e syntax.Expr, iotaVal int64) constant.Value {
	if e == nil {
		return nil
	}
	switch e := e.(type) {
	case *syntax.BasicLit:
		return evalBasicLit(e)
	case *syntax.Name:
		if e.Value == "iota" {
			return constant.MakeInt64(iotaVal)
		}
		_, obj := c.lookup(e.Value, c.pkg.scope)
		if obj == nil {
			return nil
		}
		if k, ok := obj.(*Const); ok {
			if cv, ok := k.val.(constant.Value); ok {
				return cv
			}
		}
		return nil
	case *syntax.Operation:
		return c.evalConstOp(e, iotaVal)
	case *syntax.ParenExpr:
		return c.evalConst(e.X, iotaVal)
	case *syntax.CallExpr:
		// len("string literal") is a constant in Go.
		if id, ok := e.Fun.(*syntax.Name); ok && id.Value == "len" && len(e.ArgList) == 1 {
			if lit, ok := e.ArgList[0].(*syntax.BasicLit); ok && lit.Kind == syntax.StringLit {
				s := constant.StringVal(evalBasicLit(lit))
				return constant.MakeInt64(int64(len(s)))
			}
		}
		// unsafe.Sizeof/Alignof/Offsetof — return a placeholder int constant.
		if sel, ok := e.Fun.(*syntax.SelectorExpr); ok {
			if pkg, ok := sel.X.(*syntax.Name); ok && pkg.Value == "unsafe" {
				switch sel.Sel.Value {
				case "Sizeof", "Alignof", "Offsetof":
					return constant.MakeInt64(8) // conservative placeholder (pointer size)
				}
			}
		}
		// Type conversion of constant: T(x).
		if len(e.ArgList) != 1 {
			return nil
		}
		inner := c.evalConst(e.ArgList[0], iotaVal)
		if inner == nil {
			return nil
		}
		targetType := c.resolveTypeExpr(e.Fun)
		if targetType == nil {
			return nil
		}
		return convertConst(inner, targetType)
	}
	return nil
}

func evalBasicLit(e *syntax.BasicLit) constant.Value {
	switch e.Kind {
	case syntax.IntLit:
		return constant.MakeFromLiteral(e.Value, token.INT, 0)
	case syntax.FloatLit:
		return constant.MakeFromLiteral(e.Value, token.FLOAT, 0)
	case syntax.StringLit:
		return constant.MakeFromLiteral(e.Value, token.STRING, 0)
	case syntax.RuneLit:
		return constant.MakeFromLiteral(e.Value, token.CHAR, 0)
	}
	return nil
}

func (c *Checker) evalConstOp(e *syntax.Operation, iotaVal int64) constant.Value {
	if e.Y == nil {
		// unary
		x := c.evalConst(e.X, iotaVal)
		if x == nil {
			return nil
		}
		op := syntaxOpToToken(e.Op)
		if op == token.ILLEGAL {
			return nil
		}
		return constant.UnaryOp(op, x, 0)
	}
	// binary
	x := c.evalConst(e.X, iotaVal)
	y := c.evalConst(e.Y, iotaVal)
	if x == nil || y == nil {
		return nil
	}
	op := syntaxOpToToken(e.Op)
	if op == token.ILLEGAL {
		return nil
	}
	if op == token.SHL || op == token.SHR {
		shift, ok := constant.Uint64Val(y)
		if !ok {
			return nil
		}
		return constant.Shift(x, op, uint(shift))
	}
	// Moxie: | on string constants is concat. The syntax AST has Or (|) because
	// rewriteConstPipes only runs on the go/ast side, not on syntax.File.
	if op == token.OR && x.Kind() == constant.String && y.Kind() == constant.String {
		xs := constant.StringVal(x)
		ys := constant.StringVal(y)
		return constant.MakeString(xs + ys)
	}
	// Comparison operators must use constant.Compare, not BinaryOp.
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return constant.MakeBool(constant.Compare(x, op, y))
	}
	return constant.BinaryOp(x, op, y)
}

func syntaxOpToToken(op syntax.Operator) token.Token {
	switch op {
	case syntax.Add:
		return token.ADD
	case syntax.Sub:
		return token.SUB
	case syntax.Mul:
		return token.MUL
	case syntax.Div:
		return token.QUO
	case syntax.Rem:
		return token.REM
	case syntax.And:
		return token.AND
	case syntax.Or:
		return token.OR
	case syntax.Xor:
		return token.XOR
	case syntax.Shl:
		return token.SHL
	case syntax.Shr:
		return token.SHR
	case syntax.AndNot:
		return token.AND_NOT
	case syntax.Not:
		return token.NOT
	case syntax.Eql:
		return token.EQL
	case syntax.Neq:
		return token.NEQ
	case syntax.Lss:
		return token.LSS
	case syntax.Leq:
		return token.LEQ
	case syntax.Gtr:
		return token.GTR
	case syntax.Geq:
		return token.GEQ
	}
	return token.ILLEGAL
}

// convertConst converts a constant value to the representation expected by
// a given target type. Handles explicit type conversions in const exprs.
func convertConst(v constant.Value, target Type) constant.Value {
	b, ok := target.Underlying().(*Basic)
	if !ok {
		return v
	}
	switch {
	case b.info&IsInteger != 0:
		i, _ := constant.Int64Val(v)
		return constant.MakeInt64(i)
	case b.info&IsFloat != 0:
		f, _ := constant.Float64Val(v)
		return constant.MakeFloat64(f)
	case b.info&IsString != 0:
		if v.Kind() == constant.Int {
			n, _ := constant.Uint64Val(v)
			r := rune(n)
			return constant.MakeString(string(r))
		}
		return v
	}
	return v
}

// untypedTypeOf returns the untyped type for a constant value.
func untypedTypeOf(v constant.Value) Type {
	switch v.Kind() {
	case constant.Bool:
		return Typ[UntypedBool]
	case constant.Int:
		return Typ[UntypedInt]
	case constant.Float:
		return Typ[UntypedFloat]
	case constant.String:
		return Typ[UntypedString]
	}
	return nil
}

// constInt64 extracts the int64 value of a constant, used for array lengths etc.
// Returns 0 and false if not a constant integer.
func constInt64(v constant.Value) (int64, bool) {
	if v == nil || v.Kind() != constant.Int {
		return 0, false
	}
	n, exact := constant.Int64Val(v)
	return n, exact
}

// IsConstExpr reports whether e is a constant expression that can be evaluated
// at compile time (used to gate array length resolution).
func (c *Checker) IsConstExpr(e syntax.Expr) bool {
	return c.evalConst(e, 0) != nil
}

// evalArrayLen evaluates a constant array length expression.
func (c *Checker) evalArrayLen(e syntax.Expr) int64 {
	v := c.evalConst(e, 0)
	if v == nil {
		return 0
	}
	n, _ := constInt64(v)
	return n
}

// interpString converts a Go string literal value to a Go string.
// Handles quoted forms: "...", `...`, '...'.
func interpString(s string) string {
	if len(s) < 2 {
		return s
	}
	if s[0] == '`' {
		return s[1 : len(s)-1]
	}
	unquoted, err := strconv.Unquote(s)
	if err != nil {
		return strings.Trim(s, `"`)
	}
	return unquoted
}

// bigIntVal extracts a *big.Int from a constant for use in overflow checks.
func bigIntVal(v constant.Value) *big.Int {
	if v.Kind() != constant.Int {
		return nil
	}
	b := new(big.Int)
	s := v.ExactString()
	b.SetString(s, 10)
	return b
}
