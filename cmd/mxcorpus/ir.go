package main

import (
	"encoding/json"
	"fmt"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"moxie/compiler"
	"moxie/compileopts"
	"moxie/goenv"
	"moxie/loader"

	llvm "tinygo.org/x/go-llvm"
)

var optLevels = []string{"0", "1", "2", "s"}

// extractIR compiles a package at all four optimization levels and extracts
// per-function IR. Uses the compiler directly — no linking, no DCE.
func extractIR(pkgName, outdir string) error {
	wrapDir, err := os.MkdirTemp("", "mxcorpus-wrap-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(wrapDir)

	wrapMain := filepath.Join(wrapDir, "main.mx")
	wrapMod := filepath.Join(wrapDir, "moxie.mod")

	mainSrc := fmt.Sprintf("package main\n\nimport _ %q\n\nfunc main() {}\n", pkgName)
	if err := os.WriteFile(wrapMain, []byte(mainSrc), 0644); err != nil {
		return err
	}

	var modSrc string
	if flagModDir != "" {
		modFile := filepath.Join(flagModDir, "moxie.mod")
		modData, err := os.ReadFile(modFile)
		if err != nil {
			return fmt.Errorf("read %s: %w", modFile, err)
		}
		var modName string
		for _, line := range strings.Split(string(modData), "\n") {
			if strings.HasPrefix(line, "module ") {
				modName = strings.TrimSpace(line[7:])
				break
			}
		}
		if modName == "" {
			return fmt.Errorf("no module name in %s", modFile)
		}
		modSrc = fmt.Sprintf("module mxcorpus_wrap\n\ngo 1.25.0\n\nreplace %s => %s\n", modName, flagModDir)
	} else {
		moxieRoot := goenv.Get("MOXIEROOT")
		modSrc = fmt.Sprintf("module mxcorpus_wrap\n\ngo 1.25.0\n\nreplace %s => %s/src/%s\n", pkgName, moxieRoot, pkgName)
	}
	if err := os.WriteFile(wrapMod, []byte(modSrc), 0644); err != nil {
		return err
	}

	baseIR, err := compilePackageIR(wrapDir, pkgName)
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}

	symbols := map[string]map[string]string{}
	for _, opt := range optLevels {
		label := optLabel(opt)
		nFuncs, funcs, err := compileAndExtractIR(opt, label, outdir, baseIR)
		if err != nil {
			return fmt.Errorf("ir opt=%s: %w", opt, err)
		}
		fmt.Printf("  IR %s: %d functions\n", label, nFuncs)
		m := map[string]string{}
		for _, name := range funcs {
			m[name] = sanitizeIRName(name)
		}
		symbols[label] = m
	}

	symData, err := json.MarshalIndent(symbols, "", "  ")
	if err != nil {
		return fmt.Errorf("symbols json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outdir, "symbols.json"), symData, 0644); err != nil {
		return err
	}
	fmt.Printf("  SYMBOLS: %d opt levels\n", len(symbols))
	return nil
}

// compilePackageIR loads the program, compiles the target package, and returns
// unoptimized IR. Called once; the result is then optimized at each opt level.
func compilePackageIR(wrapDir, targetPkg string) ([]byte, error) {
	config, err := makeConfig("0", wrapDir)
	if err != nil {
		return nil, err
	}

	compilerConfig := &compiler.Config{
		Triple:           config.Triple(),
		CPU:              config.CPU(),
		Features:         config.Features(),
		ABI:              config.ABI(),
		GOOS:             config.GOOS(),
		GOARCH:           config.GOARCH(),
		BuildMode:        config.BuildMode(),
		CodeModel:        config.CodeModel(),
		RelocationModel:  config.RelocationModel(),
		MoxieVersion:     goenv.Version(),
		Scheduler:        config.Scheduler(),
		DefaultStackSize: config.StackSize(),
		MaxStackAlloc:    config.MaxStackAlloc(),
		NeedsStackObjects: config.NeedsStackObjects(),
		Debug:            true,
		PanicStrategy:    config.PanicStrategy(),
	}

	machine, err := compiler.NewTargetMachine(compilerConfig)
	if err != nil {
		return nil, err
	}
	defer machine.Dispose()

	lprogram, err := loader.Load(config, ".", types.Config{
		Sizes: compiler.Sizes(machine),
	})
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	if err := lprogram.Parse(); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	program := lprogram.LoadSSA()

	var targetLoaderPkg *loader.Package
	for _, pkg := range lprogram.Sorted() {
		if pkg.ImportPath == targetPkg {
			targetLoaderPkg = pkg
			break
		}
	}
	if targetLoaderPkg == nil {
		return nil, fmt.Errorf("package %q not found in program", targetPkg)
	}

	mod, errs := compiler.CompilePackage(
		targetLoaderPkg.ImportPath,
		targetLoaderPkg,
		program.Package(targetLoaderPkg.Pkg),
		machine,
		compilerConfig,
		false,
	)
	if errs != nil {
		return nil, fmt.Errorf("compile: %v", errs)
	}
	defer mod.Dispose()

	if err := llvm.VerifyModule(mod, llvm.PrintMessageAction); err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}

	return []byte(mod.String()), nil
}

