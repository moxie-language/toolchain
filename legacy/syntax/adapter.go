package syntax

import (
	"bytes"
	"go/ast"
	"go/token"
)

type commentEntry struct {
	pos  Pos
	text string
}

type pragmaData struct {
	comments []commentEntry
}

func pragmaHandler(pos Pos, blank bool, text string, current Pragma) Pragma {
	p, _ := current.(*pragmaData)
	if p == nil {
		p = &pragmaData{}
	}
	prefix := "//"
	p.comments = append(p.comments, commentEntry{pos: pos, text: prefix + text})
	return p
}

func (a *Adapter) pragmaToDoc(p Pragma) *ast.CommentGroup {
	pd, _ := p.(*pragmaData)
	if pd == nil || len(pd.comments) == 0 {
		return nil
	}
	cg := &ast.CommentGroup{}
	for _, c := range pd.comments {
		cg.List = append(cg.List, &ast.Comment{
			Slash: a.pos(c.pos),
			Text:  c.text,
		})
	}
	return cg
}

type Adapter struct {
	fset *token.FileSet
	file *token.File
}

// Convert parses src using the bootstrap parser and returns a go/ast.File
// registered in fset. filename is used for position info.
func Convert(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
	af, _, err := ConvertAndKeep(fset, filename, src)
	return af, err
}

// ConvertAndKeep is like Convert but also returns the parsed *syntax.File.
// The syntax.File is used by the native type checker (B1) to annotate
// expressions with type info without going through go/ast.
func ConvertAndKeep(fset *token.FileSet, filename string, src []byte) (*ast.File, *File, error) {
	base := NewFileBase(filename)
	sf, err := Parse(base, bytes.NewReader(src), nil, pragmaHandler, 0)
	if err != nil {
		return nil, nil, err
	}

	a := &Adapter{fset: fset}
	a.file = fset.AddFile(filename, -1, len(src))

	var lines []int
	lines = append(lines, 0)
	for i, b := range src {
		if b == '\n' && i+1 < len(src) {
			lines = append(lines, i+1)
		}
	}
	a.file.SetLines(lines)

	af := a.convertFile(sf)
	return af, sf, nil
}

func (a *Adapter) pos(p Pos) token.Pos {
	// syntax Pos stores 0-based line/col from scanner snapshot
	line := int(p.Line()) + 1
	col := int(p.Col()) + 1
	if line < 1 {
		line = 1
	}
	if col < 1 {
		col = 1
	}
	if line > a.file.LineCount() {
		line = a.file.LineCount()
	}
	result := a.file.LineStart(line) + token.Pos(col-1)
	return result
}

func (a *Adapter) ident(n *Name) *ast.Ident {
	if n == nil {
		return nil
	}
	return &ast.Ident{NamePos: a.pos(n.Pos()), Name: n.Value}
}

func (a *Adapter) convertFile(f *File) *ast.File {
	af := &ast.File{
		Package:   a.pos(f.Pos()),
		Name:      a.ident(f.PkgName),
		FileStart: token.Pos(a.file.Base()),
		FileEnd:   token.Pos(a.file.Base() + a.file.Size()),
	}

	var imports []*ast.ImportSpec
	groupMap := map[*Group]*ast.GenDecl{}

	for _, d := range f.DeclList {
		g := declGroup(d)
		if g != nil {
			if gd, ok := groupMap[g]; ok {
				// Add spec to existing group
				gd.Specs = append(gd.Specs, a.spec(d))
				continue
			}
			// Start new group
			gd := &ast.GenDecl{
				TokPos: a.pos(d.Pos()),
				Tok:    declToken(d),
				Lparen: a.pos(d.Pos()),
				Specs:  []ast.Spec{a.spec(d)},
			}
			groupMap[g] = gd
			af.Decls = append(af.Decls, gd)
			continue
		}

		ad := a.decl(d)
		if ad != nil {
			af.Decls = append(af.Decls, ad)
		}
	}

	for _, d := range af.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			if d.Doc != nil {
				af.Comments = append(af.Comments, d.Doc)
			}
			if d.Tok == token.IMPORT {
				for _, s := range d.Specs {
					if is, ok := s.(*ast.ImportSpec); ok {
						imports = append(imports, is)
					}
				}
			}
		case *ast.FuncDecl:
			if d.Doc != nil {
				af.Comments = append(af.Comments, d.Doc)
			}
		}
	}
	af.Imports = imports
	return af
}

