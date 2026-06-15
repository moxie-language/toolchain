package llvmpure

import (
	"fmt"
	"os"
	"unsafe"

	"github.com/ebitengine/purego"
)

type (
	Context      struct{ ref uintptr }
	Module       struct{ ref uintptr }
	Type         struct{ ref uintptr }
	Value        struct{ ref uintptr }
	BasicBlock   struct{ ref uintptr }
	Builder      struct{ ref uintptr }
	Metadata     struct{ ref uintptr }
	Attribute    struct{ ref uintptr }
	Comdat       struct{ ref uintptr }
	MemoryBuffer struct{ ref uintptr }
	PassManager  struct{ ref uintptr }
	Use          struct{ ref uintptr }
)

const Version = "19.1.0"

type DebugLoc struct {
	Line, Col uint
	Scope     Metadata
	InlinedAt Metadata
}

func (c Context) IsNil() bool      { return c.ref == 0 }
func (m Module) IsNil() bool       { return m.ref == 0 }
func (t Type) IsNil() bool         { return t.ref == 0 }
func (v Value) IsNil() bool        { return v.ref == 0 }
func (b BasicBlock) IsNil() bool   { return b.ref == 0 }
func (b Builder) IsNil() bool      { return b.ref == 0 }
func (m Metadata) IsNil() bool     { return m.ref == 0 }
func (a Attribute) IsNil() bool    { return a.ref == 0 }
func (c Comdat) IsNil() bool       { return c.ref == 0 }
func (m MemoryBuffer) IsNil() bool { return m.ref == 0 }
func (p PassManager) IsNil() bool  { return p.ref == 0 }
func (u Use) IsNil() bool          { return u.ref == 0 }

type TypeKind int

const (
	VoidTypeKind      TypeKind = 0
	HalfTypeKind      TypeKind = 1
	FloatTypeKind     TypeKind = 2
	DoubleTypeKind    TypeKind = 3
	X86_FP80TypeKind  TypeKind = 4
	FP128TypeKind     TypeKind = 5
	PPC_FP128TypeKind TypeKind = 6
	LabelTypeKind     TypeKind = 7
	IntegerTypeKind   TypeKind = 8
	FunctionTypeKind  TypeKind = 9
	StructTypeKind    TypeKind = 10
	ArrayTypeKind     TypeKind = 11
	PointerTypeKind   TypeKind = 12
	VectorTypeKind    TypeKind = 13
	MetadataTypeKind  TypeKind = 14
	TokenTypeKind     TypeKind = 16
)

type Opcode int

const (
	Ret         Opcode = 1
	Br          Opcode = 2
	Switch      Opcode = 3
	IndirectBr  Opcode = 4
	Invoke      Opcode = 5
	Unreachable Opcode = 7
	Add         Opcode = 8
	FAdd        Opcode = 9
	Sub         Opcode = 10
	FSub        Opcode = 11
	Mul         Opcode = 12
	FMul        Opcode = 13
	UDiv        Opcode = 14
	SDiv        Opcode = 15
	FDiv        Opcode = 16
	URem        Opcode = 17
	SRem        Opcode = 18
	FRem        Opcode = 19
	Shl         Opcode = 20
	LShr        Opcode = 21
	AShr        Opcode = 22
	And         Opcode = 23
	Or          Opcode = 24
	Xor         Opcode = 25
	Alloca      Opcode = 26
	Load        Opcode = 27
	Store       Opcode = 28
	GEP         Opcode = 29
	Trunc       Opcode = 30
	ZExt        Opcode = 31
	SExt        Opcode = 32
	FPToUI      Opcode = 33
	FPToSI      Opcode = 34
	UIToFP      Opcode = 35
	SIToFP      Opcode = 36
	FPTrunc     Opcode = 37
	FPExt       Opcode = 38
	PtrToInt    Opcode = 39
	IntToPtr    Opcode = 40
	BitCast     Opcode = 41
	ICmp        Opcode = 42
	FCmp        Opcode = 43
	PHI         Opcode = 44
	Call        Opcode = 45
	Select      Opcode = 46
	VAArg       Opcode = 49
	ExtractElement Opcode = 50
	InsertElement  Opcode = 51
	ShuffleVector  Opcode = 52
	ExtractValue   Opcode = 53
	InsertValue    Opcode = 54
	Resume         Opcode = 58
	LandingPad     Opcode = 59
	CleanupRet     Opcode = 61
	CatchRet       Opcode = 62
	CatchPad       Opcode = 63
	CleanupPad     Opcode = 64
	CatchSwitch    Opcode = 65
)

type Linkage int

const (
	ExternalLinkage            Linkage = 0
	AvailableExternallyLinkage Linkage = 1
	LinkOnceAnyLinkage         Linkage = 2
	LinkOnceODRLinkage         Linkage = 3
	WeakAnyLinkage             Linkage = 5
	WeakODRLinkage             Linkage = 6
	AppendingLinkage           Linkage = 7
	InternalLinkage            Linkage = 8
	PrivateLinkage             Linkage = 9
	ExternalWeakLinkage        Linkage = 12
	CommonLinkage              Linkage = 14
)

type Visibility int

const (
	DefaultVisibility   Visibility = 0
	HiddenVisibility    Visibility = 1
	ProtectedVisibility Visibility = 2
)

