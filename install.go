package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"moxie/builder"
	"moxie/cache"
	"moxie/compileopts"
	"moxie/goenv"
)

// Install fetches (if remote), compiles, and caches a package binary + .mxh.
// pkgName is either a local directory path or a remote import path (e.g.
// "git.smesh.lol/iskradb"). The URL is passed as-is to git; protocol
// detection is left to git itself.
func Install(pkgName string, options *compileopts.Options) error {
	config, err := builder.NewConfig(options)
	if err != nil {
		return err
	}

	importPath := pkgName
	localDir := pkgName

	// Remote package: import path contains a dot before the first slash.
	firstSeg := strings.SplitN(pkgName, "/", 2)[0]
	isRemote := strings.Contains(firstSeg, ".") && !isDir(pkgName)

	if isRemote {
		importPath = pkgName
		localDir = cache.SourcePath(importPath)
		if err := fetchOrUpdate(pkgName, localDir); err != nil {
			return err
		}
	} else {
		// Local directory: derive import path from directory name for cache key.
		abs, err := filepath.Abs(localDir)
		if err != nil {
			return err
		}
		importPath = filepath.Base(abs)
		localDir = abs
	}

	triple := config.Triple()

	// Check cache hit: reuse if source hash + compiler version + triple match.
	srcHash, err := cache.SourceHash(localDir)
	if err != nil {
		return fmt.Errorf("install: hashing source: %w", err)
	}
	compilerVer := goenv.Version()

	if meta, err := cache.ReadMeta(triple, importPath); err == nil {
		if meta.SourceHash == srcHash && meta.CompilerVersion == compilerVer && meta.Triple == triple {
			fmt.Printf("install: %s is up to date\n", importPath)
			return nil
		}
	}

	// Build the binary.
	binName := filepath.Base(importPath)
	binPath := cache.BinPath(triple, importPath, binName)
	if err := os.MkdirAll(filepath.Dir(binPath), 0755); err != nil {
		return err
	}
	tmpdir, err := os.MkdirTemp("", "moxie-install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpdir)

	_, err = builder.Build(localDir, binPath, tmpdir, config)
	if err != nil {
		return fmt.Errorf("install: build: %w", err)
	}

	// Extract .mxh (target-independent).
	mxhDir := filepath.Dir(cache.MXHPath(importPath))
	if err := os.MkdirAll(mxhDir, 0755); err != nil {
		return err
	}
	if err := Header(localDir, mxhDir, "", options); err != nil {
		return fmt.Errorf("install: extracting header: %w", err)
	}
	// Header writes <pkgname>.mxh but we want it at <importpath>.mxh.
	// Move if the names differ.
	pkgBase := filepath.Base(localDir)
	emitted := filepath.Join(mxhDir, pkgBase+".mxh")
	wanted := cache.MXHPath(importPath)
	if emitted != wanted {
		if err := os.Rename(emitted, wanted); err != nil {
			return fmt.Errorf("install: moving .mxh: %w", err)
		}
	}

	// Write metadata.
	return cache.WriteMeta(triple, importPath, &cache.Meta{
		SourceHash:      srcHash,
		CompilerVersion: compilerVer,
		Triple:          triple,
		InstalledAt:     time.Now(),
	})
}

// fetchOrUpdate clones or pulls a remote package into localDir.
// The URL is passed as-is to git, allowing git to detect the protocol
// (SSH, HTTPS, etc.) from its own configuration.
func fetchOrUpdate(importPath, localDir string) error {
	if isDir(filepath.Join(localDir, ".git")) {
		// Already cloned: pull latest.
		cmd := exec.Command("git", "-C", localDir, "pull", "--ff-only")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git pull %s: %w", importPath, err)
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(localDir), 0755); err != nil {
		return err
	}
	cmd := exec.Command("git", "clone", "--depth=1", importPath, localDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone %s: %w", importPath, err)
	}
	return nil
}

// Fetch reads moxie.mod in the current directory, resolves all declared
// dependencies, and runs Install for each. This is the network-fetch step;
// moxie build never touches the network after this.
func Fetch(options *compileopts.Options) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	modFile := findModFileIn(wd)
	if modFile == "" {
		return fmt.Errorf("fetch: no moxie.mod or go.mod found in %s", wd)
	}
	deps, err := readFetchDeps(modFile)
	if err != nil {
		return err
	}
	if len(deps) == 0 {
		fmt.Println("fetch: no external dependencies found")
		return nil
	}
	for _, dep := range deps {
		fmt.Printf("fetch: installing %s\n", dep)
		if err := Install(dep, options); err != nil {
			return fmt.Errorf("fetch %s: %w", dep, err)
		}
	}
	return nil
}

// findModFileIn returns the moxie.mod or go.mod path in dir.
func findModFileIn(dir string) string {
	for _, name := range []string{"moxie.mod", "go.mod"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// readFetchDeps extracts require lines from a mod file that look like
// remote import paths (contain a '.' before the first '/').
func readFetchDeps(modFile string) ([]string, error) {
	data, err := os.ReadFile(modFile)
	if err != nil {
		return nil, err
	}
	var deps []string
	inRequire := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "require (" {
			inRequire = true
			continue
		}
		if inRequire && trimmed == ")" {
			inRequire = false
			continue
		}
		if strings.HasPrefix(trimmed, "require ") {
			trimmed = strings.TrimPrefix(trimmed, "require ")
		} else if !inRequire {
			continue
		}
		// "module/path v1.2.3" or "module/path vX.Y.Z // indirect"
		parts := strings.Fields(trimmed)
		if len(parts) < 2 {
			continue
		}
		importPath := parts[0]
		firstSeg := strings.SplitN(importPath, "/", 2)[0]
		if strings.Contains(firstSeg, ".") {
			deps = append(deps, importPath)
		}
	}
	return deps, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
