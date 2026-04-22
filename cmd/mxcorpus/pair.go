package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func enrichManifest(importPath, outdir string) error {
	manifestPath := filepath.Join(outdir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var manifest PackageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	pairManifest(&manifest, importPath, outdir)

	data, err = json.MarshalIndent(&manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return err
	}

	paired := 0
	for _, f := range manifest.Files {
		for _, s := range f.Segments {
			if len(s.IRFiles) > 0 {
				paired++
			}
		}
	}
	fmt.Printf("  PAIRED: %d segments\n", paired)
	return nil
}

func pairManifest(manifest *PackageManifest, importPath, outdir string) {
	irFiles := listSegDir(filepath.Join(outdir, "ir"))
	asmFiles := listSegDir(filepath.Join(outdir, "asm"))
	binFiles := listSegDir(filepath.Join(outdir, "bin"))
	lineinfoFiles := listSegDir(filepath.Join(outdir, "lineinfo"))

	for fi := range manifest.Files {
		fm := &manifest.Files[fi]
		for si := range fm.Segments {
			seg := &fm.Segments[si]
			if seg.Kind != "func" && seg.Kind != "method" {
				continue
			}

			irName := buildIRName(importPath, seg.Kind, seg.Name)
			seg.IRName = irName
			safeName := sanitizeIRName(irName)

			seg.IRFiles = matchOptFiles(irFiles, safeName, ".ll")
			seg.ASMFiles = matchOptFiles(asmFiles, safeName, ".s")
			seg.BinFiles = matchOptFiles(binFiles, safeName, ".bin.hex")

			liFile := safeName + ".lineinfo"
			if info, ok := lineinfoFiles[liFile]; ok {
				seg.Lineinfo = &SizedFile{File: liFile, SizeBytes: info}
			}
		}
	}
}

func buildIRName(importPath, kind, name string) string {
	if kind == "method" {
		// name is like "(*RuneIter).Next" → "(*unicode/utf8.RuneIter).Next"
		if strings.HasPrefix(name, "(*") {
			dotIdx := strings.Index(name, ").")
			if dotIdx > 2 {
				typeName := name[2:dotIdx]
				method := name[dotIdx+2:]
				return "(*" + importPath + "." + typeName + ")." + method
			}
		}
		// Value receiver: "(T).M" → "(unicode/utf8.T).M"
		if strings.HasPrefix(name, "(") {
			dotIdx := strings.Index(name, ").")
			if dotIdx > 1 {
				typeName := name[1:dotIdx]
				method := name[dotIdx+2:]
				return "(" + importPath + "." + typeName + ")." + method
			}
		}
	}
	return importPath + "." + name
}

func matchOptFiles(files map[string]int, baseName, ext string) OptFileMap {
	m := OptFileMap{}
	for _, opt := range optLevels {
		label := optLabel(opt)
		filename := baseName + "." + label + ext
		if size, ok := files[filename]; ok {
			m[label] = &SizedFile{File: filename, SizeBytes: size}
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func listSegDir(dir string) map[string]int {
	m := map[string]int{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return m
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		m[e.Name()] = int(info.Size())
	}
	return m
}
