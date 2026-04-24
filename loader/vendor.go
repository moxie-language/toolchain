package loader

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Vendor copies all required dependencies into the vendor/ directory
// under the module root. It reads requires and replaces from the module
// file, resolves each to the module cache, and copies the source files.
// A vendor/modules.txt is written for compatibility with Go tooling.
// A vendor/moxie.sum is written with SHA256 hashes of each vendored file.
func Vendor(dir string) error {
	modPath, modDir, _, err := readModInfo(dir)
	if err != nil {
		return fmt.Errorf("vendor: %w", err)
	}
	if modPath == "" {
		return fmt.Errorf("vendor: no module file found in %s or parents", dir)
	}

	modFile := findModFile(modDir)
	requires, err := readModRequires(modFile)
	if err != nil {
		return fmt.Errorf("vendor: %w", err)
	}
	replaces, err := readModReplaces(modFile, modDir)
	if err != nil {
		return fmt.Errorf("vendor: %w", err)
	}

	if len(requires) == 0 && len(replaces) == 0 {
		fmt.Println("no dependencies to vendor")
		return nil
	}

	modCache := gomodcache()

	vendorDir := filepath.Join(modDir, "vendor")
	if err := os.RemoveAll(vendorDir); err != nil {
		return fmt.Errorf("vendor: cleaning vendor dir: %w", err)
	}
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		return fmt.Errorf("vendor: creating vendor dir: %w", err)
	}

	var modulesTxt strings.Builder
	var sumLines []string

	mods := make([]string, 0, len(requires))
	for mod := range requires {
		mods = append(mods, mod)
	}
	sort.Strings(mods)

	for _, mod := range mods {
		ver := requires[mod]

		var srcDir string
		if localDir, ok := replaces[mod]; ok {
			srcDir = localDir
		} else {
			srcDir = filepath.Join(modCache, mod+"@"+ver)
		}

		if !isDir(srcDir) {
			return fmt.Errorf("vendor: source not found for %s@%s at %s", mod, ver, srcDir)
		}

		dstDir := filepath.Join(vendorDir, mod)

		hashes, err := copyTree(srcDir, dstDir)
		if err != nil {
			return fmt.Errorf("vendor: copying %s: %w", mod, err)
		}

		modulesTxt.WriteString(fmt.Sprintf("# %s %s\n## explicit\n", mod, ver))

		for _, h := range hashes {
			sumLines = append(sumLines, fmt.Sprintf("%s %s", h.hash, h.path))
		}
	}

	if err := os.WriteFile(filepath.Join(vendorDir, "modules.txt"), []byte(modulesTxt.String()), 0644); err != nil {
		return fmt.Errorf("vendor: writing modules.txt: %w", err)
	}

	sort.Strings(sumLines)
	sumContent := strings.Join(sumLines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(vendorDir, "moxie.sum"), []byte(sumContent), 0644); err != nil {
		return fmt.Errorf("vendor: writing moxie.sum: %w", err)
	}

	fmt.Printf("vendored %d modules into %s\n", len(mods), vendorDir)
	return nil
}

type fileHash struct {
	path string
	hash string
}

func copyTree(src, dst string) ([]fileHash, error) {
	var hashes []fileHash
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || base == ".git" || base[0] == '.' {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0755)
		}

		base := filepath.Base(path)
		if base[0] == '.' || base[0] == '_' {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			return err
		}

		h := sha256.Sum256(data)
		hashes = append(hashes, fileHash{
			path: filepath.Join(filepath.Base(dst), rel),
			hash: "sha256:" + hex.EncodeToString(h[:]),
		})
		return nil
	})
	return hashes, err
}