func declGroup(d Decl) *Group {
	switch d := d.(type) {
	case *ImportDecl:
		return d.Group
	case *ConstDecl:
		return d.Group
	case *TypeDecl:
		return d.Group
	case *VarDecl:
		return d.Group
	}
	return nil
}

func declToken(d Decl) token.Token {
	switch d.(type) {
	case *ImportDecl:
		return token.IMPORT
	case *ConstDecl:
		return token.CONST
	case *TypeDecl:
		return token.TYPE
	case *VarDecl:
		return token.VAR
	}
	return token.ILLEGAL
}

func (a *Adapter) spec(d Decl) ast.Spec {
	switch d := d.(type) {
	case *ImportDecl:
		spec := &ast.ImportSpec{Path: a.basicLit(d.Path)}
		if d.LocalPkgName != nil {
			spec.Name = a.ident(d.LocalPkgName)
		}
		return spec
	case *ConstDecl:
		return &ast.ValueSpec{
			Names:  a.nameList(d.NameList),
			Type:   a.expr(d.Type),
			Values: a.exprList(d.Values),
		}
	case *TypeDecl:
		ts := &ast.TypeSpec{
			Name: a.ident(d.Name),
			Type: a.expr(d.Type),
		}
		if d.Alias {
			ts.Assign = a.pos(d.Pos())
		}
		if len(d.TParamList) > 0 {
			ts.TypeParams = a.fieldList(d.TParamList)
		}
		return ts
	case *VarDecl:
		return &ast.ValueSpec{
			Names:  a.nameList(d.NameList),
			Type:   a.expr(d.Type),
			Values: a.exprList(d.Values),
		}
	}
	return nil
}

func (a *Adapter) decl(d Decl) ast.Decl {
	switch d := d.(type) {
	case *ImportDecl:
		spec := &ast.ImportSpec{
			Path: a.basicLit(d.Path),
		}
		if d.LocalPkgName != nil {
			spec.Name = a.ident(d.LocalPkgName)
		}
		return &ast.GenDecl{
			Doc:    a.pragmaToDoc(d.Pragma),
			TokPos: a.pos(d.Pos()),
			Tok:    token.IMPORT,
			Specs:  []ast.Spec{spec},
		}
	case *ConstDecl:
		vs := &ast.ValueSpec{
			Names:  a.nameList(d.NameList),
			Type:   a.expr(d.Type),
			Values: a.exprList(d.Values),
		}
		return &ast.GenDecl{
			Doc:    a.pragmaToDoc(d.Pragma),
			TokPos: a.pos(d.Pos()),
			Tok:    token.CONST,
			Specs:  []ast.Spec{vs},
		}
	case *TypeDecl:
		ts := &ast.TypeSpec{
			Name:   a.ident(d.Name),
			Assign: 0,
			Type:   a.expr(d.Type),
		}
		if d.Alias {
			ts.Assign = a.pos(d.Pos())
		}
		if len(d.TParamList) > 0 {
			ts.TypeParams = a.fieldList(d.TParamList)
		}
		return &ast.GenDecl{
			Doc:    a.pragmaToDoc(d.Pragma),
			TokPos: a.pos(d.Pos()),
			Tok:    token.TYPE,
			Specs:  []ast.Spec{ts},
		}
	case *VarDecl:
		vs := &ast.ValueSpec{
			Names:  a.nameList(d.NameList),
			Type:   a.expr(d.Type),
			Values: a.exprList(d.Values),
		}
		return &ast.GenDecl{
			Doc:    a.pragmaToDoc(d.Pragma),
			TokPos: a.pos(d.Pos()),
			Tok:    token.VAR,
			Specs:  []ast.Spec{vs},
		}
	case *FuncDecl:
		fd := &ast.FuncDecl{
			Doc:  a.pragmaToDoc(d.Pragma),
			Name: a.ident(d.Name),
			Type: a.funcType(d.Type),
			Body: a.blockStmt(d.Body),
		}
		if d.Recv != nil {
			fd.Recv = &ast.FieldList{
				List: []*ast.Field{a.field(d.Recv)},
			}
		}
		if len(d.TParamList) > 0 {
			fd.Type.TypeParams = a.fieldList(d.TParamList)
		}
		return fd
	}
	return nil
}

