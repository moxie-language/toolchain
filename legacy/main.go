package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/shlex"
	"moxie/builder"
	"moxie/cache"
	"moxie/compileopts"
	"moxie/diagnostics"
	"moxie/goenv"
	"moxie/loader"
	"golang.org/x/tools/go/buildutil"
)

type commandError struct {
	Msg  string
	File string
	Err  error
}

func (e *commandError) Error() string {
	return e.Msg + " " + e.File + ": " + e.Err.Error()
}

func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	inf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer inf.Close()
	outf, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0777)
	if err != nil {
		return err
	}
	_, err = io.Copy(outf, inf)
	if err != nil {
		return err
	}
	err = outf.Close()
	if err != nil {
		return err
	}
	return os.Remove(src)
}

func printCommand(cmd string, args ...string) {
	command := append([]string{cmd}, args...)
	for i, arg := range command {
		const specialChars = "~`#$&*()\\|[]{};'\"<>?! "
		if strings.ContainsAny(arg, specialChars) {
			arg = "'" + strings.ReplaceAll(arg, `'`, `'\''`) + "'"
			command[i] = arg
		}
	}
	fmt.Fprintln(os.Stderr, strings.Join(command, " "))
}

// moxieRoot resolves the module root the way every command wants it: MOXIEROOT
// when set, otherwise the conventional install location $HOME/moxie. Falling
// back to the current directory made an unset MOXIEROOT read whatever checkout
// happened to be the cwd. Stage4 resolves it the same way (moxieRoot in
// _mxc_stage4/main.mx).
func moxieRoot() string {
	if root := goenv.Get("MOXIEROOT"); root != "" {
		return root
	}
	home := os.Getenv("HOME")
	if home == "" {
		return "."
	}
	return filepath.Join(home, "moxie")
}

// BuildRuntime compiles the runtime packages into a standalone .bc file
// with external linkage on all symbols.
func BuildRuntime(outpath string, config *compileopts.Config) error {
	if outpath == "" {
		root := moxieRoot()
		outpath = filepath.Join(root, "sysroot", "runtime_go.bc")
	}
	if !strings.HasSuffix(outpath, ".bc") {
		outpath = outpath + ".bc"
	}

	tmpdir, err := os.MkdirTemp("", "moxie-rt-build")
	if err != nil {
		return err
	}
	if !config.Options.Work {
		defer os.RemoveAll(tmpdir)
	}

	// Compile runtime as the "main" package - the loader resolves all
	// runtime dependencies. SkipMainNameCheck + StandaloneRuntime flags
	// bypass the main-package requirement and skip internalization.
	_, err = builder.Build("runtime", outpath, tmpdir, config)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "  -> %s\n", outpath)
	return nil
}

// Build compiles and links the given package and writes it to outpath.
func Build(pkgName, outpath string, config *compileopts.Config) error {
	tmpdir, err := os.MkdirTemp("", "moxie")
	if err != nil {
		return err
	}
	if !config.Options.Work {
		defer os.RemoveAll(tmpdir)
	}

	result, err := builder.Build(pkgName, outpath, tmpdir, config)
	if err != nil {
		return err
	}

	if result.Binary != "" {
		if outpath == "" {
			if strings.HasSuffix(pkgName, ".mx") {
				outpath = filepath.Base(pkgName[:len(pkgName)-3]) + config.DefaultBinaryExtension()
			} else {
				outpath = filepath.Base(result.MainDir) + config.DefaultBinaryExtension()
			}
		}
		if err := moveFile(result.Binary, outpath); err != nil {
			return err
		}
	}
	return nil
}

