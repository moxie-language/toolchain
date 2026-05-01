// mxcorpus generates a paired AST/IR/ASM/BIN corpus from Moxie source packages.
//
// For each package, it produces:
//   - Source segments (.mx) extracted from original source by declaration
//   - LLVM IR segments (.ll) at four optimization levels (O0, O1, O2, Os)
//   - Assembly segments (.s) at four optimization levels
//   - Binary segments (.bin.hex) at four optimization levels
//   - DWARF line info (.lineinfo) mapping binary offsets to source lines
//   - A manifest.json mapping all segments with positions and metadata
//
// Usage:
//
//	mxcorpus -o <outdir> [-verify] <directory...>         # AST only
//	mxcorpus -o <outdir> -ir <import-path>                # AST + IR
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"moxie/mxtext"
)

var (
	flagVerify bool
	flagIR     string
	flagModDir string
)

func main() {
	outdir := flag.String("o", "corpus", "output directory")
	flag.BoolVar(&flagVerify, "verify", false, "verify segment concatenation")
	flag.StringVar(&flagIR, "ir", "", "generate IR for this import path (e.g. unicode/utf8)")
	flag.StringVar(&flagModDir, "moddir", "", "module root directory for non-stdlib packages")
	flag.Parse()

	// AST extraction from directories.
	var pkgDirs []string
	for _, dir := range flag.Args() {
		dirs, err := processDir(dir, *outdir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", dir, err)
			os.Exit(1)
		}
		pkgDirs = append(pkgDirs, dirs...)
	}

	// IR + ASM + BIN + lineinfo + pairing from import path.
	if flagIR != "" {
		pkgDir := ""
		irBase := filepath.Base(flagIR)
		for _, d := range pkgDirs {
			if filepath.Base(d) == irBase {
				pkgDir = d
				break
			}
		}
		if pkgDir == "" {
			if len(pkgDirs) == 1 {
				pkgDir = pkgDirs[0]
			} else {
				fmt.Fprintf(os.Stderr, "ir: cannot determine package directory for %q among %v\n", flagIR, pkgDirs)
				os.Exit(1)
			}
		}
		if err := extractIR(flagIR, pkgDir); err != nil {
			fmt.Fprintf(os.Stderr, "ir: %v\n", err)
			os.Exit(1)
		}
		if err := extractASM(pkgDir); err != nil {
			fmt.Fprintf(os.Stderr, "asm: %v\n", err)
			os.Exit(1)
		}
		if err := extractBIN(pkgDir); err != nil {
			fmt.Fprintf(os.Stderr, "bin: %v (non-fatal)\n", err)
		}
		if err := extractLineInfo(pkgDir); err != nil {
			fmt.Fprintf(os.Stderr, "lineinfo: %v (non-fatal)\n", err)
		}
		if err := enrichManifest(flagIR, pkgDir); err != nil {
			fmt.Fprintf(os.Stderr, "pair: %v\n", err)
			os.Exit(1)
		}
	}
}

func processDir(dir, outdir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	pkgFiles := map[string][]parsedFile{}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mx") {
			continue
		}
		if strings.HasSuffix(e.Name(), "_test.mx") {
			continue
		}

		path := filepath.Join(dir, e.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}

		origSrc := append([]byte(nil), src...)

		rewritten := mxtext.RewriteChanLiterals(src, fset)
		rewritten = mxtext.RewriteSliceLiterals(rewritten, fset)

		f, err := parser.ParseFile(fset, e.Name(), rewritten, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}

		pf := parsedFile{
			name:    e.Name(),
			origSrc: origSrc,
			src:     rewritten,
			file:    f,
		}
		pkgFiles[f.Name.Name] = append(pkgFiles[f.Name.Name], pf)
	}

	dirBase := filepath.Base(dir)
	var result []string

	for pkgName, files := range pkgFiles {
		outName := dirBase
		if pkgName != dirBase && pkgName != "main" {
			outName = pkgName
		} else if pkgName == "main" && len(pkgFiles) > 1 {
			outName = dirBase + "_gen"
		}

		pkgDir := filepath.Join(outdir, outName)
		manifest := &PackageManifest{
			Package: pkgName,
		}

		for fileIdx, pf := range files {
			fm, err := extractAST(fset, pf, fileIdx+1, pkgDir)
			if err != nil {
				return nil, fmt.Errorf("ast %s: %w", pf.name, err)
			}
			manifest.Files = append(manifest.Files, fm)
		}

		manifestPath := filepath.Join(pkgDir, "manifest.json")
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(manifestPath, data, 0644); err != nil {
			return nil, err
		}

		n := 0
		for _, f := range manifest.Files {
			n += len(f.Segments)
		}
		fmt.Printf("%s: %d files, %d segments\n", outName, len(manifest.Files), n)

		if flagVerify {
			if err := verifyConcat(files, pkgDir); err != nil {
				return nil, err
			}
		}
		result = append(result, pkgDir)
	}

	return result, nil
}

func verifyConcat(files []parsedFile, pkgDir string) error {
	for _, pf := range files {
		segDir := filepath.Join(pkgDir, pf.name+".segments")
		entries, err := os.ReadDir(segDir)
		if err != nil {
			return fmt.Errorf("verify %s: %w", pf.name, err)
		}

		var names []string
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".mx") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)

		var concat []byte
		for _, n := range names {
			data, err := os.ReadFile(filepath.Join(segDir, n))
			if err != nil {
				return err
			}
			concat = append(concat, data...)
		}

		if string(concat) != string(pf.src) {
			return fmt.Errorf("verify %s: segment concat (%d bytes) != rewritten source (%d bytes)", pf.name, len(concat), len(pf.src))
		}
		fmt.Printf("  verify %s: PASS (%d bytes)\n", pf.name, len(pf.src))
	}
	return nil
}
