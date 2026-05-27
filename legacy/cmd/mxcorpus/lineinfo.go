package main

import (
	"debug/dwarf"
	"debug/elf"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func extractLineInfo(outdir string) error {
	objPath := filepath.Join(outdir, "module.O0.o")
	nFuncs, err := extractFunctionLineInfo(objPath, outdir)
	if err != nil {
		return fmt.Errorf("lineinfo: %w", err)
	}
	fmt.Printf("  LINEINFO: %d functions\n", nFuncs)
	return nil
}

func extractFunctionLineInfo(objPath, outdir string) (int, error) {
	segDir := filepath.Join(outdir, "lineinfo")
	if err := os.MkdirAll(segDir, 0755); err != nil {
		return 0, err
	}

	f, err := elf.Open(objPath)
	if err != nil {
		return 0, fmt.Errorf("elf open: %w", err)
	}
	defer f.Close()

	syms, err := f.Symbols()
	if err != nil {
		return 0, fmt.Errorf("elf symbols: %w", err)
	}

	type funcSym struct {
		name   string
		offset uint64
		size   uint64
	}
	var funcs []funcSym
	for _, sym := range syms {
		if elf.ST_TYPE(sym.Info) != elf.STT_FUNC || sym.Size == 0 {
			continue
		}
		if sym.Section == elf.SHN_UNDEF || int(sym.Section) >= len(f.Sections) {
			continue
		}
		if f.Sections[sym.Section].Name != ".text" {
			continue
		}
		funcs = append(funcs, funcSym{name: sym.Name, offset: sym.Value, size: sym.Size})
	}

	sort.Slice(funcs, func(i, j int) bool { return funcs[i].offset < funcs[j].offset })

	dw, err := f.DWARF()
	if err != nil {
		return 0, fmt.Errorf("dwarf: %w", err)
	}

	reader := dw.Reader()
	var lineEntries []dwarf.LineEntry
	for {
		entry, err := reader.Next()
		if err != nil {
			return 0, fmt.Errorf("dwarf read: %w", err)
		}
		if entry == nil {
			break
		}
		if entry.Tag != dwarf.TagCompileUnit {
			continue
		}
		lr, err := dw.LineReader(entry)
		if err != nil || lr == nil {
			continue
		}
		var le dwarf.LineEntry
		for {
			err := lr.Next(&le)
			if err != nil {
				break
			}
			lineEntries = append(lineEntries, le)
		}
	}

	sort.Slice(lineEntries, func(i, j int) bool {
		return lineEntries[i].Address < lineEntries[j].Address
	})

	n := 0
	for _, fn := range funcs {
		start := fn.offset
		end := fn.offset + fn.size

		var sb strings.Builder
		for _, le := range lineEntries {
			if le.Address < start || le.Address >= end {
				continue
			}
			relOff := le.Address - start
			fmt.Fprintf(&sb, "0x%04x  %s:%d\n", relOff, filepath.Base(le.File.Name), le.Line)
		}

		if sb.Len() == 0 {
			continue
		}

		safeName := sanitizeIRName(fn.name)
		filename := safeName + ".lineinfo"
		if err := os.WriteFile(filepath.Join(segDir, filename), []byte(sb.String()), 0644); err != nil {
			return 0, err
		}
		n++
	}

	return n, nil
}