// Test runs the tests in the given package.
func Test(pkgName string, stdout, stderr io.Writer, options *compileopts.Options, outpath string) (bool, error) {
	options.TestConfig.CompileTestBinary = true
	config, err := builder.NewConfig(options)
	if err != nil {
		return false, err
	}

	testConfig := &options.TestConfig

	var flags []string
	if testConfig.Verbose {
		flags = append(flags, "-test.v")
	}
	if testConfig.Short {
		flags = append(flags, "-test.short")
	}
	if testConfig.RunRegexp != "" {
		flags = append(flags, "-test.run="+testConfig.RunRegexp)
	}
	if testConfig.SkipRegexp != "" {
		flags = append(flags, "-test.skip="+testConfig.SkipRegexp)
	}
	if testConfig.BenchRegexp != "" {
		flags = append(flags, "-test.bench="+testConfig.BenchRegexp)
	}
	if testConfig.BenchTime != "" {
		flags = append(flags, "-test.benchtime="+testConfig.BenchTime)
	}
	if testConfig.BenchMem {
		flags = append(flags, "-test.benchmem")
	}
	if testConfig.Count != nil && *testConfig.Count != 1 {
		flags = append(flags, "-test.count="+strconv.Itoa(*testConfig.Count))
	}
	if testConfig.Shuffle != "" {
		flags = append(flags, "-test.shuffle="+testConfig.Shuffle)
	}

	logToStdout := testConfig.Verbose || testConfig.BenchRegexp != ""
	var buf bytes.Buffer
	var output io.Writer = &buf
	if logToStdout {
		output = os.Stdout
	}

	passed := false
	var duration time.Duration
	result, err := buildAndRun(pkgName, config, output, flags, nil, 0, func(cmd *exec.Cmd, result builder.BuildResult) error {
		if testConfig.CompileOnly || outpath != "" {
			if outpath == "" {
				outpath = filepath.Base(result.MainDir) + ".test"
			}
			return moveFile(result.Binary, outpath)
		}
		if testConfig.CompileOnly {
			passed = true
			return nil
		}
		cmd.Dir = result.MainDir
		start := time.Now()
		err = cmd.Run()
		duration = time.Since(start)
		passed = err == nil
		if !passed && !logToStdout {
			buf.WriteTo(stdout)
		}
		if _, ok := err.(*exec.ExitError); ok {
			return nil
		}
		return err
	})

	if testConfig.CompileOnly {
		return true, err
	}

	importPath := strings.TrimSuffix(result.ImportPath, ".test")
	var w io.Writer = stdout
	if logToStdout {
		w = os.Stdout
	}
	if err, ok := err.(loader.NoTestFilesError); ok {
		fmt.Fprintf(w, "?   \t%s\t[no test files]\n", err.ImportPath)
		return true, nil
	} else if passed {
		fmt.Fprintf(w, "ok  \t%s\t%.3fs\n", importPath, duration.Seconds())
	} else {
		fmt.Fprintf(w, "FAIL\t%s\t%.3fs\n", importPath, duration.Seconds())
	}
	return passed, err
}

// Run compiles and runs the given program.
func Run(pkgName string, options *compileopts.Options, cmdArgs []string) error {
	config, err := builder.NewConfig(options)
	if err != nil {
		return err
	}
	_, err = buildAndRun(pkgName, config, os.Stdout, cmdArgs, nil, 0, func(cmd *exec.Cmd, result builder.BuildResult) error {
		return cmd.Run()
	})
	return err
}

func buildAndRun(pkgName string, config *compileopts.Config, stdout io.Writer, cmdArgs, environmentVars []string, timeout time.Duration, run func(cmd *exec.Cmd, result builder.BuildResult) error) (builder.BuildResult, error) {
	needsEnvInVars := config.GOOS() == "js"
	var args, env []string
	if needsEnvInVars {
		runtimeGlobals := make(map[string]string)
		if len(cmdArgs) != 0 {
			runtimeGlobals["osArgs"] = strings.Join(cmdArgs, "\x00")
		}
		if len(environmentVars) != 0 {
			runtimeGlobals["osEnv"] = strings.Join(environmentVars, "\x00")
		}
		if len(runtimeGlobals) != 0 {
			config.Options.GlobalValues = map[string]map[string]string{
				"runtime": runtimeGlobals,
			}
		}
	} else {
		args = cmdArgs
		env = environmentVars
	}

	tmpdir, err := os.MkdirTemp("", "moxie")
	if err != nil {
		return builder.BuildResult{}, err
	}
	if !config.Options.Work {
		defer os.RemoveAll(tmpdir)
	}

	result, err := builder.Build(pkgName, "", tmpdir, config)
	if err != nil {
		return result, err
	}

	var ctx context.Context
	if timeout != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
		defer cancel()
	}

	name := result.Binary
	if config.Target.Emulator != "" {
		parts := strings.Fields(config.Target.Emulator)
		for i, p := range parts {
			parts[i] = strings.ReplaceAll(p, "{}", result.Binary)
		}
		name = parts[0]
		args = append(parts[1:], args...)
	}

	var cmd *exec.Cmd
	if ctx != nil {
		cmd = exec.CommandContext(ctx, name, args...)
	} else {
		cmd = exec.Command(name, args...)
	}
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdout = stdout
	cmd.Stderr = os.Stderr

	config.Options.Semaphore <- struct{}{}
	defer func() {
		<-config.Options.Semaphore
	}()

	if config.Options.PrintCommands != nil {
		config.Options.PrintCommands(cmd.Path, cmd.Args[1:]...)
	}
	err = run(cmd, result)
	if err != nil {
		if ctx != nil && ctx.Err() == context.DeadlineExceeded {
			fmt.Fprintf(stdout, "--- timeout of %s exceeded, terminating...\n", timeout)
			err = ctx.Err()
		}
		return result, &commandError{"failed to run compiled binary", result.Binary, err}
	}
	return result, nil
}

