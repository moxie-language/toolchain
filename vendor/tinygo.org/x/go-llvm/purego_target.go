//go:build purego

package llvm

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

type (
	TargetData      struct{ C uintptr }
	Target          struct{ C uintptr }
	TargetMachine   struct{ C uintptr }
	ByteOrdering    int
	RelocMode       int
	CodeGenOptLevel int
	CodeGenFileType int
	CodeModel       int
)

func (td TargetData) IsNil() bool    { return td.C == 0 }
func (t Target) IsNil() bool         { return t.C == 0 }
func (tm TargetMachine) IsNil() bool { return tm.C == 0 }

const (
	BigEndian    ByteOrdering = 0
	LittleEndian ByteOrdering = 1
)

const (
	RelocDefault      RelocMode = 0
	RelocStatic       RelocMode = 1
	RelocPIC          RelocMode = 2
	RelocDynamicNoPic RelocMode = 3
)

const (
	CodeGenLevelNone       CodeGenOptLevel = 0
	CodeGenLevelLess       CodeGenOptLevel = 1
	CodeGenLevelDefault    CodeGenOptLevel = 2
	CodeGenLevelAggressive CodeGenOptLevel = 3
)

const (
	CodeModelDefault    CodeModel = 0
	CodeModelJITDefault CodeModel = 1
	CodeModelTiny       CodeModel = 2
	CodeModelSmall      CodeModel = 3
	CodeModelKernel     CodeModel = 4
	CodeModelMedium     CodeModel = 5
	CodeModelLarge      CodeModel = 6
)

const (
	AssemblyFile CodeGenFileType = 0
	ObjectFile   CodeGenFileType = 1
)

var (
	// Per-target init (inline functions in C headers aren't exported)
	targetInits []uintptr

	// Target lookup
	symGetTargetFromTriple   uintptr
	symGetFirstTarget        uintptr
	symGetNextTarget         uintptr
	symGetTargetName         uintptr
	symGetTargetDescription  uintptr
	symGetDefaultTargetTriple uintptr

	// TargetMachine
	symCreateTargetMachine       uintptr
	symCreateTargetDataLayout    uintptr
	symGetTargetMachineTriple    uintptr
	symTargetMachineEmitToMemBuf uintptr
	symAddAnalysisPasses         uintptr
	symDisposeTargetMachine      uintptr

	// TargetData
	symCreateTargetData          uintptr
	symCopyStringRepOfTargetData uintptr
	symByteOrder                 uintptr
	symPointerSize               uintptr
	symIntPtrType                uintptr
	symSizeOfTypeInBits          uintptr
	symStoreSizeOfType           uintptr
	symABISizeOfType             uintptr
	symABIAlignOfType            uintptr
	symCallFrameAlignOfType      uintptr
	symPrefAlignOfType           uintptr
	symPrefAlignOfGlobal         uintptr
	symElementAtOffset           uintptr
	symOffsetOfElement           uintptr
	symDisposeTargetData         uintptr

	// MemoryBuffer
	symGetBufferStart      uintptr
	symGetBufferSize       uintptr
	symDisposeMemoryBuffer uintptr

	// Bitcode
	symWriteBitcodeToFD         uintptr
	symWriteBitcodeToMemBuf     uintptr
	symCreateMemBufWithFile     uintptr
	symParseBitcodeInCtx2       uintptr

	// Linker
	symLinkModules2 uintptr

	// PassManager
	symCreatePassManager  uintptr
	symDisposePassManager uintptr
	symRunPassManager     uintptr

	// Thin LTO (glue)
	symGoWriteThinLTOBitcode uintptr
)

