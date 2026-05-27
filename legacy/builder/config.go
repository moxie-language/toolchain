package builder

import (
	"fmt"
	"runtime"

	"moxie/compileopts"
	"moxie/goenv"
)

// NewConfig builds a new Config object from a set of compiler options. It also
// loads some information from the environment while doing that. For example, it
// uses the currently active GOPATH (from the goenv package) to determine the Go
// version to use.
func NewConfig(options *compileopts.Options) (*compileopts.Config, error) {
	spec, err := compileopts.LoadTarget(options)
	if err != nil {
		return nil, err
	}

	// Version range supported by Moxie.
	const minorMin = 19
	const minorMax = 25

	// Check that we support this Go toolchain version.
	gorootMajor, gorootMinor, err := goenv.GetGorootVersion()
	if err != nil {
		return nil, err
	}

	// Check that the Go toolchain version isn't too new, if we haven't been
	// compiled with the latest Go version.
	// This may be a bit too aggressive: if the newer version doesn't change the
	// Go language we will most likely be able to compile it.
	buildMajor, buildMinor, _, err := goenv.Parse(runtime.Version())
	if err != nil {
		return nil, err
	}
	if buildMajor != 1 || buildMinor < gorootMinor {
		return nil, fmt.Errorf("cannot compile with Go toolchain version go%d.%d (Moxie was built using toolchain version %s)", gorootMajor, gorootMinor, runtime.Version())
	}

	return &compileopts.Config{
		Options:        options,
		Target:         spec,
		GoMinorVersion: gorootMinor,
		TestConfig:     options.TestConfig,
	}, nil
}
