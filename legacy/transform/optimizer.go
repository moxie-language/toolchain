package transform

import (
	"errors"
	"fmt"
	"go/token"
	"os"

	"moxie/compileopts"
	"moxie/compiler/ircheck"
	"moxie/compiler/llvmutil"
	"tinygo.org/x/go-llvm"
)

// OptimizePackage runs optimization passes over the LLVM module for the given
// Go package.
func OptimizePackage(mod llvm.Module, config *compileopts.Config) {
	_, speedLevel, _ := config.OptLevel()

	// Run Moxie-specific optimization passes.
	if speedLevel > 0 {
		OptimizeMaps(mod)
	}
}

// Optimize runs a number of optimization and transformation passes over the
// given module. Some passes are specific to Moxie, others are generic LLVM
// passes.
//
// Please note that some optimizations are not optional, thus Optimize must
// always be run before emitting machine code.
func Optimize(mod llvm.Module, config *compileopts.Config) []error {
	_, speedLevel, _ := config.OptLevel()

	// Make sure these functions are kept in tact during Moxie transformation passes.
	for _, name := range functionsUsedInTransforms {
		fn := mod.NamedFunction(name)
		if fn.IsNil() {
			panic(fmt.Errorf("missing core function %q", name))
		}
		fn.SetLinkage(llvm.ExternalLinkage)
	}

	// run a check of all of our code
	if config.VerifyIR() {
		errs := ircheck.Module(mod)
		if errs != nil {
			return errs
		}
	}

	if speedLevel > 0 {
		// Run some preparatory passes for the Go optimizer.
		po := llvm.NewPassBuilderOptions()
		defer po.Dispose()
		optPasses := "globaldce,globalopt,ipsccp,instcombine<no-verify-fixpoint>,adce,function-attrs"
		if llvmutil.Version() < 18 {
			// LLVM 17 doesn't have the no-verify-fixpoint flag.
			optPasses = "globaldce,globalopt,ipsccp,instcombine,adce,function-attrs"
		}
		err := mod.RunPasses(optPasses, llvm.TargetMachine{}, po)
		if err != nil {
			return []error{fmt.Errorf("could not build pass pipeline: %w", err)}
		}

		// Run Moxie-specific optimization passes.
		OptimizeStringToBytes(mod)
		OptimizeReflectImplements(mod)
		maxStackSize := config.MaxStackAlloc()
		OptimizeAllocs(mod, nil, maxStackSize, nil)
		err = LowerInterfaces(mod, config)
		if err != nil {
			return []error{err}
		}

		errs := LowerInterrupts(mod)
		if len(errs) > 0 {
			return errs
		}

		// After interfaces are lowered, there are many more opportunities for
		// interprocedural optimizations. To get them to work, function
		// attributes have to be updated first.
		err = mod.RunPasses(optPasses, llvm.TargetMachine{}, po)
		if err != nil {
			return []error{fmt.Errorf("could not build pass pipeline: %w", err)}
		}

		// Run Moxie-specific interprocedural optimizations.
		OptimizeAllocs(mod, config.Options.PrintAllocs, maxStackSize, func(pos token.Position, msg string) {
			fmt.Fprintln(os.Stderr, pos.String()+": "+msg)
		})
		OptimizeStringToBytes(mod)
		OptimizeStringEqual(mod)

	} else {
		// Must be run at any optimization level.
		err := LowerInterfaces(mod, config)
		if err != nil {
			return []error{err}
		}
		errs := LowerInterrupts(mod)
		if len(errs) > 0 {
			return errs
		}

		// Clean up some leftover symbols of the previous transformations.
		po := llvm.NewPassBuilderOptions()
		defer po.Dispose()
		err = mod.RunPasses("globaldce", llvm.TargetMachine{}, po)
		if err != nil {
			return []error{fmt.Errorf("could not build pass pipeline: %w", err)}
		}
	}

	if config.Scheduler() == "none" {
		// Check for any goroutine starts.
		if start := mod.NamedFunction("internal/task.start"); !start.IsNil() && len(getUses(start)) > 0 {
			errs := []error{}
			for _, call := range getUses(start) {
				errs = append(errs, errorAt(call, "attempted to start a goroutine without a scheduler"))
			}
			return errs
		}
	}

	if config.VerifyIR() {
		if errs := ircheck.Module(mod); errs != nil {
			return errs
		}
	}
	if err := llvm.VerifyModule(mod, llvm.PrintMessageAction); err != nil {
		return []error{errors.New("optimizations caused a verification failure")}
	}

	if config.IsStandaloneRuntime() {
		// Standalone runtime: keep all symbols external for cross-module linking.
		// Skip internalization and globaldce entirely.
		return nil
	}

	// After Moxie-specific transforms have finished, undo exporting these functions.
	for _, name := range functionsUsedInTransforms {
		fn := mod.NamedFunction(name)
		if fn.IsNil() || fn.IsDeclaration() {
			continue
		}
		fn.SetLinkage(llvm.InternalLinkage)
	}

	// No ThinLTO pre-link passes - we emit native objects and link without LTO.
	// Run a basic cleanup pass instead of the full thinlto-pre-link pipeline.
	po := llvm.NewPassBuilderOptions()
	defer po.Dispose()
	if speedLevel > 0 {
		err := mod.RunPasses("globaldce", llvm.TargetMachine{}, po)
		if err != nil {
			return []error{fmt.Errorf("could not build pass pipeline: %w", err)}
		}
	}

	return nil
}

// functionsUsedInTransform is a list of function symbols that may be used
// during Moxie optimization passes so they have to be marked as external
// linkage until all Moxie passes have finished.
var functionsUsedInTransforms = []string{
	"runtime.alloc",
	"runtime.free",
	"runtime.nilPanic",
}
