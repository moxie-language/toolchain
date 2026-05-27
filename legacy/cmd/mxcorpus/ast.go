package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type parsedFile struct {
	name    string
	origSrc []byte // original .mx source before text rewrites
	src     []byte // after text rewrites (parseable Go)
	file    *ast.File
}

func extractAST(fset *token.FileSet, pf parsedFile, fileIndex int, outdir string) (FileManifest, error) {
	if err := os.MkdirAll(outdir, 0755); err != nil {
		return FileManifest{}, err
	}

	// Copy verbatim original source.
	if err := os.WriteFile(filepath.Join(outdir, pf.name), pf.origSrc, 0644); err != nil {
		return FileManifest{}, err
	}

	segDir := filepath.Join(outdir, pf.name+".segments")
	if err := os.MkdirAll(segDir, 0755); err != nil {
		return FileManifest{}, err
	}

	astDir := filepath.Join(outdir, "ast")
	if err := os.MkdirAll(astDir, 0755); err != nil {
		return FileManifest{}, err
	}

	fm := FileManifest{
		Path:      pf.name,
		FileIndex: fileIndex,
	}

	// Use the rewritten source for segment extraction (AST positions match rewritten bytes).
	segments := segmentFile(fset, pf.file, pf.src, fileIndex)
	for si, seg := range segments {
		segFilename := seg.Filename()
		segPath := filepath.Join(segDir, segFilename)
		if err := os.WriteFile(segPath, seg.Data, 0644); err != nil {
			return FileManifest{}, fmt.Errorf("write segment %s: %w", segPath, err)
		}

		// Generate AST dump for declarations (si=0 is pkg clause, decls start at si=1).
		astDumpFile := ""
		declIdx := si - 1
		if declIdx >= 0 && declIdx < len(pf.file.Decls) {
			dump := dumpAST(fset, pf.file.Decls[declIdx])
			if len(dump) > 0 {
				astDumpFile = strings.TrimSuffix(segFilename, ".mx") + ".ast"
				astPath := filepath.Join(astDir, astDumpFile)
				if err := os.WriteFile(astPath, []byte(dump), 0644); err != nil {
					return FileManifest{}, fmt.Errorf("write ast dump %s: %w", astPath, err)
				}
			}
		}

		sm := SegmentManifest{
			ID:        strings.TrimSuffix(segFilename, ".mx"),
			Kind:      seg.Kind,
			Name:      seg.Name,
			ASTFile:   segFilename,
			ASTDump:   astDumpFile,
			SizeBytes: len(seg.Data),
			SourcePos: SourcePos{Line: seg.StartLine, EndLine: seg.EndLine},
			Comments:  seg.Comments,
			Generated: false,
			HasSpawn:  seg.HasSpawn,
		}
		fm.Segments = append(fm.Segments, sm)
	}

	return fm, nil
}

type segment struct {
	FileIndex int
	Seq       int
	Line      int
	Kind      string
	Name      string
	Data      []byte
	StartLine int
	EndLine   int
	Comments  []string
	HasSpawn  bool
}

func (s *segment) Filename() string {
	return fmt.Sprintf("f%02d_%03d_L%d_%s_%s.mx", s.FileIndex, s.Seq, s.Line, s.Kind, sanitizeName(s.Name))
}

func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "*", "_")
	name = strings.ReplaceAll(name, "(", "")
	name = strings.ReplaceAll(name, ")", "")
	return name
}