func registerTarget() {
	// LLVMInitializeAll* are inline C functions, not exported symbols.
	// We call per-target init functions directly.
	targets := []string{"X86", "AArch64", "ARM", "WebAssembly", "RISCV", "Mips", "AMDGPU"}
	suffixes := []string{"TargetInfo", "Target", "TargetMC", "AsmParser", "AsmPrinter"}
	for _, t := range targets {
		for _, s := range suffixes {
			sym := trySym(libLLVM, "LLVMInitialize"+t+s)
			if sym != 0 {
				targetInits = append(targetInits, sym)
			}
		}
	}

	symGetTargetFromTriple = mustSym(libLLVM, "LLVMGetTargetFromTriple")
	symGetFirstTarget = mustSym(libLLVM, "LLVMGetFirstTarget")
	symGetNextTarget = mustSym(libLLVM, "LLVMGetNextTarget")
	symGetTargetName = mustSym(libLLVM, "LLVMGetTargetName")
	symGetTargetDescription = mustSym(libLLVM, "LLVMGetTargetDescription")
	symGetDefaultTargetTriple = mustSym(libLLVM, "LLVMGetDefaultTargetTriple")

	symCreateTargetMachine = mustSym(libLLVM, "LLVMCreateTargetMachine")
	symCreateTargetDataLayout = mustSym(libLLVM, "LLVMCreateTargetDataLayout")
	symGetTargetMachineTriple = mustSym(libLLVM, "LLVMGetTargetMachineTriple")
	symTargetMachineEmitToMemBuf = mustSym(libLLVM, "LLVMTargetMachineEmitToMemoryBuffer")
	symAddAnalysisPasses = mustSym(libLLVM, "LLVMAddAnalysisPasses")
	symDisposeTargetMachine = mustSym(libLLVM, "LLVMDisposeTargetMachine")

	symCreateTargetData = mustSym(libLLVM, "LLVMCreateTargetData")
	symCopyStringRepOfTargetData = mustSym(libLLVM, "LLVMCopyStringRepOfTargetData")
	symByteOrder = mustSym(libLLVM, "LLVMByteOrder")
	symPointerSize = mustSym(libLLVM, "LLVMPointerSize")
	symIntPtrType = mustSym(libLLVM, "LLVMIntPtrType")
	symSizeOfTypeInBits = mustSym(libLLVM, "LLVMSizeOfTypeInBits")
	symStoreSizeOfType = mustSym(libLLVM, "LLVMStoreSizeOfType")
	symABISizeOfType = mustSym(libLLVM, "LLVMABISizeOfType")
	symABIAlignOfType = mustSym(libLLVM, "LLVMABIAlignmentOfType")
	symCallFrameAlignOfType = mustSym(libLLVM, "LLVMCallFrameAlignmentOfType")
	symPrefAlignOfType = mustSym(libLLVM, "LLVMPreferredAlignmentOfType")
	symPrefAlignOfGlobal = mustSym(libLLVM, "LLVMPreferredAlignmentOfGlobal")
	symElementAtOffset = mustSym(libLLVM, "LLVMElementAtOffset")
	symOffsetOfElement = mustSym(libLLVM, "LLVMOffsetOfElement")
	symDisposeTargetData = mustSym(libLLVM, "LLVMDisposeTargetData")

	symGetBufferStart = mustSym(libLLVM, "LLVMGetBufferStart")
	symGetBufferSize = mustSym(libLLVM, "LLVMGetBufferSize")
	symDisposeMemoryBuffer = mustSym(libLLVM, "LLVMDisposeMemoryBuffer")

	symWriteBitcodeToFD = mustSym(libLLVM, "LLVMWriteBitcodeToFD")
	symWriteBitcodeToMemBuf = mustSym(libLLVM, "LLVMWriteBitcodeToMemoryBuffer")
	symCreateMemBufWithFile = mustSym(libLLVM, "LLVMCreateMemoryBufferWithContentsOfFile")
	symParseBitcodeInCtx2 = mustSym(libLLVM, "LLVMParseBitcodeInContext2")

	symLinkModules2 = mustSym(libLLVM, "LLVMLinkModules2")

	symCreatePassManager = mustSym(libLLVM, "LLVMCreatePassManager")
	symDisposePassManager = mustSym(libLLVM, "LLVMDisposePassManager")
	symRunPassManager = mustSym(libLLVM, "LLVMRunPassManager")

	if libGlue != 0 {
		symGoWriteThinLTOBitcode = trySym(libGlue, "LLVMGoWriteThinLTOBitcodeToMemoryBuffer")
	}
}

// ---- Target initialization ----

var targetsInitialized bool

func InitializeAllTargets() {
	if targetsInitialized {
		return
	}
	targetsInitialized = true
	for _, sym := range targetInits {
		purego.SyscallN(sym)
	}
}

func InitializeAllTargetInfos() { InitializeAllTargets() }
func InitializeAllTargetMCs()   { InitializeAllTargets() }
func InitializeAllAsmParsers()  { InitializeAllTargets() }
func InitializeAllAsmPrinters() { InitializeAllTargets() }

// ---- Target lookup ----

