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

// Install fetches (if remote) and caches a package's source + .mxh header.
// pkgName is either a local directory path or a remote import path (e.g.
// "git.smesh.lol/iskradb"). The URL is passed as-is to git; protocol
// detection is left to git itself. Builds compile the cached source like any
// other package - there is no precompiled-binary exception path.
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

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
