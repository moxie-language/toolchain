// Package compileopts contains the configuration for a single to-be-built binary.
package compileopts

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"moxie/goenv"
)

var libVersions = map[string]int{
	"musl": 3,
}

// Config keeps all configuration affecting the build in a single struct.
type Config struct {
	Options        *Options
	Target         *TargetSpec
	GoMinorVersion int
	TestConfig     TestConfig
}

func (c *Config) Triple() string    { return c.Target.Triple }
func (c *Config) CPU() string       { return c.Target.CPU }
func (c *Config) ABI() string       { return c.Target.ABI }
func (c *Config) GOOS() string      { return c.Target.GOOS }
func (c *Config) GOARCH() string    { return c.Target.GOARCH }
func (c *Config) DumpSSA() bool     { return c.Options.DumpSSA }
func (c *Config) VerifyIR() bool    { return c.Options.VerifyIR }
func (c *Config) Debug() bool       { return c.Options.Debug }
func (c *Config) ExtraFiles() []string { return c.Target.ExtraFiles }

func (c *Config) BuildMode() string {
	if c.Options.BuildMode != "" {
		return c.Options.BuildMode
	}
	if c.Target.BuildMode != "" {
		return c.Target.BuildMode
	}
	return "default"
}

func (c *Config) Features() string {
	if c.Target.Features == "" {
		return c.Options.LLVMFeatures
	}
	if c.Options.LLVMFeatures == "" {
		return c.Target.Features
	}
	return c.Target.Features + "," + c.Options.LLVMFeatures
}

// BuildTags returns the complete list of build tags used during this build.
func (c *Config) BuildTags() []string {
	tags := append([]string(nil), c.Target.BuildTags...)
	tags = append(tags,
		"moxie",
		"purego",
		"osusergo",
		"math_big_pure_go",
		"gc."+c.GC(),
		"scheduler."+c.Scheduler(),
		"moxie.unicore", // always single-core cooperative
	)
	for i := 1; i <= c.GoMinorVersion; i++ {
		tags = append(tags, fmt.Sprintf("go1.%d", i))
	}
	tags = append(tags, c.Options.Tags...)
	return tags
}

func (c *Config) GC() string {
	if c.Options.GC != "" {
		return c.Options.GC
	}
	if c.Target.GC != "" {
		return c.Target.GC
	}
	return "conservative"
}

func (c *Config) NeedsStackObjects() bool {
	return false
}

// Scheduler returns the scheduler implementation. Moxie uses "none" (no goroutines).
func (c *Config) Scheduler() string {
	if c.Options.Scheduler != "" {
		return c.Options.Scheduler
	}
	if c.Target.Scheduler != "" {
		return c.Target.Scheduler
	}
	return "none"
}

func (c *Config) OptLevel() (level string, speedLevel, sizeLevel int) {
	switch c.Options.Opt {
	case "none", "0":
		return "O0", 0, 0
	case "1":
		return "O1", 1, 0
	case "2":
		return "O2", 2, 0
	case "s":
		return "Os", 2, 1
	case "z":
		return "Oz", 2, 2
	default:
		panic("unknown optimization level: -opt=" + c.Options.Opt)
	}
}

func (c *Config) PanicStrategy() string { return c.Options.PanicStrategy }

func (c *Config) StackSize() uint64 {
	if c.Options.StackSize != 0 {
		return c.Options.StackSize
	}
	return c.Target.DefaultStackSize
}

func (c *Config) MaxStackAlloc() uint64 {
	if c.StackSize() >= 16*1024 {
		return 1024
	}
	return 256
}

func CanonicalArchName(triple string) string {
	arch := strings.Split(triple, "-")[0]
	if arch == "arm64" {
		return "aarch64"
	}
	return arch
}

func MuslArchitecture(triple string) string {
	return CanonicalArchName(triple)
}

