// Package cache manages the mxinstall binary and protocol header cache.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"moxie/goenv"
)

// Meta holds installation metadata for a cached binary.
type Meta struct {
	SourceHash      string    `json:"source_hash"`
	CompilerVersion string    `json:"compiler_version"`
	Triple          string    `json:"triple"`
	InstalledAt     time.Time `json:"installed_at"`
}

// Root returns the root of the mxinstall cache.
func Root() string {
	return filepath.Join(goenv.Get("GOCACHE"), "mxinstall")
}

// MXHPath returns the path for the cached .mxh (target-independent).
func MXHPath(importPath string) string {
	return filepath.Join(Root(), "protocols", importPath+".mxh")
}

// MetaPath returns the path for the .meta JSON file.
func MetaPath(triple, importPath string) string {
	return filepath.Join(Root(), triple, importPath, ".meta")
}

// SourcePath returns the path where cloned source lives.
func SourcePath(importPath string) string {
	return filepath.Join(Root(), "src", importPath)
}

// ReadMeta reads the cached metadata for a binary.
func ReadMeta(triple, importPath string) (*Meta, error) {
	data, err := os.ReadFile(MetaPath(triple, importPath))
	if err != nil {
		return nil, err
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("cache meta decode: %w", err)
	}
	return &m, nil
}

// WriteMeta writes the cache metadata.
func WriteMeta(triple, importPath string, m *Meta) error {
	path := MetaPath(triple, importPath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Clean removes the entire mxinstall cache.
func Clean() error {
	return os.RemoveAll(Root())
}

// List returns the import paths of all installed packages.
// Each import path is listed once regardless of how many triples it has.
func List() ([]string, error) {
	seen := make(map[string]bool)
	var out []string

	root := Root()
	// Walk triple dirs to find .meta files.
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, tripleEntry := range entries {
		if !tripleEntry.IsDir() || tripleEntry.Name() == "protocols" || tripleEntry.Name() == "src" {
			continue
		}
		tripleDir := filepath.Join(root, tripleEntry.Name())
		_ = filepath.WalkDir(tripleDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.Name() != ".meta" {
				return nil
			}
			// path = root/triple/import/path/.meta
			rel, _ := filepath.Rel(tripleDir, filepath.Dir(path))
			importPath := filepath.ToSlash(rel)
			if !seen[importPath] {
				seen[importPath] = true
				out = append(out, importPath)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out, nil
}

// SourceHash computes a stable hash of all .mx source files in dir.
// Sorts files before hashing so the result is deterministic.
func SourceHash(dir string) (string, error) {
	var files []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".mx") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)

	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
