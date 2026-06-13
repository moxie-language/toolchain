// Package goenv returns environment variables that are used in various parts of
// the compiler. You can query it manually with the `moxie env` subcommand.
package goenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Keys is a slice of all available environment variable keys.
var Keys = []string{
	"GOOS",
	"GOARCH",
	"GOROOT",
	"GOPATH",
	"GOCACHE",
	"CGO_ENABLED",
	"MOXIEROOT",
}

// Set to true if we're linking statically against LLVM.
var hasBuiltinTools = false

// MOXIEROOT is the path to the final location for checking moxie files. If
// unset (by a -X ldflag), then sourceDir() will fallback to the original build
// directory.
var MOXIEROOT string

// If a particular Clang resource dir must always be used and Moxie can't
// figure out the directory using heuristics, this global can be set using a
// linker flag.
// This is needed for Nix.
var clangResourceDir string

// Variables read from a `moxie env` command invocation.
var goEnvVars struct {
	GOPATH    string
	GOROOT    string
	GOVERSION string
}

var goEnvVarsDone bool
var goEnvVarsErr error

// Make sure goEnvVars is fresh. This can be called multiple times, the first
// time will update all environment variables in goEnvVars.
func readGoEnvVars() error {
	if goEnvVarsDone {
		return goEnvVarsErr
	}
	goEnvVarsDone = true
	func() {
		cmd := exec.Command("go", "env", "-json", "GOPATH", "GOROOT", "GOVERSION")
		output, err := cmd.Output()
		if err != nil {
			// Check for "command not found" error.
			if execErr, ok := err.(*exec.Error); ok {
				goEnvVarsErr = fmt.Errorf("could not find '%s' command: %w", execErr.Name, execErr.Err)
				return
			}
			// It's perhaps a bit ugly to handle this error here, but I couldn't
			// think of a better place further up in the call chain.
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
				if len(exitErr.Stderr) != 0 {
					// The 'go' command exited with an error message. Print that
					// message and exit, so we behave in a similar way.
					os.Stderr.Write(exitErr.Stderr)
					os.Exit(exitErr.ExitCode())
				}
			}
			// Other errors. Not sure whether there are any, but just in case.
			goEnvVarsErr = err
			return
		}
		err = json.Unmarshal(output, &goEnvVars)
		if err != nil {
			// This should never happen if we have a sane Go toolchain
			// installed.
			goEnvVarsErr = fmt.Errorf("unexpected error while unmarshalling `go env` output: %w", err)
		}
	}()

	return goEnvVarsErr
}

// Get returns a single environment variable, possibly calculating it on-demand.
// The empty string is returned for unknown environment variables.
func Get(name string) string {
	switch name {
	case "GOOS":
		goos := os.Getenv("GOOS")
		if goos == "" {
			goos = runtime.GOOS
		}
		return goos
	case "GOARCH":
		if dir := os.Getenv("GOARCH"); dir != "" {
			return dir
		}
		return runtime.GOARCH
	case "GOROOT":
		readGoEnvVars()
		return goEnvVars.GOROOT
	case "GOPATH":
		readGoEnvVars()
		return goEnvVars.GOPATH
	case "GOCACHE":
		dir, err := os.UserCacheDir()
		if err != nil {
			panic("could not find cache dir: " + err.Error())
		}
		return filepath.Join(dir, "moxie")
	case "CGO_ENABLED":
		return "1"
	case "MOXIEROOT":
		return sourceDir()
	case "WASMOPT":
		if s := os.Getenv("WASMOPT"); s != "" {
			return s
		}
		return "wasm-opt"
	default:
		return ""
	}
}

// Return the MOXIEROOT, or exit with an error.
func sourceDir() string {
	root := os.Getenv("MOXIEROOT")
	if root != "" {
		if !isSourceDir(root) {
			fmt.Fprintln(os.Stderr, "error: $MOXIEROOT was not set to the correct root")
			os.Exit(1)
		}
		return root
	}

	if MOXIEROOT != "" {
		if !isSourceDir(MOXIEROOT) {
			fmt.Fprintln(os.Stderr, "error: MOXIEROOT was not set to the correct root")
			os.Exit(1)
		}
		return MOXIEROOT
	}

	// Find root from executable path.
	path, err := os.Executable()
	if err != nil {
		panic("could not get executable path: " + err.Error())
	}
	root = filepath.Dir(filepath.Dir(path))
	if isSourceDir(root) {
		return root
	}

	// Fallback: use the original directory from where it was built.
	_, path, _, _ = runtime.Caller(0)
	root = filepath.Dir(filepath.Dir(path))
	if isSourceDir(root) {
		return root
	}

	fmt.Fprintln(os.Stderr, "error: could not autodetect root directory, set the MOXIEROOT environment variable to override")
	os.Exit(1)
	panic("unreachable")
}

// isSourceDir returns true if the directory looks like a moxie source directory.
func isSourceDir(root string) bool {
	_, err := os.Stat(filepath.Join(root, "src/runtime/internal/sys/zversion.mx"))
	if err != nil {
		_, err = os.Stat(filepath.Join(root, "src/runtime/internal/sys/zversion.go"))
	}
	return err == nil
}

// ClangResourceDir returns the clang resource dir. Uses llvm-config to detect.
func ClangResourceDir() string {
	if clangResourceDir != "" {
		return clangResourceDir
	}

	root := Get("MOXIEROOT")
	releaseHeaderDir := filepath.Join(root, "lib", "clang")
	if _, err := os.Stat(releaseHeaderDir); !errors.Is(err, fs.ErrNotExist) {
		return releaseHeaderDir
	}

	// Try versioned llvm-config first (e.g. llvm-config-19), then fall back
	// to unversioned. The unversioned llvm-config may be a different LLVM
	// version than the one linked into moxie.
	var llvmMajor string
	for _, cfgName := range []string{"llvm-config-19", "llvm-config"} {
		cmd := exec.Command(cfgName, "--version")
		out, err := cmd.Output()
		if err == nil {
			llvmMajor = strings.Split(strings.TrimSpace(string(out)), ".")[0]
			break
		}
	}
	if llvmMajor == "" {
		return ""
	}

	switch runtime.GOOS {
	case "linux":
		// Check versioned install paths (e.g. /usr/lib/llvm19/lib/clang/19).
		for _, base := range []string{
			filepath.Join("/usr/lib/llvm"+llvmMajor, "lib", "clang", llvmMajor),
			filepath.Join("/usr/lib/llvm-"+llvmMajor, "lib", "clang", llvmMajor),
			filepath.Join("/usr/lib/clang", llvmMajor),
		} {
			if _, err := os.Stat(filepath.Join(base, "include", "stdint.h")); err == nil {
				return base
			}
		}
	case "darwin":
		var prefix string
		switch runtime.GOARCH {
		case "amd64":
			prefix = "/usr/local/opt/llvm@" + llvmMajor
		case "arm64":
			prefix = "/opt/homebrew/opt/llvm@" + llvmMajor
		}
		if prefix != "" {
			path := fmt.Sprintf("%s/lib/clang/%s", prefix, llvmMajor)
			if _, err := os.Stat(path + "/include/stdint.h"); err == nil {
				return path
			}
		}
	}

	return ""
}
