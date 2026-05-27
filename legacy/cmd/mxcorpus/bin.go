package main

import (
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func extractBIN(outdir string) error {
	for _, opt := range optLevels {
		label := optLabel(opt)
		nFuncs, err := emitAndExtractBIN(label, outdir)
		if err != nil {
			return fmt.Errorf("bin %s: %w", label, err)
		}
		fmt.Printf("  BIN %s: %d functions\n", label, nFuncs)
	}
	return nil
}

func emitAndExtractBIN(label, outdir string) (int, error) {
	modIR := "module.ll"
	if label != "" {
		modIR = "module." + label + ".ll"
	}
	irPath := filepath.Join(outdir, modIR)
	irData, err := os.ReadFile(irPath)
	if err != nil {
		return 0, fmt.Errorf("read IR: %w", err)
	}

	cmd := exec.Command("llc", "-filetype=obj", "-o", "-")
	cmd.Stdin = strings.NewReader(string(irData))
	objData, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return 0, fmt.Errorf("llc obj: %s", string(exitErr.Stderr))
		}
		return 0, fmt.Errorf("llc obj: %w", err)
	}

	objFilename := "module.o"
	if label != "" {
		objFilename = "module." + label + ".o"
	}
	objPath := filepath.Join(outdir, objFilename)
	if err := os.WriteFile(objPath, objData, 0644); err != nil {
		return 0, err
	}

	return extractFunctionBIN(objPath, label, outdir)
}

func extractFunctionBIN(objPath, label, outdir string) (int, error) {
	segDir := filepath.Join(outdir, "bin")
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

	// Find .text section.
	var textSection *elf.Section
	for _, s := range f.Sections {
		if s.Name == ".text" {
			textSection = s
			break
		}
	}
	if textSection == nil {
		return 0, fmt.Errorf("no .text section in %s", objPath)
	}

	textData, err := textSection.Data()
	if err != nil {
		return 0, fmt.Errorf("read .text: %w", err)
	}

	// Collect function symbols (STT_FUNC with nonzero size in .text).
	type funcSym struct {
		name   string
		offset uint64
		size   uint64
	}
	var funcs []funcSym
	for _, sym := range syms {
		if elf.ST_TYPE(sym.Info) != elf.STT_FUNC {
			continue
		}
		if sym.Size == 0 {
			continue
		}
		if sym.Section == elf.SHN_UNDEF {
			continue
		}
		if int(sym.Section) >= len(f.Sections) {
			continue
		}
		if f.Sections[sym.Section].Name != ".text" {
			continue
		}
		// sym.Value is relative to the section in relocatable objects.
		funcs = append(funcs, funcSym{
			name:   sym.Name,
			offset: sym.Value,
			size:   sym.Size,
		})
	}

	sort.Slice(funcs, func(i, j int) bool {
		return funcs[i].offset < funcs[j].offset
	})

	n := 0
	for _, fn := range funcs {
		if fn.offset+fn.size > uint64(len(textData)) {
			continue
		}
		bytes := textData[fn.offset : fn.offset+fn.size]

		var sb strings.Builder
		for off := 0; off < len(bytes); off += 16 {
			end := off + 16
			if end > len(bytes) {
				end = len(bytes)
			}
			fmt.Fprintf(&sb, "%04x:", off)
			for _, b := range bytes[off:end] {
				fmt.Fprintf(&sb, " %02x", b)
			}
			sb.WriteByte('\n')
		}

		safeName := sanitizeIRName(fn.name)
		filename := safeName + ".bin.hex"
		if label != "" {
			filename = safeName + "." + label + ".bin.hex"
		}

		if err := os.WriteFile(filepath.Join(segDir, filename), []byte(sb.String()), 0644); err != nil {
			return 0, err
		}
		n++
	}

	return n, nil
}