type globalValuesFlag map[string]map[string]string

func (m globalValuesFlag) String() string { return "pkgpath.Var=value" }

func (m globalValuesFlag) Set(value string) error {
	equalsIndex := strings.IndexByte(value, '=')
	if equalsIndex < 0 {
		return errors.New("expected format pkgpath.Var=value")
	}
	pathAndName := value[:equalsIndex]
	pointIndex := strings.LastIndexByte(pathAndName, '.')
	if pointIndex < 0 {
		return errors.New("expected format pkgpath.Var=value")
	}
	path := pathAndName[:pointIndex]
	name := pathAndName[pointIndex+1:]
	stringValue := value[equalsIndex+1:]
	if m[path] == nil {
		m[path] = make(map[string]string)
	}
	m[path][name] = stringValue
	return nil
}

func parseGoLinkFlag(flagsString string) (map[string]map[string]string, string, error) {
	set := flag.NewFlagSet("link", flag.ExitOnError)
	globalVarValues := make(globalValuesFlag)
	set.Var(globalVarValues, "X", "Set the value of the string variable to the given value.")
	extLDFlags := set.String("extldflags", "", "additional flags to pass to external linker")
	flags, err := shlex.Split(flagsString)
	if err != nil {
		return nil, "", err
	}
	err = set.Parse(flags)
	if err != nil {
		return nil, "", err
	}
	return map[string]map[string]string(globalVarValues), *extLDFlags, nil
}

func getListOfPackages(pkgs []string, options *compileopts.Options) ([]string, error) {
	config, err := builder.NewConfig(options)
	if err != nil {
		return nil, err
	}
	return loader.MxListImportPaths(config, pkgs)
}

func handleCompilerError(err error) {
	if err != nil {
		wd, getwdErr := os.Getwd()
		if getwdErr != nil {
			wd = ""
		}
		diagnostics.CreateDiagnostics(err).WriteTo(os.Stderr, wd)
		os.Exit(1)
	}
}

func printBuildOutput(err error, jsonDiagnostics bool) {
	if err == nil {
		return
	}
	if jsonDiagnostics {
		wd, _ := os.Getwd()
		type jsonDiag struct {
			ImportPath string
			Action     string
			Output     string `json:",omitempty"`
		}
		for _, diags := range diagnostics.CreateDiagnostics(err) {
			if diags.ImportPath != "" {
				output, _ := json.Marshal(jsonDiag{diags.ImportPath, "build-output", "# " + diags.ImportPath + "\n"})
				os.Stdout.Write(append(output, '\n'))
			}
			for _, diag := range diags.Diagnostics {
				w := &bytes.Buffer{}
				diag.WriteTo(w, wd)
				output, _ := json.Marshal(jsonDiag{diags.ImportPath, "build-output", w.String()})
				os.Stdout.Write(append(output, '\n'))
			}
			output, _ := json.Marshal(jsonDiag{diags.ImportPath, "build-fail", ""})
			os.Stdout.Write(append(output, '\n'))
		}
		os.Exit(1)
	}
	handleCompilerError(err)
}