func (a *Adapter) nameList(names []*Name) []*ast.Ident {
	var out []*ast.Ident
	for _, n := range names {
		out = append(out, a.ident(n))
	}
	return out
}

func (a *Adapter) exprList(e Expr) []ast.Expr {
	if e == nil {
		return nil
	}
	if l, ok := e.(*ListExpr); ok {
		var out []ast.Expr
		for _, el := range l.ElemList {
			out = append(out, a.expr(el))
		}
		return out
	}
	return []ast.Expr{a.expr(e)}
}

func (a *Adapter) expr(e Expr) ast.Expr {
	if e == nil {
		return nil
	}
	switch e := e.(type) {
	case *Name:
		return a.ident(e)
	case *BasicLit:
		return a.basicLit(e)
	case *CompositeLit:
		cl := &ast.CompositeLit{
			Type:   a.expr(e.Type),
			Rbrace: a.pos(e.Rbrace),
		}
		for _, el := range e.ElemList {
			cl.Elts = append(cl.Elts, a.expr(el))
		}
		return cl
	case *KeyValueExpr:
		return &ast.KeyValueExpr{
			Key:   a.expr(e.Key),
			Colon: a.pos(e.Pos()),
			Value: a.expr(e.Value),
		}
	case *FuncLit:
		return &ast.FuncLit{
			Type: a.funcType(e.Type),
			Body: a.blockStmt(e.Body),
		}
	case *ParenExpr:
		return &ast.ParenExpr{
			Lparen: a.pos(e.Pos()),
			X:      a.expr(e.X),
		}
	case *SelectorExpr:
		return &ast.SelectorExpr{
			X:   a.expr(e.X),
			Sel: a.ident(e.Sel),
		}
	case *IndexExpr:
		if le, ok := e.Index.(*ListExpr); ok && len(le.ElemList) > 1 {
			var indices []ast.Expr
			for _, el := range le.ElemList {
				indices = append(indices, a.expr(el))
			}
			return &ast.IndexListExpr{
				X:       a.expr(e.X),
				Indices: indices,
			}
		}
		return &ast.IndexExpr{
			X:     a.expr(e.X),
			Index: a.expr(e.Index),
		}
	case *SliceExpr:
		se := &ast.SliceExpr{
			X:      a.expr(e.X),
			Low:    a.expr(e.Index[0]),
			High:   a.expr(e.Index[1]),
			Max:    a.expr(e.Index[2]),
			Slice3: e.Full,
		}
		return se
	case *AssertExpr:
		return &ast.TypeAssertExpr{
			X:    a.expr(e.X),
			Type: a.expr(e.Type),
		}
	case *TypeSwitchGuard:
		return &ast.TypeAssertExpr{
			X: a.expr(e.X),
		}
	case *Operation:
		return a.operation(e)
	case *CallExpr:
		ce := &ast.CallExpr{
			Fun: a.expr(e.Fun),
		}
		for _, arg := range e.ArgList {
			ce.Args = append(ce.Args, a.expr(arg))
		}
		if e.HasDots {
			ce.Ellipsis = a.pos(e.Pos())
		}
		return ce
	case *ListExpr:
		if len(e.ElemList) == 1 {
			return a.expr(e.ElemList[0])
		}
		// multi-element list: shouldn't appear as a standalone expression
		return a.expr(e.ElemList[0])
	case *ArrayType:
		if e.Len == nil {
			return &ast.ArrayType{
				Lbrack: a.pos(e.Pos()),
				Len:    &ast.Ellipsis{Ellipsis: a.pos(e.Pos())},
				Elt:    a.expr(e.Elem),
			}
		}
		return &ast.ArrayType{
			Lbrack: a.pos(e.Pos()),
			Len:    a.expr(e.Len),
			Elt:    a.expr(e.Elem),
		}
	case *SliceType:
		return &ast.ArrayType{
			Lbrack: a.pos(e.Pos()),
			Elt:    a.expr(e.Elem),
		}
	case *DotsType:
		return &ast.Ellipsis{
			Ellipsis: a.pos(e.Pos()),
			Elt:      a.expr(e.Elem),
		}
	case *StructType:
		return &ast.StructType{
			Struct: a.pos(e.Pos()),
			Fields: a.structFieldList(e),
		}
	case *InterfaceType:
		methods := a.fieldListFromFields(e.MethodList)
		if methods == nil {
			methods = &ast.FieldList{}
		}
		return &ast.InterfaceType{
			Interface: a.pos(e.Pos()),
			Methods:   methods,
		}
	case *FuncType:
		return a.funcType(e)
	case *MapType:
		return &ast.MapType{
			Map:   a.pos(e.Pos()),
			Key:   a.expr(e.Key),
			Value: a.expr(e.Value),
		}
	case *ChanType:
		ct := &ast.ChanType{
			Begin: a.pos(e.Pos()),
			Value: a.expr(e.Elem),
		}
		switch e.Dir {
		case SendOnly:
			ct.Dir = ast.SEND
		case RecvOnly:
			ct.Dir = ast.RECV
		default:
			ct.Dir = ast.SEND | ast.RECV
		}
		return ct
	case *BadExpr:
		return &ast.BadExpr{From: a.pos(e.Pos())}
	}
	return &ast.BadExpr{}
}