func compileAndExtractIR(opt, label, outdir string, baseIR []byte) (int, []string, error) {
	if err := os.MkdirAll(outdir, 0755); err != nil {
		return 0, nil, err
	}

	var irData []byte
	if opt == "0" {
		irData = baseIR
	} else {
		optFlag := "-O" + opt
		cmd := exec.Command("opt", "-S", "-passes=default<"+optFlag[1:]+">", "-o", "-")
		cmd.Stdin = strings.NewReader(string(baseIR))
		out, err := cmd.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return 0, nil, fmt.Errorf("opt %s: %s", optFlag, string(exitErr.Stderr))
			}
			return 0, nil, fmt.Errorf("opt %s: %w", optFlag, err)
		}
		irData = out
	}

	modFilename := "module.ll"
	if label != "" {
		modFilename = "module." + label + ".ll"
	}
	if err := os.WriteFile(filepath.Join(outdir, modFilename), irData, 0644); err != nil {
		return 0, nil, err
	}

	return extractFunctionIR(irData, label, outdir)
}

func extractFunctionIR(irData []byte, label, outdir string) (int, []string, error) {
	segDir := filepath.Join(outdir, "ir")
	if err := os.MkdirAll(segDir, 0755); err != nil {
		return 0, nil, err
	}

	n := 0
	var names []string
	lines := strings.Split(string(irData), "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]
		if !strings.HasPrefix(line, "define ") {
			i++
			continue
		}

		name := extractFuncName(line)
		start := i
		braceDepth := 0
		for i < len(lines) {
			for _, ch := range lines[i] {
				if ch == '{' {
					braceDepth++
				} else if ch == '}' {
					braceDepth--
				}
			}
			i++
			if braceDepth == 0 {
				break
			}
		}

		if name == "" {
			continue
		}

		funcIR := strings.Join(lines[start:i], "\n") + "\n"
		safeName := sanitizeIRName(name)
		filename := safeName + ".ll"
		if label != "" {
			filename = safeName + "." + label + ".ll"
		}

		if err := os.WriteFile(filepath.Join(segDir, filename), []byte(funcIR), 0644); err != nil {
			return 0, nil, err
		}
		names = append(names, name)
		n++
	}
	return n, names, nil
}

func extractFuncName(line string) string {
	atIdx := strings.Index(line, "@")
	if atIdx < 0 {
		return ""
	}
	rest := line[atIdx+1:]
	// Quoted name: @"(*pkg.T).Method"(args...)
	if len(rest) > 0 && rest[0] == '"' {
		endQuote := strings.Index(rest[1:], "\"")
		if endQuote < 0 {
			return ""
		}
		return rest[1 : endQuote+1]
	}
	parenIdx := strings.Index(rest, "(")
	if parenIdx < 0 {
		return ""
	}
	return rest[:parenIdx]
}

func sanitizeIRName(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "*", "_")
	name = strings.ReplaceAll(name, "(", "")
	name = strings.ReplaceAll(name, ")", "")
	name = strings.ReplaceAll(name, ".", "_")
	return name
}

func optLabel(opt string) string {
	switch opt {
	case "0":
		return "O0"
	case "1":
		return "O1"
	case "2":
		return "O2"
	case "s":
		return "Os"
	}
	return "O" + opt
}

func makeConfig(opt, dir string) (*compileopts.Config, error) {
	options := &compileopts.Options{
		GOOS:          goenv.Get("GOOS"),
		GOARCH:        goenv.Get("GOARCH"),
		Opt:           opt,
		Debug:         true,
		Directory:     dir,
		InterpTimeout: 180 * time.Second,
		Semaphore:     make(chan struct{}, runtime.GOMAXPROCS(0)),
	}

	spec, err := compileopts.LoadTarget(options)
	if err != nil {
		return nil, err
	}

	_, gorootMinor, err := goenv.GetGorootVersion()
	if err != nil {
		return nil, err
	}

	return &compileopts.Config{
		Options:        options,
		Target:         spec,
		GoMinorVersion: gorootMinor,
	}, nil
}
