// mxbuild.go provides a custom go/build.Context that discovers .mx source
// files by presenting them to go/build as .go files. This lets us reuse
// go/build's constraint matching, import resolution, and file classification
// without vendoring or modifying the standard library.
//
// The trick: ReadDir renames .mx → .go in directory listings, and OpenFile
// redirects .go reads to the corresponding .mx file on disk.

package loader

import (
	"bytes"
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
	ctx.CgoEnabled = false
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
		if data, err := os.ReadFile(mxPath); err == nil {
			rewritten := rewriteBuildDirectives(data)
			rewritten = rewriteMoxiePragmas(rewritten)
			rewritten = rewritePkgMainToInit(rewritten)
			return io.NopCloser(bytes.NewReader(rewritten)), nil
		}
	}
	return os.Open(path)
}

// rewritePkgMainToInit renames "func main()" to "func init()" in
// non-main packages. Moxie uses func main() for package initialization
// but Go's SSA builder only recognizes init().
func rewritePkgMainToInit(data []byte) []byte {
	// Check if this is the main package — don't rewrite main.main.
	if isMainPackage(data) {
		return data
	}
	// Replace "func main()" declarations with "func init()".
	return bytes.ReplaceAll(data, []byte("func main()"), []byte("func init()"))
}

// isMainPackage returns true if the source declares "package main".
func isMainPackage(data []byte) bool {
	// Scan past comments and blank lines to find the package declaration.
	rest := data
	for len(rest) > 0 {
		// Skip whitespace/newlines.
		i := 0
		for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t' || rest[i] == '\n' || rest[i] == '\r') {
			i++
		}
		rest = rest[i:]
		if len(rest) == 0 {
			break
		}
		// Skip line comments.
		if len(rest) >= 2 && rest[0] == '/' && rest[1] == '/' {
			nl := bytes.IndexByte(rest, '\n')
			if nl < 0 {
				break
			}
			rest = rest[nl+1:]
			continue
		}
		// Skip block comments.
		if len(rest) >= 2 && rest[0] == '/' && rest[1] == '*' {
			end := bytes.Index(rest[2:], []byte("*/"))
			if end < 0 {
				break
			}
			rest = rest[2+end+2:]
			continue
		}
		// Should be the package declaration.
		if bytes.HasPrefix(rest, []byte("package main")) {
			after := rest[len("package main"):]
			if len(after) == 0 || after[0] == '\n' || after[0] == '\r' || after[0] == ' ' || after[0] == '\t' || after[0] == '/' {
				return true
			}
		}
		break
	}
	return false
}

// rewriteMoxiePragmas rewrites //:pragma lines to //go:pragma so that
// the Go compiler toolchain recognizes them (linkname, inline, nobounds,
// extern, noinline, section, wasmimport, etc).
func rewriteMoxiePragmas(data []byte) []byte {
	// Fast path: no //: lines.
	if !bytes.Contains(data, []byte("//:")) {
		return data
	}
	var out []byte
	rest := data
	for len(rest) > 0 {
		nl := bytes.IndexByte(rest, '\n')
		var line []byte
		if nl >= 0 {
			line = rest[:nl+1]
			rest = rest[nl+1:]
		} else {
			line = rest
			rest = nil
		}
		trimmed := bytes.TrimLeft(line, " \t")
		// Rewrite //:<word> to //go:<word> but NOT //:build (already handled)
		// and NOT //: (bare, no word following).
		if len(trimmed) > 3 && trimmed[0] == '/' && trimmed[1] == '/' && trimmed[2] == ':' &&
			trimmed[3] >= 'a' && trimmed[3] <= 'z' && !bytes.HasPrefix(trimmed, []byte("//:build ")) {
			idx := bytes.Index(line, []byte("//:"))
			out = append(out, line[:idx]...)
			out = append(out, "//go:"...)
			out = append(out, line[idx+3:]...)
		} else {
			out = append(out, line...)
		}
	}
	return out
}

// rewriteBuildDirectives rewrites //:build lines to a single //go:build
// line so that go/build can evaluate Moxie build constraints.
// Multiple //:build lines are combined with &&.
// Only scans before the package clause.
func rewriteBuildDirectives(data []byte) []byte {
	// Fast path: no //:build in the file.
	if !bytes.Contains(data[:min(len(data), 512)], []byte("//:build")) {
		return data
	}

	// First pass: collect all //:build constraints and non-directive lines.
	var constraints []string
	type lineInfo struct {
		data        []byte
		isDirective bool
	}
	var lines []lineInfo
	rest := data
	pastPackage := false
	for len(rest) > 0 && !pastPackage {
		nl := bytes.IndexByte(rest, '\n')
		var line []byte
		if nl >= 0 {
			line = rest[:nl+1]
			rest = rest[nl+1:]
		} else {
			line = rest
			rest = nil
		}
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("//:build ")) {
			expr := string(bytes.TrimPrefix(trimmed, []byte("//:build ")))
			constraints = append(constraints, expr)
			lines = append(lines, lineInfo{data: line, isDirective: true})
		} else {
			lines = append(lines, lineInfo{data: line, isDirective: false})
		}
		if bytes.HasPrefix(trimmed, []byte("package ")) {
			pastPackage = true
		}
	}

	// Build output: emit single //go:build line, then non-directive lines.
	var out []byte
	if len(constraints) > 0 {
		combined := constraints[0]
		for _, c := range constraints[1:] {
			combined = "(" + combined + ") && (" + c + ")"
		}
		out = append(out, []byte("//go:build "+combined+"\n")...)
	}
	for _, li := range lines {
		if !li.isDirective {
			out = append(out, li.data...)
		}
	}
	out = append(out, rest...)
	return out
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