type CallConv int

const (
	CCallConv           CallConv = 0
	FastCallConv        CallConv = 8
	ColdCallConv        CallConv = 9
	X86StdcallCallConv  CallConv = 64
	X86FastcallCallConv CallConv = 65
)

type IntPredicate int

const (
	IntEQ  IntPredicate = 32
	IntNE  IntPredicate = 33
	IntUGT IntPredicate = 34
	IntUGE IntPredicate = 35
	IntULT IntPredicate = 36
	IntULE IntPredicate = 37
	IntSGT IntPredicate = 38
	IntSGE IntPredicate = 39
	IntSLT IntPredicate = 40
	IntSLE IntPredicate = 41
)

type FloatPredicate int

const (
	FloatPredicateFalse FloatPredicate = 0
	FloatOEQ            FloatPredicate = 1
	FloatOGT            FloatPredicate = 2
	FloatOGE            FloatPredicate = 3
	FloatOLT            FloatPredicate = 4
	FloatOLE            FloatPredicate = 5
	FloatONE            FloatPredicate = 6
	FloatORD            FloatPredicate = 7
	FloatUNO            FloatPredicate = 8
	FloatUEQ            FloatPredicate = 9
	FloatUGT            FloatPredicate = 10
	FloatUGE            FloatPredicate = 11
	FloatULT            FloatPredicate = 12
	FloatULE            FloatPredicate = 13
	FloatUNE            FloatPredicate = 14
	FloatPredicateTrue  FloatPredicate = 15
)

type AtomicRMWBinOp int

const (
	AtomicRMWBinOpXchg AtomicRMWBinOp = 0
	AtomicRMWBinOpAdd  AtomicRMWBinOp = 1
	AtomicRMWBinOpSub  AtomicRMWBinOp = 2
	AtomicRMWBinOpAnd  AtomicRMWBinOp = 3
	AtomicRMWBinOpNand AtomicRMWBinOp = 4
	AtomicRMWBinOpOr   AtomicRMWBinOp = 5
	AtomicRMWBinOpXor  AtomicRMWBinOp = 6
	AtomicRMWBinOpMax  AtomicRMWBinOp = 7
	AtomicRMWBinOpMin  AtomicRMWBinOp = 8
	AtomicRMWBinOpUMax AtomicRMWBinOp = 9
	AtomicRMWBinOpUMin AtomicRMWBinOp = 10
)

type AtomicOrdering int

const (
	AtomicOrderingNotAtomic              AtomicOrdering = 0
	AtomicOrderingUnordered              AtomicOrdering = 1
	AtomicOrderingMonotonic              AtomicOrdering = 2
	AtomicOrderingAcquire                AtomicOrdering = 4
	AtomicOrderingRelease                AtomicOrdering = 5
	AtomicOrderingAcquireRelease         AtomicOrdering = 6
	AtomicOrderingSequentiallyConsistent AtomicOrdering = 7
)

type ComdatSelectionKind int

const (
	AnyComdatSelectionKind           ComdatSelectionKind = 0
	ExactMatchComdatSelectionKind    ComdatSelectionKind = 1
	LargestComdatSelectionKind       ComdatSelectionKind = 2
	NoDeduplicateComdatSelectionKind ComdatSelectionKind = 3
	SameSizeComdatSelectionKind      ComdatSelectionKind = 4
)

type LandingPadClause int

const (
	LandingPadCatch  LandingPadClause = 0
	LandingPadFilter LandingPadClause = 1
)

type InlineAsmDialect int

const (
	InlineAsmDialectATT   InlineAsmDialect = 0
	InlineAsmDialectIntel InlineAsmDialect = 1
)

var (
	libLLVM  uintptr
	libGlue  uintptr
	initDone bool
	initErr  error
)

func Init(llvmPath, gluePath string) error {
	if initDone {
		return initErr
	}
	initDone = true
	var err error
	libLLVM, err = purego.Dlopen(llvmPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		initErr = fmt.Errorf("dlopen %s: %w", llvmPath, err)
		return initErr
	}
	if gluePath != "" {
		libGlue, err = purego.Dlopen(gluePath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			initErr = fmt.Errorf("dlopen %s: %w", gluePath, err)
			return initErr
		}
	}
	registerAll()
	return nil
}

func mustSym(lib uintptr, name string) uintptr {
	sym, err := purego.Dlsym(lib, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "llvmpure: missing symbol %s: %v\n", name, err)
		os.Exit(1)
	}
	return sym
}

func trySym(lib uintptr, name string) uintptr {
	sym, _ := purego.Dlsym(lib, name)
	return sym
}

func goString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	p := (*byte)(unsafe.Pointer(ptr))
	n := 0
	for {
		if *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + uintptr(n))) == 0 {
			break
		}
		n++
	}
	return string(unsafe.Slice(p, n))
}

func cString(s string) uintptr {
	b := append([]byte(s), 0)
	return uintptr(unsafe.Pointer(&b[0]))
}

func boolToInt(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

func typeRefPtr(types []Type) uintptr {
	if len(types) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&types[0]))
}

func valueRefPtr(values []Value) uintptr {
	if len(values) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&values[0]))
}

func metadataRefPtr(mds []Metadata) uintptr {
	if len(mds) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&mds[0]))
}