func GetTargetFromTriple(triple string) (Target, error) {
	ctriple := cString(triple)
	var target uintptr
	var errMsg uintptr
	r, _, _ := purego.SyscallN(symGetTargetFromTriple, ctriple, uintptr(unsafe.Pointer(&target)), uintptr(unsafe.Pointer(&errMsg)))
	runtime.KeepAlive(triple)
	if r != 0 {
		s := goString(errMsg)
		purego.SyscallN(symDisposeMessage, errMsg)
		return Target{}, fmt.Errorf("%s", s)
	}
	return Target{target}, nil
}

func FirstTarget() Target {
	r, _, _ := purego.SyscallN(symGetFirstTarget)
	return Target{r}
}

func (t Target) NextTarget() Target {
	r, _, _ := purego.SyscallN(symGetNextTarget, t.C)
	return Target{r}
}

func (t Target) Name() string {
	r, _, _ := purego.SyscallN(symGetTargetName, t.C)
	return goString(r)
}

func (t Target) Description() string {
	r, _, _ := purego.SyscallN(symGetTargetDescription, t.C)
	return goString(r)
}

func DefaultTargetTriple() string {
	r, _, _ := purego.SyscallN(symGetDefaultTargetTriple)
	s := goString(r)
	purego.SyscallN(symDisposeMessage, r)
	return s
}

// ---- TargetMachine ----

func (t Target) CreateTargetMachine(triple, cpu, features string, level CodeGenOptLevel, reloc RelocMode, codeModel CodeModel) TargetMachine {
	ctriple := cString(triple)
	ccpu := cString(cpu)
	cfeatures := cString(features)
	r, _, _ := purego.SyscallN(symCreateTargetMachine, t.C, ctriple, ccpu, cfeatures, uintptr(level), uintptr(reloc), uintptr(codeModel))
	runtime.KeepAlive(triple)
	runtime.KeepAlive(cpu)
	runtime.KeepAlive(features)
	return TargetMachine{r}
}

func (tm TargetMachine) CreateTargetData() TargetData {
	r, _, _ := purego.SyscallN(symCreateTargetDataLayout, tm.C)
	return TargetData{r}
}

func (tm TargetMachine) Triple() string {
	r, _, _ := purego.SyscallN(symGetTargetMachineTriple, tm.C)
	s := goString(r)
	purego.SyscallN(symDisposeMessage, r)
	return s
}

func (tm TargetMachine) EmitToMemoryBuffer(m Module, ft CodeGenFileType) (MemoryBuffer, error) {
	var errMsg uintptr
	var mb uintptr
	r, _, _ := purego.SyscallN(symTargetMachineEmitToMemBuf, tm.C, m.C, uintptr(ft), uintptr(unsafe.Pointer(&errMsg)), uintptr(unsafe.Pointer(&mb)))
	if r != 0 {
		s := goString(errMsg)
		purego.SyscallN(symDisposeMessage, errMsg)
		return MemoryBuffer{}, fmt.Errorf("%s", s)
	}
	return MemoryBuffer{mb}, nil
}

func (tm TargetMachine) AddAnalysisPasses(pm PassManager) {
	purego.SyscallN(symAddAnalysisPasses, tm.C, pm.C)
}

func (tm TargetMachine) Dispose() {
	purego.SyscallN(symDisposeTargetMachine, tm.C)
}

// ---- TargetData ----

func NewTargetData(rep string) TargetData {
	crep := cString(rep)
	r, _, _ := purego.SyscallN(symCreateTargetData, crep)
	runtime.KeepAlive(rep)
	return TargetData{r}
}

func (td TargetData) String() string {
	r, _, _ := purego.SyscallN(symCopyStringRepOfTargetData, td.C)
	s := goString(r)
	purego.SyscallN(symDisposeMessage, r)
	return s
}

func (td TargetData) ByteOrder() ByteOrdering {
	r, _, _ := purego.SyscallN(symByteOrder, td.C)
	return ByteOrdering(r)
}

func (td TargetData) PointerSize() int {
	r, _, _ := purego.SyscallN(symPointerSize, td.C)
	return int(r)
}

func (td TargetData) IntPtrType() Type {
	r, _, _ := purego.SyscallN(symIntPtrType, td.C)
	return Type{r}
}

func (td TargetData) TypeSizeInBits(t Type) uint64 {
	r, _, _ := purego.SyscallN(symSizeOfTypeInBits, td.C, t.C)
	return uint64(r)
}

func (td TargetData) TypeStoreSize(t Type) uint64 {
	r, _, _ := purego.SyscallN(symStoreSizeOfType, td.C, t.C)
	return uint64(r)
}

