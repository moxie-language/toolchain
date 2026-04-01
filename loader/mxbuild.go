// mxbuild.go provides a custom go/build.Context that discovers .mx source
// files by presenting them to go/build as .go files. This lets us reuse
// go/build's constraint matching, import resolution, and file classification
// without vendoring or modifying the standard library.
//
// The trick: ReadDir renames .mx → .go in directory listings, and OpenFile
// redirects .go reads to the corresponding .mx file on disk.

package loader

import (
	"go/build"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// mxContext returns a go/build.Context configured for the given target
// that transparently finds .mx files wherever go/build expects .go files.
func mxContext(goroot, goos, goarch string, buildTags []string) build.Context {
	ctx := build.Default
	ctx.GOROOT = goroot
	ctx.GOOS = goos
	ctx.GOARCH = goarch
	ctx.BuildTags = buildTags
	ctx.CgoEnabled = true
	ctx.Compiler = "gc"

	ctx.ReadDir = mxReadDir
	ctx.OpenFile = mxOpenFile

	return ctx
}

// mxReadDir reads a directory and presents .mx files as .go files.
// Actual .go files are passed through unchanged (for external deps
// in the module cache that remain .go).
func mxReadDir(dir string) ([]fs.FileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	// Track .mx base names so we can suppress duplicate .go files
	// if both foo.mx and foo.go somehow exist in the same directory.
	mxBases := make(map[string]bool)
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".mx") {
			mxBases[strings.TrimSuffix(name, ".mx")] = true
		}
	}

	infos := make([]fs.FileInfo, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		info, err := e.Info()
		if err != nil {
			return nil, err
		}

		if strings.HasSuffix(name, ".mx") {
			// Present .mx as .go so go/build's matchFile accepts it.
			infos = append(infos, renamedFileInfo{
				FileInfo: info,
				name:     strings.TrimSuffix(name, ".mx") + ".go",
			})
		} else if strings.HasSuffix(name, ".go") && mxBases[strings.TrimSuffix(name, ".go")] {
			// Skip .go if a corresponding .mx exists — .mx wins.
			continue
		} else {
			infos = append(infos, info)
		}
	}
	return infos, nil
}

// mxOpenFile opens a file, redirecting .go requests to .mx when the .mx
// file exists on disk. For files in the module cache (external deps) where
// only .go exists, opens the .go file directly.
func mxOpenFile(path string) (io.ReadCloser, error) {
	if strings.HasSuffix(path, ".go") {
		mxPath := strings.TrimSuffix(path, ".go") + ".mx"
		if f, err := os.Open(mxPath); err == nil {
			return f, nil
		}
	}
	return os.Open(path)
}

// renamedFileInfo wraps fs.FileInfo with an overridden Name().
type renamedFileInfo struct {
	fs.FileInfo
	name string
}

func (r renamedFileInfo) Name() string { return r.name }

// mxFileInfo is a minimal fs.FileInfo for synthetic entries.
type mxFileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	isDir   bool
}

func (fi mxFileInfo) Name() string      { return fi.name }
func (fi mxFileInfo) Size() int64       { return fi.size }
func (fi mxFileInfo) Mode() fs.FileMode { return fi.mode }
func (fi mxFileInfo) ModTime() time.Time { return fi.modTime }
func (fi mxFileInfo) IsDir() bool       { return fi.isDir }
func (fi mxFileInfo) Sys() any          { return nil }

// goToMx maps a list of .go filenames back to .mx where the .mx file
// exists on disk. Files that are genuinely .go (external deps) stay as-is.
func goToMx(files []string, dir string) []string {
	out := make([]string, len(files))
	for i, f := range files {
		if strings.HasSuffix(f, ".go") {
			mxName := strings.TrimSuffix(f, ".go") + ".mx"
			if _, err := os.Stat(filepath.Join(dir, mxName)); err == nil {
				out[i] = mxName
				continue
			}
		}
		out[i] = f
	}
	return out
}