func (c *Config) LibraryPath(name string) string {
	archname := c.Triple()
	if c.CPU() != "" {
		archname += "-" + c.CPU()
	}
	if c.ABI() != "" {
		archname += "-" + c.ABI()
	}
	if c.BuildMode() == "c-shared" {
		archname += "-pic"
	}
	if v, ok := libVersions[name]; ok {
		archname += "-v" + strconv.Itoa(v)
	}
	return filepath.Join(goenv.Get("GOCACHE"), name+"-"+archname)
}

func (c *Config) DefaultBinaryExtension() string {
	parts := strings.Split(c.Triple(), "-")
	if parts[0] == "wasm32" {
		return ".wasm"
	}
	return ""
}

// CFlags returns the flags to pass to the C compiler.
func (c *Config) CFlags() []string {
	var cflags []string
	for _, flag := range c.Target.CFlags {
		cflags = append(cflags, strings.ReplaceAll(flag, "{root}", goenv.Get("MOXIEROOT")))
	}
	resourceDir := goenv.ClangResourceDir()
	if resourceDir != "" {
		cflags = append(cflags, "-resource-dir="+resourceDir)
	}
	cflags = append(cflags, c.LibcCFlags()...)
	cflags = append(cflags, "-gdwarf-4")
	cflags = append(cflags, "-O"+c.Options.Opt)
	cflags = append(cflags, "--target="+c.Triple())
	if c.Target.CPU != "" {
		if c.GOARCH() == "amd64" {
			cflags = append(cflags, "-march="+c.Target.CPU)
		} else {
			cflags = append(cflags, "-mcpu="+c.Target.CPU)
		}
	}
	if c.ABI() != "" {
		cflags = append(cflags, "-mabi="+c.ABI())
	}
	if c.BuildMode() == "c-shared" {
		cflags = append(cflags, "-fPIC")
	}
	return cflags
}

func (c *Config) LibcCFlags() []string {
	switch c.Target.Libc {
	case "darwin-libSystem":
		root := goenv.Get("MOXIEROOT")
		return []string{
			"-nostdlibinc",
			"-isystem", filepath.Join(root, "lib/macos-minimal-sdk/src/usr/include"),
		}
	case "musl":
		root := goenv.Get("MOXIEROOT")
		path := c.LibraryPath("musl")
		arch := MuslArchitecture(c.Triple())
		return []string{
			"-nostdlibinc",
			"-isystem", filepath.Join(path, "include"),
			"-isystem", filepath.Join(root, "lib", "musl", "arch", arch),
			"-isystem", filepath.Join(root, "lib", "musl", "arch", "generic"),
			"-isystem", filepath.Join(root, "lib", "musl", "include"),
		}
	case "":
		return nil
	default:
		panic("unknown libc: " + c.Target.Libc)
	}
}

func (c *Config) LDFlags() []string {
	root := goenv.Get("MOXIEROOT")
	var ldflags []string
	for _, flag := range c.Target.LDFlags {
		ldflags = append(ldflags, strings.ReplaceAll(flag, "{root}", root))
	}
	ldflags = append(ldflags, "-L", root)
	if c.Target.LinkerScript != "" {
		ldflags = append(ldflags, "-T", c.Target.LinkerScript)
	}
	ldflags = append(ldflags, c.Options.ExtLDFlags...)
	return ldflags
}

func (c *Config) CodeModel() string {
	if c.Target.CodeModel != "" {
		return c.Target.CodeModel
	}
	return "default"
}

func (c *Config) RelocationModel() string {
	if c.BuildMode() == "c-shared" {
		return "pic"
	}
	if c.Target.RelocationModel != "" {
		return c.Target.RelocationModel
	}
	return "static"
}

// Emulator returns the emulator command, or empty if none configured.
func (c *Config) Emulator() string {
	return c.Target.Emulator
}

type TestConfig struct {
	CompileTestBinary bool
	CompileOnly       bool
	Verbose           bool
	Short             bool
	RunRegexp         string
	SkipRegexp        string
	Count             *int
	BenchRegexp       string
	BenchTime         string
	BenchMem          bool
	Shuffle           string
}