func segmentFile(fset *token.FileSet, file *ast.File, src []byte, fileIndex int) []segment {
	tokFile := fset.File(file.Pos())
	if tokFile == nil {
		return nil
	}

	var segments []segment
	seq := 0

	prevEnd := 0

	// Package clause segment.
	{
		seq++
		pkgEnd := tokFile.Offset(file.Name.End())
		segEnd := skipToEndOfLine(src, pkgEnd)

		var comments []string
		if file.Doc != nil {
			for _, c := range file.Doc.List {
				comments = append(comments, c.Text)
			}
		}

		startLine := 1
		endLine := fset.Position(file.Name.End()).Line

		segments = append(segments, segment{
			FileIndex: fileIndex,
			Seq:       seq,
			Line:      startLine,
			Kind:      "pkg",
			Name:      file.Name.Name,
			Data:      src[0:segEnd],
			StartLine: startLine,
			EndLine:   endLine,
			Comments:  comments,
		})
		prevEnd = segEnd
	}

	for i, decl := range file.Decls {
		seq++

		declStart := tokFile.Offset(decl.Pos())
		declEnd := tokFile.Offset(decl.End())

		docStart := declStart
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil {
				ds := tokFile.Offset(d.Doc.Pos())
				if ds < docStart {
					docStart = ds
				}
			}
		case *ast.GenDecl:
			if d.Doc != nil {
				ds := tokFile.Offset(d.Doc.Pos())
				if ds < docStart {
					docStart = ds
				}
			}
		}

		segStart := prevEnd
		if docStart < segStart {
			segStart = docStart
		}

		segEnd := declEnd
		if i+1 < len(file.Decls) {
			nextStart := offsetOfDecl(tokFile, file.Decls[i+1])
			segEnd = nextStart
		} else {
			segEnd = len(src)
		}

		kind, name := classifyDecl(decl)
		startLine := fset.Position(decl.Pos()).Line
		endLine := fset.Position(decl.End()).Line

		var comments []string
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil {
				for _, c := range d.Doc.List {
					comments = append(comments, c.Text)
				}
			}
		case *ast.GenDecl:
			if d.Doc != nil {
				for _, c := range d.Doc.List {
					comments = append(comments, c.Text)
				}
			}
		}

		hasSpawn := false
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
			hasSpawn = detectSpawn(fd.Body)
		}

		segments = append(segments, segment{
			FileIndex: fileIndex,
			Seq:       seq,
			Line:      startLine,
			Kind:      kind,
			Name:      name,
			Data:      src[segStart:segEnd],
			StartLine: startLine,
			EndLine:   endLine,
			Comments:  comments,
			HasSpawn:  hasSpawn,
		})
		prevEnd = segEnd
	}

	return segments
}

func offsetOfDecl(tokFile *token.File, decl ast.Decl) int {
	start := tokFile.Offset(decl.Pos())
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Doc != nil {
			ds := tokFile.Offset(d.Doc.Pos())
			if ds < start {
				return ds
			}
		}
	case *ast.GenDecl:
		if d.Doc != nil {
			ds := tokFile.Offset(d.Doc.Pos())
			if ds < start {
				return ds
			}
		}
	}
	return start
}

func classifyDecl(decl ast.Decl) (kind, name string) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil && len(d.Recv.List) > 0 {
			recv := exprName(d.Recv.List[0].Type)
			return "method", "(" + recv + ")." + d.Name.Name
		}
		return "func", d.Name.Name
	case *ast.GenDecl:
		switch d.Tok {
		case token.IMPORT:
			return "import", "import"
		case token.CONST:
			if len(d.Specs) > 0 {
				if vs, ok := d.Specs[0].(*ast.ValueSpec); ok && len(vs.Names) > 0 {
					return "const", vs.Names[0].Name
				}
			}
			return "const", "const"
		case token.TYPE:
			if len(d.Specs) > 0 {
				if ts, ok := d.Specs[0].(*ast.TypeSpec); ok {
					return "type", ts.Name.Name
				}
			}
			return "type", "type"
		case token.VAR:
			if len(d.Specs) > 0 {
				if vs, ok := d.Specs[0].(*ast.ValueSpec); ok && len(vs.Names) > 0 {
					return "var", vs.Names[0].Name
				}
			}
			return "var", "var"
		}
	}
	return "unknown", "unknown"
}

func exprName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprName(t.X)
	case *ast.IndexExpr:
		return exprName(t.X)
	case *ast.IndexListExpr:
		return exprName(t.X)
	}
	return "?"
}

func detectSpawn(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "spawn" {
			found = true
			return false
		}
		return true
	})
	return found
}

func skipToEndOfLine(src []byte, off int) int {
	for off < len(src) {
		if src[off] == '\n' {
			return off + 1
		}
		off++
	}
	return off
}