func (td TargetData) TypeAllocSize(t Type) uint64 {
	r, _, _ := purego.SyscallN(symABISizeOfType, td.C, t.C)
	return uint64(r)
}

func (td TargetData) ABITypeAlignment(t Type) int {
	r, _, _ := purego.SyscallN(symABIAlignOfType, td.C, t.C)
	return int(r)
}

func (td TargetData) CallFrameTypeAlignment(t Type) int {
	r, _, _ := purego.SyscallN(symCallFrameAlignOfType, td.C, t.C)
	return int(r)
}

func (td TargetData) PrefTypeAlignment(t Type) int {
	r, _, _ := purego.SyscallN(symPrefAlignOfType, td.C, t.C)
	return int(r)
}

func (td TargetData) PreferredAlignment(g Value) int {
	r, _, _ := purego.SyscallN(symPrefAlignOfGlobal, td.C, g.C)
	return int(r)
}

func (td TargetData) ElementContainingOffset(t Type, offset uint64) int {
	r, _, _ := purego.SyscallN(symElementAtOffset, td.C, t.C, uintptr(offset))
	return int(r)
}

func (td TargetData) ElementOffset(t Type, element int) uint64 {
	r, _, _ := purego.SyscallN(symOffsetOfElement, td.C, t.C, uintptr(element))
	return uint64(r)
}

func (td TargetData) Dispose() {
	purego.SyscallN(symDisposeTargetData, td.C)
}

// ---- MemoryBuffer ----

func (mb MemoryBuffer) Bytes() []byte {
	start, _, _ := purego.SyscallN(symGetBufferStart, mb.C)
	size, _, _ := purego.SyscallN(symGetBufferSize, mb.C)
	if start == 0 || size == 0 {
		return nil
	}
	return (*[1 << 30]byte)(unsafe.Pointer(start))[:int(size):int(size)]
}

func (mb MemoryBuffer) Dispose() {
	purego.SyscallN(symDisposeMemoryBuffer, mb.C)
}

// ---- Bitcode ----

var writeBitcodeToFileErr = errors.New("Failed to write bitcode to file")

func WriteBitcodeToFile(m Module, file *os.File) error {
	r, _, _ := purego.SyscallN(symWriteBitcodeToFD, m.C, uintptr(file.Fd()), 0, 0)
	if r != 0 {
		return writeBitcodeToFileErr
	}
	return nil
}

func WriteBitcodeToMemoryBuffer(m Module) MemoryBuffer {
	r, _, _ := purego.SyscallN(symWriteBitcodeToMemBuf, m.C)
	return MemoryBuffer{C: r}
}

func WriteThinLTOBitcodeToMemoryBuffer(m Module) MemoryBuffer {
	if symGoWriteThinLTOBitcode == 0 {
		return WriteBitcodeToMemoryBuffer(m)
	}
	r, _, _ := purego.SyscallN(symGoWriteThinLTOBitcode, m.C)
	return MemoryBuffer{C: r}
}

func (c Context) ParseBitcodeFile(name string) (Module, error) {
	cname := cString(name)
	var membuf uintptr
	var errMsg uintptr
	r, _, _ := purego.SyscallN(symCreateMemBufWithFile, cname, uintptr(unsafe.Pointer(&membuf)), uintptr(unsafe.Pointer(&errMsg)))
	runtime.KeepAlive(name)
	if r != 0 {
		s := goString(errMsg)
		purego.SyscallN(symDisposeMessage, errMsg)
		return Module{}, fmt.Errorf("read %s: %s", name, s)
	}

	var mod uintptr
	r, _, _ = purego.SyscallN(symParseBitcodeInCtx2, c.C, membuf, uintptr(unsafe.Pointer(&mod)))
	if r != 0 {
		return Module{}, fmt.Errorf("parse bitcode %s failed", name)
	}
	return Module{mod}, nil
}

// ---- Module linking ----

func LinkModules(dest, src Module) error {
	r, _, _ := purego.SyscallN(symLinkModules2, dest.C, src.C)
	if r != 0 {
		return fmt.Errorf("link modules failed")
	}
	return nil
}

// ---- PassManager ----

func NewPassManager() PassManager {
	r, _, _ := purego.SyscallN(symCreatePassManager)
	return PassManager{r}
}

func (pm PassManager) Dispose() {
	purego.SyscallN(symDisposePassManager, pm.C)
}

func (pm PassManager) Run(m Module) bool {
	r, _, _ := purego.SyscallN(symRunPassManager, pm.C, m.C)
	return r != 0
}