func (a *Adapter) operation(e *Operation) ast.Expr {
	if e.Y == nil {
		if e.Op == Mul {
			return &ast.StarExpr{
				Star: a.pos(e.Pos()),
				X:    a.expr(e.X),
			}
		}
		if e.Op == And {
			return &ast.UnaryExpr{
				OpPos: a.pos(e.Pos()),
				Op:    token.AND,
				X:     a.expr(e.X),
			}
		}
		return &ast.UnaryExpr{
			OpPos: a.pos(e.Pos()),
			Op:    operatorToToken(e.Op),
			X:     a.expr(e.X),
		}
	}
	return &ast.BinaryExpr{
		X:     a.expr(e.X),
		OpPos: a.pos(e.Pos()),
		Op:    operatorToToken(e.Op),
		Y:     a.expr(e.Y),
	}
}

func operatorToToken(op Operator) token.Token {
	switch op {
	case Add:
		return token.ADD
	case Sub:
		return token.SUB
	case Mul:
		return token.MUL
	case Div:
		return token.QUO
	case Rem:
		return token.REM
	case And:
		return token.AND
	case Or:
		return token.OR
	case Xor:
		return token.XOR
	case Shl:
		return token.SHL
	case Shr:
		return token.SHR
	case AndNot:
		return token.AND_NOT
	case OrOr:
		return token.LOR
	case AndAnd:
		return token.LAND
	case Eql:
		return token.EQL
	case Neq:
		return token.NEQ
	case Lss:
		return token.LSS
	case Leq:
		return token.LEQ
	case Gtr:
		return token.GTR
	case Geq:
		return token.GEQ
	case Not:
		return token.NOT
	case Recv:
		return token.ARROW
	case Tilde:
		return token.TILDE
	}
	return token.ILLEGAL
}

func (a *Adapter) basicLit(lit *BasicLit) *ast.BasicLit {
	if lit == nil {
		return nil
	}
	var kind token.Token
	switch lit.Kind {
	case IntLit:
		kind = token.INT
	case FloatLit:
		kind = token.FLOAT
	case ImagLit:
		kind = token.IMAG
	case RuneLit:
		kind = token.CHAR
	case StringLit:
		kind = token.STRING
	}
	return &ast.BasicLit{
		ValuePos: a.pos(lit.Pos()),
		Kind:     kind,
		Value:    lit.Value,
	}
}

func (a *Adapter) funcType(ft *FuncType) *ast.FuncType {
	if ft == nil {
		return &ast.FuncType{Params: &ast.FieldList{}}
	}
	params := a.fieldListFromFields(ft.ParamList)
	if params == nil {
		params = &ast.FieldList{}
	}
	return &ast.FuncType{
		Func:    a.pos(ft.Pos()),
		Params:  params,
		Results: a.fieldListFromFields(ft.ResultList),
	}
}

func (a *Adapter) fieldListFromFields(fields []*Field) *ast.FieldList {
	if len(fields) == 0 {
		return nil
	}
	fl := &ast.FieldList{}
	for _, f := range fields {
		fl.List = append(fl.List, a.field(f))
	}
	return fl
}

func (a *Adapter) fieldList(fields []*Field) *ast.FieldList {
	return a.fieldListFromFields(fields)
}

