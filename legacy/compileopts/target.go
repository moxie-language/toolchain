package compileopts

import (
	"fmt"
	"runtime"
	"strings"
)

// TargetSpec is the target specification for a given target.
type TargetSpec struct {
	Triple           string   `json:"llvm-target,omitempty"`
	CPU              string   `json:"cpu,omitempty"`
	ABI              string   `json:"target-abi,omitempty"`
	Features         string   `json:"features,omitempty"`
	GOOS             string   `json:"goos,omitempty"`
	GOARCH           string   `json:"goarch,omitempty"`
	BuildTags        []string `json:"build-tags,omitempty"`
	BuildMode        string   `json:"buildmode,omitempty"`
	Scheduler        string   `json:"scheduler,omitempty"`
	Linker           string   `json:"linker,omitempty"`
	RTLib            string   `json:"rtlib,omitempty"`
	Libc             string   `json:"libc,omitempty"`
	DefaultStackSize uint64   `json:"default-stack-size,omitempty"`
	CFlags           []string `json:"cflags,omitempty"`
	LDFlags          []string `json:"ldflags,omitempty"`
	LinkerScript     string   `json:"linkerscript,omitempty"`
	ExtraFiles       []string `json:"extra-files,omitempty"`
	BinaryFormat     string   `json:"binary-format,omitempty"`
	CodeModel        string   `json:"code-model,omitempty"`
	RelocationModel  string   `json:"relocation-model,omitempty"`
	Emulator         string   `json:"emulator,omitempty"`
}

// LoadTarget loads a target specification from options.
func LoadTarget(options *Options) (*TargetSpec, error) {
	// Parse compound target like "js/wasm" into GOOS/GOARCH.
	if strings.Contains(options.Target, "/") {
		parts := strings.SplitN(options.Target, "/", 2)
		options.GOOS = parts[0]
		options.GOARCH = parts[1]
		options.Target = ""
	}
	if options.Target == "wasm" {
		options.GOOS = "js"
		options.GOARCH = "wasm"
		options.Target = ""
	}
	return defaultTarget(options)
}

// defaultTarget synthesizes a TargetSpec from GOOS/GOARCH.
func defaultTarget(options *Options) (*TargetSpec, error) {
	spec := TargetSpec{
		GOOS:             options.GOOS,
		GOARCH:           options.GOARCH,
		BuildTags:        []string{options.GOOS, options.GOARCH},
		Linker:           "cc",
		DefaultStackSize: 1024 * 64, // 64kB
	}

	var llvmarch string
	switch options.GOARCH {
	case "amd64":
		llvmarch = "x86_64"
		spec.CPU = "x86-64"
		spec.Features = "+cmov,+cx8,+fxsr,+mmx,+sse,+sse2,+x87"
	case "arm64":
		spec.CPU = "generic"
		llvmarch = "aarch64"
		if options.GOOS == "darwin" {
			spec.Features = "+ete,+fp-armv8,+neon,+trbe,+v8a"
			llvmarch = "arm64"
		} else {
			spec.Features = "+ete,+fp-armv8,+neon,+trbe,+v8a,-fmv,-outline-atomics"
		}
	case "wasm":
		if options.GOOS != "js" {
			return nil, fmt.Errorf("GOARCH=wasm requires GOOS=js")
		}
		llvmarch = "wasm32"
		spec.CPU = "generic"
		spec.Features = "+mutable-globals,+sign-ext"
	default:
		return nil, fmt.Errorf("unsupported GOARCH=%s (moxie supports amd64, arm64, wasm)", options.GOARCH)
	}

	// Configure target based on GOOS.
	llvmos := options.GOOS
	llvmvendor := "unknown"
	switch options.GOOS {
	case "darwin":
		platformVersion := "10.12.0"
		if options.GOARCH == "arm64" {
			platformVersion = "11.0.0"
		}
		llvmvendor = "apple"
		spec.Scheduler = "none" // moxie: no goroutines
		spec.Linker = "ld.lld"
		spec.Libc = "darwin-libSystem"
		llvmos = "macosx" + platformVersion
		spec.LDFlags = append(spec.LDFlags,
			"-flavor", "darwin",
			"-dead_strip",
			"-arch", llvmarch,
			"-platform_version", "macos", platformVersion, platformVersion,
		)
		spec.ExtraFiles = append(spec.ExtraFiles,
			"src/runtime/os_darwin.c",
			"src/runtime/runtime_unix.c",
			"src/runtime/secalloc.c",
			"src/runtime/signal.c",
			"src/runtime/spawn_unix.c",
			"src/runtime/futex_darwin.c")
	case "linux":
		spec.Scheduler = "none" // moxie: no goroutines
		spec.Linker = "ld.lld"
		spec.RTLib = "compiler-rt"
		spec.Libc = "musl"
		spec.LDFlags = append(spec.LDFlags, "--gc-sections")
		if options.GOARCH == "arm64" {
			spec.CFlags = append(spec.CFlags, "-mno-outline-atomics")
		}
		spec.ExtraFiles = append(spec.ExtraFiles,
			"src/runtime/runtime_unix.c",
			"src/runtime/secalloc.c",
			"src/runtime/signal.c",
			"src/runtime/spawn_unix.c",
			"src/runtime/thread_unix.c", // domains are threads (pthread_create)
			"src/runtime/futex_linux.c",
			"src/runtime/ffi_linux.c")
	case "js":
		spec.Scheduler = "none" // moxie: no goroutines
		spec.Linker = "wasm-ld"
		spec.BuildTags = append(spec.BuildTags, "moxie.wasm", "moxie.unicore")
		spec.LDFlags = append(spec.LDFlags,
			"--export-dynamic",
			"--allow-undefined",
			"--gc-sections",
		)
	default:
		return nil, fmt.Errorf("unsupported GOOS=%s (moxie supports linux, darwin, js)", options.GOOS)
	}

	// Target triple: arch-vendor-os[-environment]
	spec.Triple = llvmarch + "-" + llvmvendor + "-" + llvmos
	if options.GOOS == "linux" {
		spec.Triple += "-musleabihf"
	}

	// Assembly files (not for WASM).
	if options.GOARCH != "wasm" {
		spec.ExtraFiles = append(spec.ExtraFiles,
			"src/runtime/asm_"+options.GOARCH+".S")
	}

	// Cross-compilation emulator.
	if options.GOARCH != runtime.GOARCH && options.GOOS == "linux" {
		switch options.GOARCH {
		case "amd64":
			spec.Emulator = "qemu-x86_64 {}"
		case "arm64":
			spec.Emulator = "qemu-aarch64 {}"
		}
	}

	return &spec, nil
}

// LookupGDB looks up a gdb executable. Returns empty string if not needed.
func (spec *TargetSpec) LookupGDB() string {
	return ""
}

// GetTargetSpecs returns available target specs. Moxie has no JSON target files.
func GetTargetSpecs() map[string]*TargetSpec {
	goos := []string{"linux", "darwin"}
	goarch := []string{"amd64", "arm64"}
	specs := make(map[string]*TargetSpec)
	for _, os := range goos {
		for _, arch := range goarch {
			name := os + "/" + arch
			t, err := defaultTarget(&Options{GOOS: os, GOARCH: arch})
			if err == nil {
				specs[name] = t
			}
		}
	}
	// Add js/wasm.
	t, err := defaultTarget(&Options{GOOS: "js", GOARCH: "wasm"})
	if err == nil {
		specs["js/wasm"] = t
	}
	return specs
}

// Triple returns a cleaned version for display.
func (spec *TargetSpec) TripleForDisplay() string {
	return strings.TrimSpace(spec.Triple)
}
