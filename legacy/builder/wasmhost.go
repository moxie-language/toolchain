package builder

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// emitWasmHost stages the page-side half of a wasm bundle beside the module.
//
// A Moxie wasm module is half an application. The other half is the JS runtime
// that instantiates it in a Worker, answers its DOM calls through the relay and
// dispatches the callbacks it registered - jsruntime/ in the moxie checkout,
// one copy, versioned with the compiler that built the module. The build copies
// it to $runtime/ next to the .wasm and writes the bundle's page bootstrap,
// <name>-wasm-host.mjs, so an app has no host files of its own to keep in sync
// with the framework.
//
// Stage4 writes the same files from the same directory (emitWasmHost in
// _mxc_stage4/main.mx): a bundle built by either compiler has to be loadable by
// the same page.
func emitWasmHost(root, outpath string) error {
	if outpath == "" || outpath == "/dev/null" {
		return nil
	}
	if root == "" {
		return errors.New("MOXIEROOT is not set, so the JS runtime to stage beside the module cannot be found")
	}
	jsdir := filepath.Join(root, "jsruntime")
	entries, err := os.ReadDir(jsdir)
	if err != nil {
		return fmt.Errorf("jsruntime: %w", err)
	}
	dir := filepath.Dir(outpath)
	runtimeDir := filepath.Join(dir, "$runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return err
	}
	staged := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mjs") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(jsdir, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(runtimeDir, e.Name()), data, 0o644); err != nil {
			return err
		}
		staged++
	}
	if staged == 0 {
		return fmt.Errorf("no JS runtime modules under %s", jsdir)
	}
	base := strings.TrimSuffix(filepath.Base(outpath), ".wasm")
	host := filepath.Join(dir, base+"-wasm-host.mjs")
	if err := os.WriteFile(host, []byte(wasmHostModule(base)), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  host %s ($runtime: %d modules)\n", host, staged)
	return nil
}

// wasmHostModule is the page bootstrap for one bundle. It is deliberately
// small: everything a page needs beyond it - the loader, the Worker host, the
// DOM relay - is in $runtime/ and is the same code for every app.
func wasmHostModule(base string) string {
	return "// " + base + "-wasm-host.mjs - written by `moxie build -target wasm`.\n" +
		"//\n" +
		"// The page-side half of a Moxie wasm bundle. The module runs in a Worker\n" +
		"// (channel receives block in Atomics.wait, which the page thread forbids)\n" +
		"// and the page answers its DOM calls through $runtime/relay.mjs, so a page\n" +
		"// only has to import this file and start it:\n" +
		"//\n" +
		"//   import { start } from './" + base + "-wasm-host.mjs';\n" +
		"//   await start();\n" +
		"\n" +
		"import { load } from './$runtime/loader.mjs';\n" +
		"\n" +
		"export const wasm = new URL('./" + base + ".wasm', import.meta.url).href;\n" +
		"\n" +
		"export function start() { return load(wasm); }\n" +
		"\n" +
		"export default start;\n"
}