func (a *Adapter) field(f *Field) *ast.Field {
	af := &ast.Field{
		Type: a.expr(f.Type),
	}
	if f.Name != nil {
		af.Names = []*ast.Ident{a.ident(f.Name)}
	}
	return af
}

func (a *Adapter) structFieldList(st *StructType) *ast.FieldList {
	fl := &ast.FieldList{}
	for i, f := range st.FieldList {
		af := a.field(f)
		if i < len(st.TagList) && st.TagList[i] != nil {
			af.Tag = a.basicLit(st.TagList[i])
		}
		fl.List = append(fl.List, af)
	}
	return fl
}

func (a *Adapter) blockStmt(b *BlockStmt) *ast.BlockStmt {
	if b == nil {
		return nil
	}
	bs := &ast.BlockStmt{
		Lbrace: a.pos(b.Pos()),
		Rbrace: a.pos(b.Rbrace),
	}
	for _, s := range b.List {
		as := a.stmt(s)
		if as != nil {
			bs.List = append(bs.List, as)
		}
	}
	return bs
}

func (a *Adapter) stmt(s Stmt) ast.Stmt {
	if s == nil {
		return nil
	}
	switch s := s.(type) {
	case *EmptyStmt:
		return &ast.EmptyStmt{Semicolon: a.pos(s.Pos())}
	case *LabeledStmt:
		return &ast.LabeledStmt{
			Label: a.ident(s.Label),
			Colon: a.pos(s.Pos()),
			Stmt:  a.stmt(s.Stmt),
		}
	case *BlockStmt:
		return a.blockStmt(s)
	case *ExprStmt:
		return &ast.ExprStmt{X: a.expr(s.X)}
	case *SendStmt:
		return &ast.SendStmt{
			Chan:  a.expr(s.Chan),
			Arrow: a.pos(s.Pos()),
			Value: a.expr(s.Value),
		}
	case *DeclStmt:
		gd := &ast.GenDecl{TokPos: a.pos(s.Pos()), Tok: token.VAR}
		for _, d := range s.DeclList {
			ad := a.decl(d)
			if gd2, ok := ad.(*ast.GenDecl); ok {
				gd.Tok = gd2.Tok
				gd.Specs = append(gd.Specs, gd2.Specs...)
			}
		}
		return &ast.DeclStmt{Decl: gd}
	case *AssignStmt:
		if s.Rhs == nil {
			// increment/decrement
			tok := token.INC
			if s.Op == Sub {
				tok = token.DEC
			}
			return &ast.IncDecStmt{
				X:      a.expr(s.Lhs),
				TokPos: a.pos(s.Pos()),
				Tok:    tok,
			}
		}
		tok := token.ASSIGN
		if s.Op == Def {
			tok = token.DEFINE
		} else if s.Op != 0 {
			tok = assignOpToToken(s.Op)
		}
		return &ast.AssignStmt{
			Lhs: a.exprList(s.Lhs),
			Tok: tok,
			Rhs: a.exprList(s.Rhs),
		}
	case *BranchStmt:
		tok := token.BREAK
		switch s.Tok {
		case Continue:
			tok = token.CONTINUE
		case Goto:
			tok = token.GOTO
		}
		return &ast.BranchStmt{
			TokPos: a.pos(s.Pos()),
			Tok:    tok,
			Label:  a.ident(s.Label),
		}
	case *CallStmt:
		x := a.expr(s.Call)
		ce, ok := x.(*ast.CallExpr)
		if !ok {
			// bare `defer f`: f is a function value, not a call
			ce = &ast.CallExpr{
				Fun:    x,
				Lparen: a.pos(s.Pos()),
				Rparen: a.pos(s.Pos()),
			}
		}
		return &ast.DeferStmt{
			Defer: a.pos(s.Pos()),
			Call:  ce,
		}
	case *ReturnStmt:
		return &ast.ReturnStmt{
			Return:  a.pos(s.Pos()),
			Results: a.exprList(s.Results),
		}
	case *IfStmt:
		is := &ast.IfStmt{
			If:   a.pos(s.Pos()),
			Init: a.stmt(s.Init),
			Cond: a.expr(s.Cond),
			Body: a.blockStmt(s.Then),
			Else: a.stmt(s.Else),
		}
		return is
	case *ForStmt:
		if rc, ok := s.Init.(*RangeClause); ok {
			rs := &ast.RangeStmt{
				For:  a.pos(s.Pos()),
				X:    a.expr(rc.X),
				Body: a.blockStmt(s.Body),
			}
			if rc.Lhs != nil {
				lhs := a.exprList(rc.Lhs)
				if len(lhs) > 0 {
					rs.Key = lhs[0]
				}
				if len(lhs) > 1 {
					rs.Value = lhs[1]
				}
				if rc.Def {
					rs.Tok = token.DEFINE
				} else {
					rs.Tok = token.ASSIGN
				}
			}
			return rs
		}
		return &ast.ForStmt{
			For:  a.pos(s.Pos()),
			Init: a.stmt(s.Init),
			Cond: a.expr(s.Cond),
			Post: a.stmt(s.Post),
			Body: a.blockStmt(s.Body),
		}
	case *SwitchStmt:
		if s.Tag != nil {
			if _, ok := s.Tag.(*TypeSwitchGuard); ok {
				return a.typeSwitchStmt(s)
			}
		}
		ss := &ast.SwitchStmt{
			Switch: a.pos(s.Pos()),
			Init:   a.stmt(s.Init),
			Tag:    a.expr(s.Tag),
			Body:   a.switchBody(s.Body, s.Rbrace),
		}
		return ss
	case *SelectStmt:
		return &ast.SelectStmt{
			Select: a.pos(s.Pos()),
			Body:   a.selectBody(s.Body, s.Rbrace),
		}
	}
	return &ast.EmptyStmt{Semicolon: a.pos(s.Pos())}
}