const usageText = `moxie version %s

usage: %s <command> [arguments]

commands:
  build:   compile packages and dependencies
  run:     compile and run immediately
  test:    test packages
  clean:   empty the cache directory (%s)
  targets: list all supported targets
  version: print version information
  env:     print environment information
  vendor:  copy dependencies into vendor/
  header:  extract codec types from a package into a .mxh protocol header
  install: fetch, compile, and cache an external package binary + .mxh
  fetch:   fetch and cache all dependencies declared in moxie.mod
  cache:   manage the install cache (subcommands: list, clean)
  help:    print this help text
`

func usage(command string) {
	fmt.Fprintf(os.Stderr, usageText, goenv.Version(), os.Args[0], goenv.Get("GOCACHE"))
	if flag.Parsed() {
		fmt.Fprintln(os.Stderr, "\nflags:")
		flag.PrintDefaults()
	}
}

func handleChdirFlag() {
	used := 2
	if used >= len(os.Args) {
		return
	}
	var dir string
	switch a := os.Args[used]; {
	default:
		return
	case a == "-C", a == "--C":
		if used+1 >= len(os.Args) {
			return
		}
		dir = os.Args[used+1]
		copy(os.Args[used:], os.Args[used+2:])
		os.Args = os.Args[:len(os.Args)-2]
	case strings.HasPrefix(a, "-C="), strings.HasPrefix(a, "--C="):
		_, dir, _ = strings.Cut(a, "=")
		copy(os.Args[used:], os.Args[used+1:])
		os.Args = os.Args[:len(os.Args)-1]
	}
	if err := os.Chdir(dir); err != nil {
		fmt.Fprintln(os.Stderr, "cannot chdir:", err)
		os.Exit(1)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "No command-line arguments supplied.")
		usage("")
		os.Exit(1)
	}
	command := os.Args[1]

	opt := flag.String("opt", "0", "optimization level: 0, 1, 2, s, z (Moxie default: 0 — LLVM optimization is forbidden; Moxie's source-to-codegen mapping is 1:1, the optimizer assumes semantics Moxie does not have)")
	panicStrategy := flag.String("panic", "print", "panic strategy (print, trap)")
	scheduler := flag.String("scheduler", "", "which scheduler to use (none, tasks)")
	work := flag.Bool("work", false, "print the name of the temporary build directory and do not delete this directory on exit")
	interpTimeout := flag.Duration("interp-timeout", 180*time.Second, "interp optimization pass timeout")
	var tags buildutil.TagsFlag
	flag.Var(&tags, "tags", "a space-separated list of extra build tags")
	target := flag.String("target", "", "target specification")
	buildMode := flag.String("buildmode", "", "build mode to use (default)")
	stackSize := flag.Uint64("stack-size", 0, "goroutine stack size")
	printSize := flag.String("size", "", "print sizes (none, short, full)")
	printStacks := flag.Bool("print-stacks", false, "print stack sizes of goroutines")
	printAllocsString := flag.String("print-allocs", "", "regular expression of functions for which heap allocations should be printed")
	printCommands := flag.Bool("x", false, "Print commands")
	flagJSON := flag.Bool("json", false, "print output in JSON format")
	parallelism := flag.Int("p", runtime.GOMAXPROCS(0), "the number of build jobs that can run in parallel")
	nodebug := flag.Bool("no-debug", false, "strip debug information")
	nobounds := flag.Bool("nobounds", false, "do not emit bounds checks")
	ldflags := flag.String("ldflags", "", "Go link tool compatible ldflags")
	llvmFeatures := flag.String("llvm-features", "", "comma separated LLVM features to enable")
	cpuprofile := flag.String("cpuprofile", "", "cpuprofile output")
	memprofile := flag.String("memprofile", "", "heap profile output")

	// Internal flags.
	printIR := flag.Bool("internal-printir", false, "print LLVM IR")
	dumpSSA := flag.Bool("internal-dumpssa", false, "dump internal Moxie SSA")
	verifyIR := flag.Bool("internal-verifyir", false, "run extra verification steps on LLVM IR")
	standaloneRuntime := flag.Bool("standalone-runtime", false, "keep runtime symbols external (for build-runtime)")
	rebuildAll := flag.Bool("a", false, "rebuild all packages from scratch (ignore build cache; required for bootstrap)")

	var flagDeps, flagTest bool
	if command == "help" || command == "list" {
		flag.BoolVar(&flagDeps, "deps", false, "supply -deps flag to moxie list")
		flag.BoolVar(&flagTest, "test", false, "supply -test flag to moxie list")
	}
	var outpath string
	if command == "help" || command == "build" || command == "test" || command == "header" {
		flag.StringVar(&outpath, "o", "", "output filename")
	}

	var testConfig compileopts.TestConfig
	if command == "help" || command == "test" {
		flag.BoolVar(&testConfig.CompileOnly, "c", false, "compile the test binary but do not run it")
		flag.BoolVar(&testConfig.Verbose, "v", false, "verbose: print additional output")
		flag.BoolVar(&testConfig.Short, "short", false, "short: run smaller test suite to save time")
		flag.StringVar(&testConfig.RunRegexp, "run", "", "run: regexp of tests to run")
		flag.StringVar(&testConfig.SkipRegexp, "skip", "", "skip: regexp of tests to skip")
		testConfig.Count = flag.Int("count", 1, "count: number of times to run tests/benchmarks")
		flag.StringVar(&testConfig.BenchRegexp, "bench", "", "bench: regexp of benchmarks to run")
		flag.StringVar(&testConfig.BenchTime, "benchtime", "", "run each benchmark for duration d")
		flag.BoolVar(&testConfig.BenchMem, "benchmem", false, "show memory stats for benchmarks")
		flag.StringVar(&testConfig.Shuffle, "shuffle", "", "shuffle the order the tests and benchmarks run")
	}

	handleChdirFlag()
	switch command {
	case "clang", "ld.lld", "wasm-ld":
		err := builder.RunTool(command, os.Args[2:]...)
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	flag.CommandLine.Parse(os.Args[2:])
	globalVarValues, extLDFlags, err := parseGoLinkFlag(*ldflags)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var printAllocs *regexp.Regexp
	if *printAllocsString != "" {
		printAllocs, err = regexp.Compile(*printAllocsString)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	options := &compileopts.Options{
		GOOS:          goenv.Get("GOOS"),
		GOARCH:        goenv.Get("GOARCH"),
		Target:        *target,
		BuildMode:     *buildMode,
		StackSize:     *stackSize,
		Opt:           *opt,
		PanicStrategy: *panicStrategy,
		Scheduler:     *scheduler,
		Work:          *work,
		InterpTimeout: *interpTimeout,
		PrintIR:       *printIR,
		DumpSSA:       *dumpSSA,
		VerifyIR:      *verifyIR,
		Semaphore:     make(chan struct{}, *parallelism),
		Debug:         !*nodebug,
		Nobounds:      *nobounds,
		PrintSizes:    *printSize,
		PrintStacks:   *printStacks,
		PrintAllocs:   printAllocs,
		Tags:          []string(tags),
		TestConfig:    testConfig,
		GlobalValues:  globalVarValues,
		LLVMFeatures:       *llvmFeatures,
		StandaloneRuntime:  *standaloneRuntime,
		RebuildAll:         *rebuildAll,
	}
	if *printCommands {
		options.PrintCommands = printCommand
	}

	if extLDFlags != "" {
		options.ExtLDFlags, err = shlex.Split(extLDFlags)
		if err != nil {
			fmt.Fprintln(os.Stderr, "could not parse -extldflags:", err)
			os.Exit(1)
		}
	}

	err = options.Verify()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		usage(command)
		os.Exit(1)
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "could not create CPU profile: ", err)
			os.Exit(1)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintln(os.Stderr, "could not start CPU profile: ", err)
			os.Exit(1)
		}
		defer pprof.StopCPUProfile()
	}

	if *memprofile != "" {
		defer func() {
			runtime.GC()
			f, err := os.Create(*memprofile)
			if err != nil {
				fmt.Fprintln(os.Stderr, "could not create memory profile: ", err)
				return
			}
			defer f.Close()
			if err := pprof.WriteHeapProfile(f); err != nil {
				fmt.Fprintln(os.Stderr, "could not write memory profile: ", err)
			}
		}()
	}

	switch command {
	case "build":
		pkgName := "."
		if flag.NArg() == 1 {
			pkgName = filepath.ToSlash(flag.Arg(0))
		} else if flag.NArg() > 1 {
			fmt.Fprintln(os.Stderr, "build only accepts a single positional argument: package name")
			usage(command)
			os.Exit(1)
		}
		config, err := builder.NewConfig(options)
		handleCompilerError(err)
		err = Build(pkgName, outpath, config)
		printBuildOutput(err, *flagJSON)
	case "run":
		if flag.NArg() < 1 {
			fmt.Fprintln(os.Stderr, "No package specified.")
			usage(command)
			os.Exit(1)
		}
		pkgName := filepath.ToSlash(flag.Arg(0))
		err := Run(pkgName, options, flag.Args()[1:])
		printBuildOutput(err, *flagJSON)
	case "test":
		var pkgNames []string
		for i := 0; i < flag.NArg(); i++ {
			pkgNames = append(pkgNames, filepath.ToSlash(flag.Arg(i)))
		}
		if len(pkgNames) == 0 {
			pkgNames = []string{"."}
		}
		explicitPkgNames, err := getListOfPackages(pkgNames, options)
		if err != nil {
			fmt.Printf("cannot resolve packages: %v\n", err)
			os.Exit(1)
		}
		if outpath != "" && len(explicitPkgNames) > 1 {
			fmt.Println("cannot use -o flag with multiple packages")
			os.Exit(1)
		}

		fail := make(chan struct{}, 1)
		bufs := make([]testOutputBuf, len(explicitPkgNames))
		for i := range bufs {
			bufs[i].entries = make(chan outputEntry, 64)
		}
		flusherDone := make(chan struct{})
		go func() {
			defer close(flusherDone)
			for i := range bufs {
				bufs[i].flush(os.Stdout, os.Stderr)
			}
		}()
		testSema := make(chan struct{}, cap(options.Semaphore))
		testsDone := make(chan struct{}, len(explicitPkgNames))
		for i, pkgName := range explicitPkgNames {
			pkgName := pkgName
			buf := &bufs[i]
			testSema <- struct{}{}
			go func() {
				defer func() { <-testSema }()
				defer func() { close(buf.entries); testsDone <- struct{}{} }()
				stdout := (*testStdout)(buf)
				stderr := (*testStderr)(buf)
				passed, err := Test(pkgName, stdout, stderr, options, outpath)
				if err != nil {
					wd, _ := os.Getwd()
					diagnostics.CreateDiagnostics(err).WriteTo(os.Stderr, wd)
					os.Exit(1)
				}
				if !passed {
					select {
					case fail <- struct{}{}:
					default:
					}
				}
			}()
		}
		for range explicitPkgNames {
			<-testsDone
		}
		<-flusherDone
		close(fail)
		if _, fail := <-fail; fail {
			os.Exit(1)
		}
	case "targets":
		specs := compileopts.GetTargetSpecs()
		names := make([]string, 0, len(specs))
		for key := range specs {
			names = append(names, key)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Println(name)
		}
	case "info":
		config, err := builder.NewConfig(options)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		config.GoMinorVersion = 0
		if *flagJSON {
			data, _ := json.MarshalIndent(struct {
				Target     *compileopts.TargetSpec `json:"target"`
				GOOS       string                  `json:"goos"`
				GOARCH     string                  `json:"goarch"`
				BuildTags  []string                `json:"build_tags"`
				Scheduler  string                  `json:"scheduler"`
				LLVMTriple string                  `json:"llvm_triple"`
			}{
				Target:     config.Target,
				GOOS:       config.GOOS(),
				GOARCH:     config.GOARCH(),
				BuildTags:  config.BuildTags(),
				Scheduler:  config.Scheduler(),
				LLVMTriple: config.Triple(),
			}, "", "  ")
			fmt.Println(string(data))
		} else {
			fmt.Printf("LLVM triple:       %s\n", config.Triple())
			fmt.Printf("GOOS:              %s\n", config.GOOS())
			fmt.Printf("GOARCH:            %s\n", config.GOARCH())
			fmt.Printf("build tags:        %s\n", strings.Join(config.BuildTags(), " "))
			fmt.Printf("scheduler:         %s\n", config.Scheduler())
		}
	case "list":
		config, err := builder.NewConfig(options)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		var extraArgs []string
		if *flagJSON {
			extraArgs = append(extraArgs, "-json")
		}
		if flagDeps {
			extraArgs = append(extraArgs, "-deps")
		}
		if flagTest {
			extraArgs = append(extraArgs, "-test")
		}
		cmd, err := loader.List(config, extraArgs, flag.Args())
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to run `moxie list`:", err)
			os.Exit(1)
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			fmt.Fprintln(os.Stderr, "failed to run `moxie list`:", err)
			os.Exit(1)
		}
	case "vendor":
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := loader.Vendor(wd); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "clean":
		err := os.RemoveAll(goenv.Get("GOCACHE"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot clean cache:", err)
			os.Exit(1)
		}
	case "header":
		pkgName := "."
		if flag.NArg() >= 1 {
			pkgName = filepath.ToSlash(flag.Arg(0))
		}
		if outpath == "" {
			outpath = "."
		}
		// Scan remaining args for --prev <old.mxh>.
		prevPath := ""
		args := flag.Args()
		for i := 1; i < len(args)-1; i++ {
			if args[i] == "--prev" || args[i] == "-prev" {
				prevPath = args[i+1]
			}
		}
		err := Header(pkgName, outpath, prevPath, options)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "install":
		if flag.NArg() < 1 {
			fmt.Fprintln(os.Stderr, "install requires a package path")
			usage(command)
			os.Exit(1)
		}
		pkgName := filepath.ToSlash(flag.Arg(0))
		err := Install(pkgName, options)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "fetch":
		err := Fetch(options)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "cache":
		subcmd := ""
		if flag.NArg() >= 1 {
			subcmd = flag.Arg(0)
		}
		switch subcmd {
		case "clean":
			if err := cache.Clean(); err != nil {
				fmt.Fprintln(os.Stderr, "cannot clean install cache:", err)
				os.Exit(1)
			}
		case "list":
			paths, err := cache.List()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			for _, p := range paths {
				fmt.Println(p)
			}
		default:
			fmt.Fprintln(os.Stderr, "cache subcommand must be 'list' or 'clean'")
			os.Exit(1)
		}
	case "build-runtime":
		options.StandaloneRuntime = true
		config, err := builder.NewConfig(options)
		handleCompilerError(err)
		err = BuildRuntime(outpath, config)
		printBuildOutput(err, *flagJSON)
	case "help":
		usage("")
	case "version":
		goversion := "<unknown>"
		if s, err := goenv.GorootVersionString(); err == nil {
			goversion = s
		}
		fmt.Printf("moxie version %s %s/%s (using moxie version %s)\n", goenv.Version(), runtime.GOOS, runtime.GOARCH, goversion)
	case "env":
		if flag.NArg() == 0 {
			for _, key := range goenv.Keys {
				fmt.Printf("%s=%#v\n", key, goenv.Get(key))
			}
		} else {
			for i := 0; i < flag.NArg(); i++ {
				fmt.Println(goenv.Get(flag.Arg(i)))
			}
		}
	default:
		fmt.Fprintln(os.Stderr, "Unknown command:", command)
		usage("")
		os.Exit(1)
	}
}

// testOutputBuf buffers the output of concurrent tests.
// Output entries are sent through a channel; the flusher goroutine is the
// sole consumer, so no mutex is needed.
type testOutputBuf struct {
	entries chan outputEntry
}

func (b *testOutputBuf) flush(stdout, stderr io.Writer) {
	for e := range b.entries {
		if e.stderr {
			stderr.Write(e.data)
		} else {
			stdout.Write(e.data)
		}
	}
}

type testStdout testOutputBuf

func (out *testStdout) Write(data []byte) (int, error) {
	cp := make([]byte, len(data))
	copy(cp, data)
	out.entries <- outputEntry{stderr: false, data: cp}
	return len(data), nil
}

type testStderr testOutputBuf

func (out *testStderr) Write(data []byte) (int, error) {
	cp := make([]byte, len(data))
	copy(cp, data)
	out.entries <- outputEntry{stderr: true, data: cp}
	return len(data), nil
}

type outputEntry struct {
	stderr bool
	data   []byte
}
