package compileopts

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	validBuildModeOptions     = []string{"default"}
	validGCOptions            = []string{"none", "leaking", "conservative", "boehm"}
	validSchedulerOptions     = []string{"none", "tasks"}
	validPanicStrategyOptions = []string{"print", "trap"}
	validOptOptions           = []string{"none", "0", "1", "2", "s", "z"}
)

// Options contains extra options to give to the compiler.
type Options struct {
	GOOS          string
	GOARCH        string
	Directory     string // working dir
	Target        string
	BuildMode     string
	Opt           string
	GC            string
	PanicStrategy string
	Scheduler     string
	StackSize     uint64
	Work          bool
	InterpTimeout time.Duration
	PrintIR       bool
	DumpSSA       bool
	VerifyIR      bool
	SkipDWARF     bool
	PrintCommands func(cmd string, args ...string) `json:"-"`
	Semaphore     chan struct{}                    `json:"-"`
	Debug         bool
	Nobounds      bool
	PrintSizes    string
	PrintAllocs   *regexp.Regexp
	PrintStacks   bool
	Tags          []string
	GlobalValues  map[string]map[string]string
	TestConfig    TestConfig
	LLVMFeatures  string
	ExtLDFlags    []string
}

// Verify performs validation on the given options.
func (o *Options) Verify() error {
	if o.BuildMode != "" && !isInArray(validBuildModeOptions, o.BuildMode) {
		return fmt.Errorf(`invalid buildmode option '%s': valid values are %s`,
			o.BuildMode, strings.Join(validBuildModeOptions, ", "))
	}
	if o.GC != "" && !isInArray(validGCOptions, o.GC) {
		return fmt.Errorf(`invalid gc option '%s': valid values are %s`,
			o.GC, strings.Join(validGCOptions, ", "))
	}
	if o.Scheduler != "" && !isInArray(validSchedulerOptions, o.Scheduler) {
		return fmt.Errorf(`invalid scheduler option '%s': valid values are %s`,
			o.Scheduler, strings.Join(validSchedulerOptions, ", "))
	}
	if o.PanicStrategy != "" && !isInArray(validPanicStrategyOptions, o.PanicStrategy) {
		return fmt.Errorf(`invalid panic option '%s': valid values are %s`,
			o.PanicStrategy, strings.Join(validPanicStrategyOptions, ", "))
	}
	if o.Opt != "" && !isInArray(validOptOptions, o.Opt) {
		return fmt.Errorf("invalid -opt=%s: valid values are %s",
			o.Opt, strings.Join(validOptOptions, ", "))
	}
	return nil
}

func isInArray(arr []string, item string) bool {
	for _, i := range arr {
		if i == item {
			return true
		}
	}
	return false
}
