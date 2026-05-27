module bootstrap/probe_llvm

go 1.24

require bootstrap/llvmpure v0.0.0

require github.com/ebitengine/purego v0.10.0 // indirect

replace bootstrap/llvmpure => ../llvmpure