func (a *Adapter) typeSwitchStmt(s *SwitchStmt) *ast.TypeSwitchStmt {
	tsg := s.Tag.(*TypeSwitchGuard)
	var assign ast.Stmt
	ta := &ast.TypeAssertExpr{X: a.expr(tsg.X)}
	if tsg.Lhs != nil {
		assign = &ast.AssignStmt{
			Lhs: []ast.Expr{a.ident(tsg.Lhs)},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{ta},
		}
	} else {
		assign = &ast.ExprStmt{X: ta}
	}
	return &ast.TypeSwitchStmt{
		Switch: a.pos(s.Pos()),
		Init:   a.stmt(s.Init),
		Assign: assign,
		Body:   a.switchBody(s.Body, s.Rbrace),
	}
}

func (a *Adapter) switchBody(clauses []*CaseClause, rbrace Pos) *ast.BlockStmt {
	bs := &ast.BlockStmt{Rbrace: a.pos(rbrace)}
	for _, c := range clauses {
		cc := &ast.CaseClause{
			Case:  a.pos(c.Pos()),
			List:  a.exprList(c.Cases),
			Colon: a.pos(c.Colon),
		}
		for _, s := range c.Body {
			as := a.stmt(s)
			if as != nil {
				cc.Body = append(cc.Body, as)
			}
		}
		bs.List = append(bs.List, cc)
	}
	return bs
}

func (a *Adapter) selectBody(clauses []*CommClause, rbrace Pos) *ast.BlockStmt {
	bs := &ast.BlockStmt{Rbrace: a.pos(rbrace)}
	for _, c := range clauses {
		cc := &ast.CommClause{
			Case:  a.pos(c.Pos()),
			Comm:  a.stmt(c.Comm),
			Colon: a.pos(c.Colon),
		}
		for _, s := range c.Body {
			as := a.stmt(s)
			if as != nil {
				cc.Body = append(cc.Body, as)
			}
		}
		bs.List = append(bs.List, cc)
	}
	return bs
}

func assignOpToToken(op Operator) token.Token {
	switch op {
	case Add:
		return token.ADD_ASSIGN
	case Sub:
		return token.SUB_ASSIGN
	case Mul:
		return token.MUL_ASSIGN
	case Div:
		return token.QUO_ASSIGN
	case Rem:
		return token.REM_ASSIGN
	case And:
		return token.AND_ASSIGN
	case Or:
		return token.OR_ASSIGN
	case Xor:
		return token.XOR_ASSIGN
	case Shl:
		return token.SHL_ASSIGN
	case Shr:
		return token.SHR_ASSIGN
	case AndNot:
		return token.AND_NOT_ASSIGN
	}
	return token.ASSIGN
}
