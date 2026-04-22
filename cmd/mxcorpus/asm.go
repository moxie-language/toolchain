package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func extractASM(outdir string) error {
	for _, opt := range optLevels {
		label := optLabel(opt)
		nFuncs, err := emitAndExtractASM(label, outdir)
		if err != nil {
			return fmt.Errorf("asm %s: %w", label, err)
		}
		fmt.Printf("  ASM %s: %d functions\n", label, nFuncs)
	}
	return nil
}

func emitAndExtractASM(label, outdir string) (int, error) {
	modIR := "module.ll"
	if label != "" {
		modIR = "module." + label + ".ll"
	}
	irPath := filepath.Join(outdir, modIR)
	irData, err := os.ReadFile(irPath)
	if err != nil {
		return 0, fmt.Errorf("read IR: %w", err)
	}

	cmd := exec.Command("llc", "-filetype=asm", "-o", "-")
	cmd.Stdin = strings.NewReader(string(irData))
	asmData, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return 0, fmt.Errorf("llc asm: %s", string(exitErr.Stderr))
		}
		return 0, fmt.Errorf("llc asm: %w", err)
	}

	asmFilename := "module.s"
	if label != "" {
		asmFilename = "module." + label + ".s"
	}
	if err := os.WriteFile(filepath.Join(outdir, asmFilename), asmData, 0644); err != nil {
		return 0, err
	}

	return extractFunctionASM(asmData, label, outdir)
}

func extractFunctionASM(asmData []byte, label, outdir string) (int, error) {
	segDir := filepath.Join(outdir, "asm")
	if err := os.MkdirAll(segDir, 0755); err != nil {
		return 0, err
	}

	n := 0
	lines := strings.Split(string(asmData), "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]
		name := extractASMFuncLabel(line)
		if name == "" {
			i++
			continue
		}

		// Backtrack to include preamble directives (.hidden, .globl, .p2align, .type).
		start := i
		for start > 0 {
			prev := lines[start-1]
			if strings.HasPrefix(prev, "\t.hidden") || strings.HasPrefix(prev, "\t.globl") ||
				strings.HasPrefix(prev, "\t.p2align") || strings.HasPrefix(prev, "\t.type") {
				start--
				continue
			}
			break
		}

		// Scan forward to "# -- End function" or next function preamble.
		i++
		for i < len(lines) {
			if strings.Contains(lines[i], "# -- End function") {
				i++
				break
			}
			i++
		}

		funcASM := strings.Join(lines[start:i], "\n") + "\n"
		safeName := sanitizeIRName(name)
		filename := safeName + ".s"
		if label != "" {
			filename = safeName + "." + label + ".s"
		}

		if err := os.WriteFile(filepath.Join(segDir, filename), []byte(funcASM), 0644); err != nil {
			return 0, err
		}
		n++
	}
	return n, nil
}

func extractASMFuncLabel(line string) string {
	if len(line) == 0 || line[0] == '.' || line[0] == '\t' || line[0] == ' ' || line[0] == '#' {
		return ""
	}
	// Quoted labels: "pkg/path.Func":  # @"pkg/path.Func"
	if line[0] == '"' {
		colonIdx := strings.Index(line, "\":")
		if colonIdx < 1 {
			return ""
		}
		return line[1:colonIdx]
	}
	if !strings.HasSuffix(line, ":") {
		return ""
	}
	name := strings.TrimSuffix(line, ":")
	if strings.HasPrefix(name, ".") {
		return ""
	}
	return name
}
