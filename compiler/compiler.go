package compiler

import (
	"debug/dwarf"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math/bits"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"moxie/compiler/llvmutil"
	"moxie/loader"
	"moxie/mxssa"
	"moxie/ssabridge"
	"moxie/src/moxie"
	"moxie/typecheck"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/types/typeutil"
	"tinygo.org/x/go-llvm"
)

func init() {
	llvm.InitializeAllTargets()
	llvm.InitializeAllTargetMCs()
	llvm.InitializeAllTargetInfos()
	llvm.InitializeAllAsmParsers()
	llvm.InitializeAllAsmPrinters()
}

// Config is the configuration for the compiler. Most settings should be copied
// directly from compileopts.Config, it recreated here to decouple the compiler
// package a bit and because it makes caching easier.
//
// This struct can be used for caching: if one of the flags here changes the
// code must be recompiled.
type Config struct {
	// Target and output information.
	Triple          string
	CPU             string
	Features        string
	ABI             string
	GOOS            string
	GOARCH          string
	BuildMode       string
	CodeModel       string
	RelocationModel string
	SizeLevel       int
	MoxieVersion    string // for llvm.ident

	// Various compiler options that determine how code is generated.
	Scheduler          string
	AutomaticStackSize bool
	DefaultStackSize   uint64
	MaxStackAlloc      uint64
	NeedsStackObjects  bool
	Debug              bool // Whether to emit debug information in the LLVM module.
	Nobounds           bool // Whether to skip bounds checks
	PanicStrategy      string
}

// compilerContext contains function-independent data that should still be
// available while compiling every function. It is not strictly read-only, but
// must not contain function-dependent data such as an IR builder.
type compilerContext struct {
	*Config
	DumpSSA          bool
	mod              llvm.Module
	ctx              llvm.Context
	builder          llvm.Builder // only used for constant operations
	dibuilder        *llvm.DIBuilder
	cu               llvm.Metadata
	difiles          map[string]llvm.Metadata
	ditypes          map[types.Type]llvm.Metadata
	llvmTypes        typeutil.Map
	interfaceTypes   typeutil.Map
	machine          llvm.TargetMachine
	targetData       llvm.TargetData
	intType          llvm.Type
	dataPtrType      llvm.Type // pointer in address space 0
	funcPtrType      llvm.Type // pointer in function address space (1 for AVR, 0 elsewhere)
	funcPtrAddrSpace int
	uintptrType      llvm.Type
	program          *ssa.Program
	mxProgram        *mxssa.Program   // B3 bridge: native SSA, populated by ssabridge
	mxTranslator     *ssabridge.Translator
	diagnostics      []error
	functionInfos    map[*mxssa.Function]functionInfo
	astComments      map[string]*ast.CommentGroup
	embedGlobals     map[string][]*loader.EmbedFile
	pkg              *types.Package
	mxPkg            *typecheck.Package // B3 bridge: native types for current package
	tcLLVMTypes      map[typecheck.Type]llvm.Type // B3 bridge: typecheck.Type → LLVM cache
	packageDir       string // directory for this package
	runtimePkg       *types.Package
}

// newCompilerContext returns a new compiler context ready for use, most
// importantly with a newly created LLVM context and module.
func newCompilerContext(moduleName string, machine llvm.TargetMachine, config *Config, dumpSSA bool) *compilerContext {
	c := &compilerContext{
		Config:        config,
		DumpSSA:       dumpSSA,
		difiles:       make(map[string]llvm.Metadata),
		ditypes:       make(map[types.Type]llvm.Metadata),
		machine:       machine,
		targetData:    machine.CreateTargetData(),
		functionInfos: map[*mxssa.Function]functionInfo{},
		astComments:   map[string]*ast.CommentGroup{},
		tcLLVMTypes:   map[typecheck.Type]llvm.Type{},
	}

	c.ctx = llvm.NewContext()
	c.builder = c.ctx.NewBuilder()
	c.mod = c.ctx.NewModule(moduleName)
	c.mod.SetTarget(config.Triple)
	c.mod.SetDataLayout(c.targetData.String())
	if c.Debug {
		c.dibuilder = llvm.NewDIBuilder(c.mod)
	}

	c.uintptrType = c.ctx.IntType(c.targetData.PointerSize() * 8)
	// Moxie: int is always 32 bits on all targets. Slice headers still use
	// uintptr-sized len/cap; trunc/ext happens at the int boundary.
	c.intType = c.ctx.Int32Type()
	c.dataPtrType = llvm.PointerType(c.ctx.Int8Type(), 0)

	dummyFuncType := llvm.FunctionType(c.ctx.VoidType(), nil, false)
	dummyFunc := llvm.AddFunction(c.mod, "moxie.dummy", dummyFuncType)
	c.funcPtrAddrSpace = dummyFunc.Type().PointerAddressSpace()
	c.funcPtrType = dummyFunc.Type()
	dummyFunc.EraseFromParentAsFunction()

	return c
}

// Dispose everything related to the context, _except_ for the IR module (and
// the associated context).
func (c *compilerContext) dispose() {
	c.builder.Dispose()
}

// builder contains all information relevant to build a single function.
type builder struct {
	*compilerContext
	llvm.Builder
	fn                *ssa.Function
	mxFn              *mxssa.Function          // B3 bridge: native SSA function
	llvmFnType        llvm.Type
	llvmFn            llvm.Value
	info              functionInfo
	mxLocals          map[mxssa.Value]llvm.Value // local variables (TC)
	blockInfo         []blockInfo
	currentBlock      *mxssa.BasicBlock
	currentBlockInfo  *blockInfo
	tarjanStack       []uint
	tarjanIndex       uint
	mxPhis            []phiNodeTC
	deferPtr          llvm.Value
	deferFrame        llvm.Value
	stackChainAlloca  llvm.Value
	landingpad        llvm.BasicBlock
	difunc            llvm.Metadata
	dilocals          map[*types.Var]llvm.Metadata
	initInlinedAt     llvm.Metadata            // fake inlinedAt position
	initPseudoFuncs   map[string]llvm.Metadata // fake "inlined" functions for proper init debug locations
	allDeferFuncs     []interface{}
	deferFuncs        map[*mxssa.Function]int
	deferInvokeFuncs  map[string]int
	deferClosureFuncs map[*mxssa.Function]int
	deferExprFuncs    map[mxssa.Value]int
	selectRecvBufTC   map[*mxssa.Select]llvm.Value
	deferBuiltinFuncs map[mxssa.Value]deferBuiltin
	runDefersBlock    []llvm.BasicBlock
	afterDefersBlock  []llvm.BasicBlock
}

func newBuilder(c *compilerContext, irbuilder llvm.Builder, f *ssa.Function) *builder {
	mxFn := c.mxTranslator.EnsureFunction(f)
	fnType, fn := c.getFunction(mxFn)
	b := &builder{
		compilerContext: c,
		Builder:         irbuilder,
		fn:              f,
		mxFn:            mxFn,
		llvmFnType:      fnType,
		llvmFn:          fn,
		info:            c.getFunctionInfo(mxFn),
		mxLocals:        make(map[mxssa.Value]llvm.Value),
		dilocals:        make(map[*types.Var]llvm.Metadata),
	}
	return b
}

type blockInfo struct {
	// entry is the LLVM basic block corresponding to the start of this *ssa.Block.
	entry llvm.BasicBlock

	// exit is the LLVM basic block corresponding to the end of this *ssa.Block.
	// It will be different than entry if any of the block's instructions contain internal branches.
	exit llvm.BasicBlock

	// tarjan holds state for applying Tarjan's strongly connected components algorithm to the CFG.
	// This is used by defer.go to determine whether to stack- or heap-allocate defer data.
	tarjan tarjanNode
}

type deferBuiltin struct {
	callName string
	pos      token.Pos
	argTypes []typecheck.Type
	callback int
}

type phiNodeTC struct {
	phi  *mxssa.Phi
	llvm llvm.Value
}

// NewTargetMachine returns a new llvm.TargetMachine based on the passed-in
// configuration. It is used by the compiler and is needed for machine code
// emission.
func NewTargetMachine(config *Config) (llvm.TargetMachine, error) {
	target, err := llvm.GetTargetFromTriple(config.Triple)
	if err != nil {
		return llvm.TargetMachine{}, err
	}

	var codeModel llvm.CodeModel
	var relocationModel llvm.RelocMode

	switch config.CodeModel {
	case "default":
		codeModel = llvm.CodeModelDefault
	case "tiny":
		codeModel = llvm.CodeModelTiny
	case "small":
		codeModel = llvm.CodeModelSmall
	case "kernel":
		codeModel = llvm.CodeModelKernel
	case "medium":
		codeModel = llvm.CodeModelMedium
	case "large":
		codeModel = llvm.CodeModelLarge
	}

	switch config.RelocationModel {
	case "static":
		relocationModel = llvm.RelocStatic
	case "pic":
		relocationModel = llvm.RelocPIC
	case "dynamicnopic":
		relocationModel = llvm.RelocDynamicNoPic
	}

	machine := target.CreateTargetMachine(config.Triple, config.CPU, config.Features, llvm.CodeGenLevelDefault, relocationModel, codeModel)
	return machine, nil
}

// Sizes returns a types.Sizes appropriate for the given target machine. It
// includes the correct int size and alignment as is necessary for the Go
// typechecker.
func Sizes(machine llvm.TargetMachine) types.Sizes {
	targetData := machine.CreateTargetData()
	defer targetData.Dispose()

	// Moxie: int is always 32 bits on all targets.
	intWidth := 32

	// Construct a complex128 type because that's likely the type with the
	// biggest alignment on most/all ABIs.
	ctx := llvm.NewContext()
	defer ctx.Dispose()
	complex128Type := ctx.StructType([]llvm.Type{ctx.DoubleType(), ctx.DoubleType()}, false)
	return &stdSizes{
		IntSize:  int64(intWidth / 8),
		PtrSize:  int64(targetData.PointerSize()),
		MaxAlign: int64(targetData.ABITypeAlignment(complex128Type)),
	}
}

// CompilePackage compiles a single package to a LLVM module.
func CompilePackage(moduleName string, pkg *loader.Package, ssaPkg *ssa.Package, machine llvm.TargetMachine, config *Config, dumpSSA bool) (llvm.Module, []error) {
	c := newCompilerContext(moduleName, machine, config, dumpSSA)
	defer c.dispose()
	c.packageDir = pkg.OriginalDir()
	c.embedGlobals = pkg.EmbedGlobals
	c.pkg = pkg.Pkg
	c.runtimePkg = ssaPkg.Prog.ImportedPackage("runtime").Pkg
	c.program = ssaPkg.Prog

	// Convert AST to SSA.
	ssaPkg.Build()

	// B3 bridge: build mxssa.Program from ssa.Program for migrated files.
	typePkgs := map[string]*types.Package{}
	for _, p := range ssaPkg.Prog.AllPackages() {
		typePkgs[p.Pkg.Path()] = p.Pkg
	}
	mxProg, mxTrans := ssabridge.Translate(ssaPkg.Prog.Fset, ssaPkg.Prog, typePkgs)
	c.mxProgram = mxProg
	c.mxTranslator = mxTrans
	c.mxTranslator.TranslatePackage(ssaPkg)
	// Pre-translate runtime and internal/task so getPackageFunction can resolve
	// them via mxProgram.ImportedPackage. TranslatePackage is idempotent.
	for _, importPath := range []string{"runtime", "internal/task"} {
		if rt := ssaPkg.Prog.ImportedPackage(importPath); rt != nil {
			c.mxTranslator.TranslatePackage(rt)
		}
	}
	if mxP := mxProg.ImportedPackage(pkg.Pkg.Path()); mxP != nil {
		c.mxPkg = mxP.Pkg
	}
	c.validateBridgeTypes()

	// Initialize debug information.
	if c.Debug {
		c.cu = c.dibuilder.CreateCompileUnit(llvm.DICompileUnit{
			Language:  0xb, // DW_LANG_C99 (0xc, off-by-one?)
			File:      "<unknown>",
			Dir:       "",
			Producer:  "Moxie",
			Optimized: true,
		})
	}

	// Load comments such as //go:extern on globals.
	c.loadASTComments(pkg)

	// Predeclare the runtime.alloc function, which is used by the wordpack
	// functionality.
	c.getRuntimeFunction("alloc")
	if c.NeedsStackObjects {
		// Predeclare trackPointer, which is used everywhere we use runtime.alloc.
		c.getRuntimeFunction("trackPointer")
	}

	// Compile all functions, methods, and global variables in this package.
	irbuilder := c.ctx.NewBuilder()
	defer irbuilder.Dispose()
	c.createPackage(irbuilder, ssaPkg)

	// see: https://reviews.llvm.org/D18355
	if c.Debug {
		c.mod.AddNamedMetadataOperand("llvm.module.flags",
			c.ctx.MDNode([]llvm.Metadata{
				llvm.ConstInt(c.ctx.Int32Type(), 2, false).ConstantAsMetadata(), // Warning on mismatch
				c.ctx.MDString("Debug Info Version"),
				llvm.ConstInt(c.ctx.Int32Type(), 3, false).ConstantAsMetadata(), // DWARF version
			}),
		)
		c.mod.AddNamedMetadataOperand("llvm.module.flags",
			c.ctx.MDNode([]llvm.Metadata{
				llvm.ConstInt(c.ctx.Int32Type(), 7, false).ConstantAsMetadata(), // Max on mismatch
				c.ctx.MDString("Dwarf Version"),
				llvm.ConstInt(c.ctx.Int32Type(), 4, false).ConstantAsMetadata(),
			}),
		)
		if c.MoxieVersion != "" {
			// It is necessary to set llvm.ident, otherwise debugging on MacOS
			// won't work.
			c.mod.AddNamedMetadataOperand("llvm.ident",
				c.ctx.MDNode(([]llvm.Metadata{
					c.ctx.MDString("Moxie version " + c.MoxieVersion),
				})))
		}
		c.dibuilder.Finalize()
		c.dibuilder.Destroy()
	}

	// Add the "target-abi" flag, which is necessary on RISC-V otherwise it will
	// pick one that doesn't match the -mabi Clang flag.
	if c.ABI != "" {
		c.mod.AddNamedMetadataOperand("llvm.module.flags",
			c.ctx.MDNode([]llvm.Metadata{
				llvm.ConstInt(c.ctx.Int32Type(), 1, false).ConstantAsMetadata(), // Error on mismatch
				c.ctx.MDString("target-abi"),
				c.ctx.MDString(c.ABI),
			}),
		)
	}

	return c.mod, c.diagnostics
}

func (c *compilerContext) getRuntimeType(name string) types.Type {
	return c.runtimePkg.Scope().Lookup(name).(*types.TypeName).Type()
}

// getLLVMRuntimeType obtains a named type from the runtime package and returns
// it as a LLVM type, creating it if necessary. It is a shorthand for
// getLLVMType(getRuntimeType(name)).
func (c *compilerContext) getLLVMRuntimeType(name string) llvm.Type {
	return c.getLLVMType(c.getRuntimeType(name))
}

// getLLVMType returns a LLVM type for a Go type. It doesn't recreate already
// created types. This is somewhat important for performance, but especially
// important for named struct types (which should only be created once).
func (c *compilerContext) getLLVMType(goType types.Type) llvm.Type {
	// Try to load the LLVM type from the cache.
	// Note: *types.Named isn't unique when working with generics.
	// See https://github.com/golang/go/issues/53914
	// This is the reason for using typeutil.Map to lookup LLVM types for Go types.
	ival := c.llvmTypes.At(goType)
	if ival != nil {
		return ival.(llvm.Type)
	}
	// Not already created, so adding this type to the cache.
	llvmType := c.makeLLVMType(goType)
	c.llvmTypes.Set(goType, llvmType)
	return llvmType
}

// makeLLVMType creates a LLVM type for a Go type. Don't call this, use
// getLLVMType instead.
func (c *compilerContext) makeLLVMType(goType types.Type) llvm.Type {
	switch typ := types.Unalias(goType).(type) {
	case *types.Array:
		elemType := c.getLLVMType(typ.Elem())
		return llvm.ArrayType(elemType, int(typ.Len()))
	case *types.Basic:
		switch typ.Kind() {
		case types.Bool, types.UntypedBool:
			return c.ctx.Int1Type()
		case types.Int8, types.Uint8:
			return c.ctx.Int8Type()
		case types.Int16, types.Uint16:
			return c.ctx.Int16Type()
		case types.Int32, types.Uint32:
			return c.ctx.Int32Type()
		case types.Int, types.Uint:
			return c.intType
		case types.Int64, types.Uint64:
			return c.ctx.Int64Type()
		case types.Float32:
			return c.ctx.FloatType()
		case types.Float64:
			return c.ctx.DoubleType()
		case types.Complex64:
			return c.ctx.StructType([]llvm.Type{c.ctx.FloatType(), c.ctx.FloatType()}, false)
		case types.Complex128:
			return c.ctx.StructType([]llvm.Type{c.ctx.DoubleType(), c.ctx.DoubleType()}, false)
		case types.String, types.UntypedString:
			return c.getLLVMRuntimeType("_string")
		case types.Uintptr:
			return c.uintptrType
		case types.UnsafePointer:
			return c.dataPtrType
		default:
			panic("unknown basic type: " + typ.String())
		}
	case *types.Chan, *types.Map, *types.Pointer:
		return c.dataPtrType // all pointers are the same
	case *types.Interface:
		return c.getLLVMRuntimeType("_interface")
	case *types.Named:
		if st, ok := typ.Underlying().(*types.Struct); ok {
			// Structs are a special case. While other named types are ignored
			// in LLVM IR, named structs are implemented as named structs in
			// LLVM. This is because it is otherwise impossible to create
			// self-referencing types such as linked lists.
			llvmName := typ.String()
			// Reuse existing named type from the module to keep identity
			// consistent with getLLVMTypeTC.
			existing := c.mod.GetTypeByName(llvmName)
			if !existing.IsNil() {
				c.llvmTypes.Set(goType, existing)
				return existing
			}
			llvmType := c.ctx.StructCreateNamed(llvmName)
			c.llvmTypes.Set(goType, llvmType) // avoid infinite recursion
			underlying := c.getLLVMType(st)
			llvmType.StructSetBody(underlying.StructElementTypes(), false)
			return llvmType
		}
		return c.getLLVMType(typ.Underlying())
	case *types.Signature: // function value
		return c.funcValueType()
	case *types.Slice:
		// Moxie: []byte uses the named _string type (string=[]byte unification).
		if basic, ok := typ.Elem().(*types.Basic); ok && basic.Kind() == types.Byte {
			return c.getLLVMRuntimeType("_string")
		}
		members := []llvm.Type{
			c.dataPtrType,
			c.uintptrType, // len
			c.uintptrType, // cap
		}
		return c.ctx.StructType(members, false)
	case *types.Struct:
		members := make([]llvm.Type, typ.NumFields())
		for i := 0; i < typ.NumFields(); i++ {
			members[i] = c.getLLVMType(typ.Field(i).Type())
		}
		return c.ctx.StructType(members, false)
	case *types.TypeParam:
		return c.getLLVMType(typ.Underlying())
	case *types.Tuple:
		members := make([]llvm.Type, typ.Len())
		for i := 0; i < typ.Len(); i++ {
			members[i] = c.getLLVMType(typ.At(i).Type())
		}
		return c.ctx.StructType(members, false)
	default:
		panic("unknown type: " + goType.String())
	}
}

// getDITypeForGlobal returns the DWARF type for a global variable's element
// type. Wraps the SSA-side resolution so symbol.go can stay
// go/types-import-free until the createDIType chain (§3) migrates.
func (c *compilerContext) getDITypeForGlobal(g *mxssa.Global) llvm.Metadata {
	if goG := c.mxTranslator.ReverseGlobal(g); goG != nil {
		if ptr, ok := goG.Type().(*types.Pointer); ok {
			return c.getDIType(ptr.Elem())
		}
	}
	return llvm.Metadata{}
}

// Get all methods of a type.
func getAllMethods(prog *ssa.Program, typ types.Type) []*types.Selection {
	ms := prog.MethodSets.MethodSet(typ)
	methods := make([]*types.Selection, ms.Len())
	for i := 0; i < ms.Len(); i++ {
		methods[i] = ms.At(i)
	}
	return methods
}

// Is this a pointer type of some sort? Can be unsafe.Pointer or any *T pointer.
func isPointer(typ types.Type) bool {
	if _, ok := typ.(*types.Pointer); ok {
		return true
	} else if typ, ok := typ.(*types.Basic); ok && typ.Kind() == types.UnsafePointer {
		return true
	} else {
		return false
	}
}

// Get the DWARF type for this Go type.
func (c *compilerContext) getDIType(typ types.Type) llvm.Metadata {
	if md, ok := c.ditypes[typ]; ok {
		return md
	}
	md := c.createDIType(typ)
	c.ditypes[typ] = md
	return md
}

// createDIType creates a new DWARF type. Don't call this function directly,
// call getDIType instead.
func (c *compilerContext) createDIType(typ types.Type) llvm.Metadata {
	llvmType := c.getLLVMType(typ)
	sizeInBytes := c.targetData.TypeAllocSize(llvmType)
	switch typ := typ.(type) {
	case *types.Alias:
		// Implement types.Alias just like types.Named: by treating them like a
		// C typedef.
		temporaryMDNode := c.dibuilder.CreateReplaceableCompositeType(llvm.Metadata{}, llvm.DIReplaceableCompositeType{
			Tag:         dwarf.TagTypedef,
			SizeInBits:  sizeInBytes * 8,
			AlignInBits: uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
		})
		c.ditypes[typ] = temporaryMDNode
		md := c.dibuilder.CreateTypedef(llvm.DITypedef{
			Type: c.getDIType(types.Unalias(typ)), // TODO: use typ.Rhs in Go 1.23
			Name: typ.String(),
		})
		temporaryMDNode.ReplaceAllUsesWith(md)
		return md
	case *types.Array:
		return c.dibuilder.CreateArrayType(llvm.DIArrayType{
			SizeInBits:  sizeInBytes * 8,
			AlignInBits: uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
			ElementType: c.getDIType(typ.Elem()),
			Subscripts: []llvm.DISubrange{
				{
					Lo:    0,
					Count: typ.Len(),
				},
			},
		})
	case *types.Basic:
		var encoding llvm.DwarfTypeEncoding
		if typ.Info()&types.IsBoolean != 0 {
			encoding = llvm.DW_ATE_boolean
		} else if typ.Info()&types.IsFloat != 0 {
			encoding = llvm.DW_ATE_float
		} else if typ.Info()&types.IsComplex != 0 {
			encoding = llvm.DW_ATE_complex_float
		} else if typ.Info()&types.IsUnsigned != 0 {
			encoding = llvm.DW_ATE_unsigned
		} else if typ.Info()&types.IsInteger != 0 {
			encoding = llvm.DW_ATE_signed
		} else if typ.Kind() == types.UnsafePointer {
			return c.dibuilder.CreatePointerType(llvm.DIPointerType{
				Name:         "unsafe.Pointer",
				SizeInBits:   c.targetData.TypeAllocSize(llvmType) * 8,
				AlignInBits:  uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
				AddressSpace: 0,
			})
		} else if typ.Info()&types.IsString != 0 {
			return c.dibuilder.CreateStructType(llvm.Metadata{}, llvm.DIStructType{
				Name:        "string",
				SizeInBits:  sizeInBytes * 8,
				AlignInBits: uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
				Elements: []llvm.Metadata{
					c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
						Name:         "ptr",
						SizeInBits:   c.targetData.TypeAllocSize(c.dataPtrType) * 8,
						AlignInBits:  uint32(c.targetData.ABITypeAlignment(c.dataPtrType)) * 8,
						OffsetInBits: 0,
						Type:         c.getDIType(types.NewPointer(types.Typ[types.Byte])),
					}),
					c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
						Name:         "len",
						SizeInBits:   c.targetData.TypeAllocSize(c.uintptrType) * 8,
						AlignInBits:  uint32(c.targetData.ABITypeAlignment(c.uintptrType)) * 8,
						OffsetInBits: c.targetData.ElementOffset(llvmType, 1) * 8,
						Type:         c.getDIType(types.Typ[types.Uintptr]),
					}),
					c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
						Name:         "cap",
						SizeInBits:   c.targetData.TypeAllocSize(c.uintptrType) * 8,
						AlignInBits:  uint32(c.targetData.ABITypeAlignment(c.uintptrType)) * 8,
						OffsetInBits: c.targetData.ElementOffset(llvmType, 2) * 8,
						Type:         c.getDIType(types.Typ[types.Uintptr]),
					}),
				},
			})
		} else {
			panic("unknown basic type")
		}
		return c.dibuilder.CreateBasicType(llvm.DIBasicType{
			Name:       typ.String(),
			SizeInBits: sizeInBytes * 8,
			Encoding:   encoding,
		})
	case *types.Chan:
		return c.getDIType(types.NewPointer(c.getRuntimeType("channel")))
	case *types.Interface:
		return c.getDIType(c.getRuntimeType("_interface"))
	case *types.Map:
		return c.getDIType(types.NewPointer(c.getRuntimeType("hashmap")))
	case *types.Named:
		// Placeholder metadata node, to be replaced afterwards.
		temporaryMDNode := c.dibuilder.CreateReplaceableCompositeType(llvm.Metadata{}, llvm.DIReplaceableCompositeType{
			Tag:         dwarf.TagTypedef,
			SizeInBits:  sizeInBytes * 8,
			AlignInBits: uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
		})
		c.ditypes[typ] = temporaryMDNode
		md := c.dibuilder.CreateTypedef(llvm.DITypedef{
			Type: c.getDIType(typ.Underlying()),
			Name: typ.String(),
		})
		temporaryMDNode.ReplaceAllUsesWith(md)
		return md
	case *types.Pointer:
		return c.dibuilder.CreatePointerType(llvm.DIPointerType{
			Pointee:      c.getDIType(typ.Elem()),
			SizeInBits:   c.targetData.TypeAllocSize(llvmType) * 8,
			AlignInBits:  uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
			AddressSpace: 0,
		})
	case *types.Signature:
		// actually a closure
		fields := llvmType.StructElementTypes()
		return c.dibuilder.CreateStructType(llvm.Metadata{}, llvm.DIStructType{
			SizeInBits:  sizeInBytes * 8,
			AlignInBits: uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
			Elements: []llvm.Metadata{
				c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
					Name:         "context",
					SizeInBits:   c.targetData.TypeAllocSize(fields[1]) * 8,
					AlignInBits:  uint32(c.targetData.ABITypeAlignment(fields[1])) * 8,
					OffsetInBits: 0,
					Type:         c.getDIType(types.Typ[types.UnsafePointer]),
				}),
				c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
					Name:         "fn",
					SizeInBits:   c.targetData.TypeAllocSize(fields[0]) * 8,
					AlignInBits:  uint32(c.targetData.ABITypeAlignment(fields[0])) * 8,
					OffsetInBits: c.targetData.ElementOffset(llvmType, 1) * 8,
					Type:         c.getDIType(types.Typ[types.UnsafePointer]),
				}),
			},
		})
	case *types.Slice:
		fields := llvmType.StructElementTypes()
		return c.dibuilder.CreateStructType(llvm.Metadata{}, llvm.DIStructType{
			Name:        typ.String(),
			SizeInBits:  sizeInBytes * 8,
			AlignInBits: uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
			Elements: []llvm.Metadata{
				c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
					Name:         "ptr",
					SizeInBits:   c.targetData.TypeAllocSize(fields[0]) * 8,
					AlignInBits:  uint32(c.targetData.ABITypeAlignment(fields[0])) * 8,
					OffsetInBits: 0,
					Type:         c.getDIType(types.NewPointer(typ.Elem())),
				}),
				c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
					Name:         "len",
					SizeInBits:   c.targetData.TypeAllocSize(c.uintptrType) * 8,
					AlignInBits:  uint32(c.targetData.ABITypeAlignment(c.uintptrType)) * 8,
					OffsetInBits: c.targetData.ElementOffset(llvmType, 1) * 8,
					Type:         c.getDIType(types.Typ[types.Uintptr]),
				}),
				c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
					Name:         "cap",
					SizeInBits:   c.targetData.TypeAllocSize(c.uintptrType) * 8,
					AlignInBits:  uint32(c.targetData.ABITypeAlignment(c.uintptrType)) * 8,
					OffsetInBits: c.targetData.ElementOffset(llvmType, 2) * 8,
					Type:         c.getDIType(types.Typ[types.Uintptr]),
				}),
			},
		})
	case *types.Struct:
		elements := make([]llvm.Metadata, typ.NumFields())
		for i := range elements {
			field := typ.Field(i)
			fieldType := field.Type()
			llvmField := c.getLLVMType(fieldType)
			elements[i] = c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
				Name:         field.Name(),
				SizeInBits:   c.targetData.TypeAllocSize(llvmField) * 8,
				AlignInBits:  uint32(c.targetData.ABITypeAlignment(llvmField)) * 8,
				OffsetInBits: c.targetData.ElementOffset(llvmType, i) * 8,
				Type:         c.getDIType(fieldType),
			})
		}
		md := c.dibuilder.CreateStructType(llvm.Metadata{}, llvm.DIStructType{
			SizeInBits:  sizeInBytes * 8,
			AlignInBits: uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
			Elements:    elements,
		})
		return md
	case *types.TypeParam:
		return c.getDIType(typ.Underlying())
	default:
		panic("unknown type while generating DWARF debug type: " + typ.String())
	}
}

// setDebugLocation sets the current debug location for the builder.
func (b *builder) setDebugLocation(pos token.Pos) {
	if pos == token.NoPos {
		// No debug information available for this instruction.
		b.SetCurrentDebugLocation(0, 0, b.difunc, llvm.Metadata{})
		return
	}

	position := b.mxProgram.Fset.Position(pos)
	if b.mxFn.Synthetic == "package initializer" {
		// Package initializers are treated specially, because while individual
		// Go SSA instructions have file/line/col information, the parent
		// function does not. LLVM doesn't store filename information per
		// instruction, only per function. We work around this difference by
		// creating a fake DIFunction for each Go file and say that the
		// instruction really came from that (fake) function but was inlined in
		// the package initializer function.
		position := b.mxProgram.Fset.Position(pos)
		name := filepath.Base(position.Filename)
		difunc, ok := b.initPseudoFuncs[name]
		if !ok {
			diFuncType := b.dibuilder.CreateSubroutineType(llvm.DISubroutineType{
				File: b.getDIFile(position.Filename),
			})
			difunc = b.dibuilder.CreateFunction(b.getDIFile(position.Filename), llvm.DIFunction{
				Name:         b.mxFn.RelString(nil) + "#" + name,
				File:         b.getDIFile(position.Filename),
				Line:         0,
				Type:         diFuncType,
				LocalToUnit:  true,
				IsDefinition: true,
				ScopeLine:    0,
				Flags:        llvm.FlagPrototyped,
				Optimized:    true,
			})
			b.initPseudoFuncs[name] = difunc
		}
		b.SetCurrentDebugLocation(uint(position.Line), uint(position.Column), difunc, b.initInlinedAt)
		return
	}

	// Regular debug information.
	b.SetCurrentDebugLocation(uint(position.Line), uint(position.Column), b.difunc, llvm.Metadata{})
}

// getLocalVariable returns a debug info entry for a local variable, which may
// either be a parameter or a regular variable. It will create a new metadata
// entry if there isn't one for the variable yet.
func (b *builder) getLocalVariable(variable *types.Var) llvm.Metadata {
	if dilocal, ok := b.dilocals[variable]; ok {
		// DILocalVariable was already created, return it directly.
		return dilocal
	}

	pos := b.mxProgram.Fset.Position(variable.Pos())

	// Check whether this is a function parameter.
	for i, param := range b.fn.Params {
		if param.Object().(*types.Var) == variable {
			// Yes it is, create it as a function parameter.
			dilocal := b.dibuilder.CreateParameterVariable(b.difunc, llvm.DIParameterVariable{
				Name:           param.Name(),
				File:           b.getDIFile(pos.Filename),
				Line:           pos.Line,
				Type:           b.getDIType(param.Type()),
				AlwaysPreserve: true,
				ArgNo:          i + 1,
			})
			b.dilocals[variable] = dilocal
			return dilocal
		}
	}

	// No, it's not a parameter. Create a regular (auto) variable.
	dilocal := b.dibuilder.CreateAutoVariable(b.difunc, llvm.DIAutoVariable{
		Name:           variable.Name(),
		File:           b.getDIFile(pos.Filename),
		Line:           pos.Line,
		Type:           b.getDIType(variable.Type()),
		AlwaysPreserve: true,
	})
	b.dilocals[variable] = dilocal
	return dilocal
}

// attachDebugInfo adds debug info to a function declaration. It returns the
// DISubprogram metadata node.
func (c *compilerContext) attachDebugInfo(f *mxssa.Function) llvm.Metadata {
	syn := f.Syntax()
	if syn == nil {
		return llvm.Metadata{}
	}
	pos := c.mxProgram.Fset.Position(syn.Pos())
	_, fn := c.getFunction(f)
	return c.attachDebugInfoRaw(f, fn, "", pos.Filename, pos.Line)
}

// attachDebugInfo adds debug info to a function declaration. It returns the
// DISubprogram metadata node. This method allows some more control over how
// debug info is added to the function.
func (c *compilerContext) attachDebugInfoRaw(f *mxssa.Function, llvmFn llvm.Value, suffix, filename string, line int) llvm.Metadata {
	// Debug info for this function. Iterate receiver + params via mxssa.Function
	// signature; convert to go/types via ReverseType for getDIType (debug info
	// chain still keyed on go/types until §3).
	sig := f.Signature
	var diparams []llvm.Metadata
	if sig != nil {
		recv := sig.Recv()
		params := sig.Params()
		paramCount := 0
		if params != nil {
			paramCount = params.Len()
		}
		if recv != nil {
			paramCount++
		}
		diparams = make([]llvm.Metadata, 0, paramCount)
		if recv != nil {
			if goT := c.mxTranslator.ReverseType(recv.Type()); goT != nil {
				diparams = append(diparams, c.getDIType(goT))
			}
		}
		if params != nil {
			for i := 0; i < params.Len(); i++ {
				if goT := c.mxTranslator.ReverseType(params.At(i).Type()); goT != nil {
					diparams = append(diparams, c.getDIType(goT))
				}
			}
		}
	}
	diFuncType := c.dibuilder.CreateSubroutineType(llvm.DISubroutineType{
		File:       c.getDIFile(filename),
		Parameters: diparams,
		Flags:      0, // ?
	})
	// Use ssa.Function.RelString(nil) for the canonical name (mxssa's RelString
	// loses the receiver type for methods).
	canonicalName := f.RelString(nil)
	if goFn := c.mxTranslator.ReverseFunction(f); goFn != nil {
		canonicalName = goFn.RelString(nil)
	}
	difunc := c.dibuilder.CreateFunction(c.getDIFile(filename), llvm.DIFunction{
		Name:         canonicalName + suffix,
		LinkageName:  c.getFunctionInfo(f).linkName + suffix,
		File:         c.getDIFile(filename),
		Line:         line,
		Type:         diFuncType,
		LocalToUnit:  true,
		IsDefinition: true,
		ScopeLine:    0,
		Flags:        llvm.FlagPrototyped,
		Optimized:    true,
	})
	llvmFn.SetSubprogram(difunc)
	return difunc
}

// getDIFile returns a DIFile metadata node for the given filename. It tries to
// use one that was already created, otherwise it falls back to creating a new
// one.
func (c *compilerContext) getDIFile(filename string) llvm.Metadata {
	if _, ok := c.difiles[filename]; !ok {
		dir, file := filepath.Split(filename)
		if dir != "" {
			dir = dir[:len(dir)-1]
		}
		c.difiles[filename] = c.dibuilder.CreateFile(file, dir)
	}
	return c.difiles[filename]
}

// createPackage builds the LLVM IR for all types, methods, and global variables
// in the given package.
func (c *compilerContext) createPackage(irbuilder llvm.Builder, pkg *ssa.Package) {
	// Sort by position, so that the order of the functions in the IR matches
	// the order of functions in the source file. This is useful for testing,
	// for example.
	var members []string
	for name := range pkg.Members {
		members = append(members, name)
	}
	sort.Slice(members, func(i, j int) bool {
		iPos := pkg.Members[members[i]].Pos()
		jPos := pkg.Members[members[j]].Pos()
		if i == j {
			// Cannot sort by pos, so do it by name.
			return members[i] < members[j]
		}
		return iPos < jPos
	})

	// Define all functions.
	for _, name := range members {
		member := pkg.Members[name]
		switch member := member.(type) {
		case *ssa.Function:
			if member.TypeParams() != nil {
				// Do not try to build generic (non-instantiated) functions.
				continue
			}
			// Create the function definition.
			b := newBuilder(c, irbuilder, member)
			if _, ok := mathToLLVMMapping[member.RelString(nil)]; ok {
				// The body of this function (if there is one) is ignored and
				// replaced with a LLVM intrinsic call.
				b.defineMathOp()
				continue
			}
			if ok := b.defineMathBitsIntrinsic(); ok {
				// Like a math intrinsic, the body of this function was replaced
				// with a LLVM intrinsic.
				continue
			}
			if member.Blocks == nil {
				// Try to define this as an intrinsic function.
				b.defineIntrinsicFunction()
				// It might not be an intrinsic function but simply an external
				// function (defined via //go:linkname). Leave it undefined in
				// that case.
				continue
			}
			b.createFunctionDispatch()
		case *ssa.Type:
			if types.IsInterface(member.Type()) {
				// Interfaces don't have concrete methods.
				continue
			}
			if _, isalias := member.Type().(*types.Alias); isalias {
				// Aliases don't need to be redefined, since they just refer to
				// an already existing type whose methods will be defined.
				continue
			}

			// Named type. We should make sure all methods are created.
			// This includes both functions with pointer receivers and those
			// without.
			methods := getAllMethods(pkg.Prog, member.Type())
			methods = append(methods, getAllMethods(pkg.Prog, types.NewPointer(member.Type()))...)
			for _, method := range methods {
				// Parse this method.
				fn := pkg.Prog.MethodValue(method)
				if fn == nil {
					continue // probably a generic method
				}
				if member.Type().String() != member.String() {
					// This is a member on a type alias. Do not build such a
					// function.
					continue
				}
				if fn.Blocks == nil {
					continue // external function
				}
				if fn.Synthetic != "" && fn.Synthetic != "package initializer" {
					// This function is a kind of wrapper function (created by
					// the ssa package, not appearing in the source code) that
					// is created by the getFunction method as needed.
					// Therefore, don't build it here to avoid "function
					// redeclared" errors.
					continue
				}
				// Create the function definition.
				b := newBuilder(c, irbuilder, fn)
				b.createFunctionDispatch()
			}
		case *ssa.Global:
			// Global variable.
			mxMember := c.mxTranslator.LookupGlobal(member)
			info := c.getGlobalInfo(mxMember)
			global := c.getGlobal(mxMember)
			if files, ok := c.embedGlobals[member.Name()]; ok {
				c.createEmbedGlobal(member, global, files)
			} else if !info.extern {
				global.SetInitializer(llvm.ConstNull(global.GlobalValueType()))
				global.SetVisibility(llvm.HiddenVisibility)
				if info.section != "" {
					global.SetSection(info.section)
				}
			}
		}
	}

	// Add forwarding functions for functions that would otherwise be
	// implemented in assembly.
	for _, name := range members {
		member := pkg.Members[name]
		switch member := member.(type) {
		case *ssa.Function:
			if member.Blocks != nil {
				continue // external function
			}
			mxMember := c.mxTranslator.EnsureFunction(member)
			info := c.getFunctionInfo(mxMember)
			if aliasName, ok := stdlibAliases[info.linkName]; ok {
				alias := c.mod.NamedFunction(aliasName)
				if alias.IsNil() {
					// Shouldn't happen, but perhaps best to just ignore.
					// The error will be a link error, if there is an error.
					continue
				}
				b := newBuilder(c, irbuilder, member)
				b.createAlias(alias)
			}
		}
	}
}

// createEmbedGlobal creates an initializer for a //go:embed global variable.
func (c *compilerContext) createEmbedGlobal(member *ssa.Global, global llvm.Value, files []*loader.EmbedFile) {
	switch typ := member.Type().(*types.Pointer).Elem().Underlying().(type) {
	case *types.Basic:
		// String type.
		if typ.Kind() != types.String {
			// This is checked at the AST level, so should be unreachable.
			panic("expected a string type")
		}
		if len(files) != 1 {
			c.addError(member.Pos(), fmt.Sprintf("//go:embed for a string should be given exactly one file, got %d", len(files)))
			return
		}
		strObj := c.getEmbedFileString(files[0])
		global.SetInitializer(strObj)
		global.SetVisibility(llvm.HiddenVisibility)

	case *types.Slice:
		if typ.Elem().Underlying().(*types.Basic).Kind() != types.Byte {
			// This is checked at the AST level, so should be unreachable.
			panic("expected a byte slice")
		}
		if len(files) != 1 {
			c.addError(member.Pos(), fmt.Sprintf("//go:embed for a string should be given exactly one file, got %d", len(files)))
			return
		}
		file := files[0]
		bufferValue := c.ctx.ConstString(string(file.Data), false)
		bufferGlobal := llvm.AddGlobal(c.mod, bufferValue.Type(), c.mxPkg.Path()+"$embedslice")
		bufferGlobal.SetInitializer(bufferValue)
		bufferGlobal.SetLinkage(llvm.InternalLinkage)
		bufferGlobal.SetAlignment(1)
		slicePtr := llvm.ConstInBoundsGEP(bufferValue.Type(), bufferGlobal, []llvm.Value{
			llvm.ConstInt(c.uintptrType, 0, false),
			llvm.ConstInt(c.uintptrType, 0, false),
		})
		sliceLen := llvm.ConstInt(c.uintptrType, file.Size, false)
		sliceObj := llvm.ConstNamedStruct(c.getLLVMRuntimeType("_string"), []llvm.Value{slicePtr, sliceLen, sliceLen})
		global.SetInitializer(sliceObj)
		global.SetVisibility(llvm.HiddenVisibility)

		if c.Debug {
			// Add debug info to the slice backing array.
			position := c.mxProgram.Fset.Position(member.Pos())
			diglobal := c.dibuilder.CreateGlobalVariableExpression(llvm.Metadata{}, llvm.DIGlobalVariableExpression{
				File:        c.getDIFile(position.Filename),
				Line:        position.Line,
				Type:        c.getDIType(types.NewArray(types.Typ[types.Byte], int64(len(file.Data)))),
				LocalToUnit: true,
				Expr:        c.dibuilder.CreateExpression(nil),
			})
			bufferGlobal.AddMetadata(0, diglobal)
		}

	case *types.Struct:
		// Assume this is an embed.FS struct:
		// https://cs.opensource.google/go/go/+/refs/tags/go1.18.2:src/embed/embed.go;l=148
		// It looks like this:
		//   type FS struct {
		//       files *file
		//   }

		// Make a slice of the files, as they will appear in the binary. They
		// are sorted in a special way to allow for binary searches, see
		// src/embed/embed.go for details.
		dirset := map[string]struct{}{}
		var allFiles []*loader.EmbedFile
		for _, file := range files {
			allFiles = append(allFiles, file)
			dirname := file.Name
			for {
				dirname, _ = path.Split(path.Clean(dirname))
				if dirname == "" {
					break
				}
				if _, ok := dirset[dirname]; ok {
					break
				}
				dirset[dirname] = struct{}{}
				allFiles = append(allFiles, &loader.EmbedFile{
					Name: dirname,
				})
			}
		}
		sort.Slice(allFiles, func(i, j int) bool {
			dir1, name1 := path.Split(path.Clean(allFiles[i].Name))
			dir2, name2 := path.Split(path.Clean(allFiles[j].Name))
			if dir1 != dir2 {
				return dir1 < dir2
			}
			return name1 < name2
		})

		// Make the backing array for the []files slice. This is a LLVM global.
		embedFileStructType := typ.Field(0).Type().(*types.Pointer).Elem().(*types.Slice).Elem()
		llvmEmbedFileStructType := c.getLLVMType(embedFileStructType)
		var fileStructs []llvm.Value
		for _, file := range allFiles {
			fileStruct := llvm.ConstNull(llvmEmbedFileStructType)
			name := c.createConst(ssa.NewConst(constant.MakeString(file.Name), types.Typ[types.String]), member.Pos())
			fileStruct = c.builder.CreateInsertValue(fileStruct, name, 0, "") // "name" field
			if file.Hash != "" {
				data := c.getEmbedFileString(file)
				fileStruct = c.builder.CreateInsertValue(fileStruct, data, 1, "") // "data" field
			}
			fileStructs = append(fileStructs, fileStruct)
		}
		sliceDataInitializer := llvm.ConstArray(llvmEmbedFileStructType, fileStructs)
		sliceDataGlobal := llvm.AddGlobal(c.mod, sliceDataInitializer.Type(), c.mxPkg.Path()+"$embedfsfiles")
		sliceDataGlobal.SetInitializer(sliceDataInitializer)
		sliceDataGlobal.SetLinkage(llvm.InternalLinkage)
		sliceDataGlobal.SetGlobalConstant(true)
		sliceDataGlobal.SetUnnamedAddr(true)
		sliceDataGlobal.SetAlignment(c.targetData.ABITypeAlignment(sliceDataInitializer.Type()))
		if c.Debug {
			// Add debug information for code size attribution (among others).
			position := c.mxProgram.Fset.Position(member.Pos())
			diglobal := c.dibuilder.CreateGlobalVariableExpression(llvm.Metadata{}, llvm.DIGlobalVariableExpression{
				File:        c.getDIFile(position.Filename),
				Line:        position.Line,
				Type:        c.getDIType(types.NewArray(embedFileStructType, int64(len(allFiles)))),
				LocalToUnit: true,
				Expr:        c.dibuilder.CreateExpression(nil),
			})
			sliceDataGlobal.AddMetadata(0, diglobal)
		}

		// Create the slice object itself.
		// Because embed.FS refers to it as *[]embed.file instead of a plain
		// []embed.file, we have to store this as a global.
		slicePtr := llvm.ConstInBoundsGEP(sliceDataInitializer.Type(), sliceDataGlobal, []llvm.Value{
			llvm.ConstInt(c.uintptrType, 0, false),
			llvm.ConstInt(c.uintptrType, 0, false),
		})
		sliceLen := llvm.ConstInt(c.uintptrType, uint64(len(fileStructs)), false)
		sliceInitializer := c.ctx.ConstStruct([]llvm.Value{slicePtr, sliceLen, sliceLen}, false)
		sliceGlobal := llvm.AddGlobal(c.mod, sliceInitializer.Type(), c.mxPkg.Path()+"$embedfsslice")
		sliceGlobal.SetInitializer(sliceInitializer)
		sliceGlobal.SetLinkage(llvm.InternalLinkage)
		sliceGlobal.SetGlobalConstant(true)
		sliceGlobal.SetUnnamedAddr(true)
		sliceGlobal.SetAlignment(c.targetData.ABITypeAlignment(sliceInitializer.Type()))
		if c.Debug {
			position := c.mxProgram.Fset.Position(member.Pos())
			diglobal := c.dibuilder.CreateGlobalVariableExpression(llvm.Metadata{}, llvm.DIGlobalVariableExpression{
				File:        c.getDIFile(position.Filename),
				Line:        position.Line,
				Type:        c.getDIType(types.NewSlice(embedFileStructType)),
				LocalToUnit: true,
				Expr:        c.dibuilder.CreateExpression(nil),
			})
			sliceGlobal.AddMetadata(0, diglobal)
		}

		// Define the embed.FS struct. It has only one field: the files (as a
		// *[]embed.file).
		globalInitializer := llvm.ConstNull(c.getLLVMType(member.Type().(*types.Pointer).Elem()))
		globalInitializer = c.builder.CreateInsertValue(globalInitializer, sliceGlobal, 0, "")
		global.SetInitializer(globalInitializer)
		global.SetVisibility(llvm.HiddenVisibility)
		global.SetAlignment(c.targetData.ABITypeAlignment(globalInitializer.Type()))
	}
}

// getEmbedFileString returns the (constant) string object with the contents of
// the given file. This is a llvm.Value of a regular Go string.
func (c *compilerContext) getEmbedFileString(file *loader.EmbedFile) llvm.Value {
	dataGlobalName := "embed/file_" + file.Hash
	dataGlobal := c.mod.NamedGlobal(dataGlobalName)
	dataGlobalType := llvm.ArrayType(c.ctx.Int8Type(), int(file.Size))
	if dataGlobal.IsNil() {
		dataGlobal = llvm.AddGlobal(c.mod, dataGlobalType, dataGlobalName)
	}
	strPtr := llvm.ConstInBoundsGEP(dataGlobalType, dataGlobal, []llvm.Value{
		llvm.ConstInt(c.uintptrType, 0, false),
		llvm.ConstInt(c.uintptrType, 0, false),
	})
	strLen := llvm.ConstInt(c.uintptrType, file.Size, false)
	return llvm.ConstNamedStruct(c.getLLVMRuntimeType("_string"), []llvm.Value{strPtr, strLen, strLen})
}

// Start defining a function so that it can be filled with instructions: load
// parameters, create basic blocks, and set up debug information.
// This is separated out from createFunction() so that it is also usable to
// define compiler intrinsics like the atomic operations in sync/atomic.
func (b *builder) createFunctionStart(intrinsic bool) {
	if b.DumpSSA {
		fmt.Printf("\nfunc %s:\n", b.fn)
	}
	if !b.llvmFn.IsDeclaration() {
		errValue := b.llvmFn.Name() + " redeclared in this program"
		fnPos := getPosition(b.llvmFn)
		if fnPos.IsValid() {
			errValue += "\n\tprevious declaration at " + fnPos.String()
		}
		b.addError(b.mxFn.Pos(), errValue)
		return
	}

	b.addStandardDefinedAttributes(b.llvmFn)
	if !b.info.exported {
		// Do not set visibility for local linkage (internal or private).
		// Otherwise a "local linkage requires default visibility"
		// assertion error in llvm-project/llvm/include/llvm/IR/GlobalValue.h:236
		// is thrown.
		if b.llvmFn.Linkage() != llvm.InternalLinkage &&
			b.llvmFn.Linkage() != llvm.PrivateLinkage {
			b.llvmFn.SetVisibility(llvm.HiddenVisibility)
		}
		b.llvmFn.SetUnnamedAddr(true)
	}
	if b.info.section != "" {
		b.llvmFn.SetSection(b.info.section)
	}
	if b.info.exported && strings.HasPrefix(b.Triple, "wasm") {
		// Set the exported name. This is necessary for WebAssembly because
		// otherwise the function is not exported.
		functionAttr := b.ctx.CreateStringAttribute("wasm-export-name", b.info.linkName)
		b.llvmFn.AddFunctionAttr(functionAttr)
		// Unlike most targets, exported functions are actually visible in
		// WebAssembly (even if it's not called from within the WebAssembly
		// module). But LTO generally optimizes such functions away. Therefore,
		// exported functions must be explicitly marked as used.
		llvmutil.AppendToGlobal(b.mod, "llvm.used", b.llvmFn)
	}

	// Some functions have a pragma controlling the inlining level.
	switch b.info.inline {
	case inlineHint:
		// Add LLVM inline hint to functions with //go:inline pragma.
		inline := b.ctx.CreateEnumAttribute(llvm.AttributeKindID("inlinehint"), 0)
		b.llvmFn.AddFunctionAttr(inline)
	case inlineNone:
		// Add LLVM attribute to always avoid inlining this function.
		noinline := b.ctx.CreateEnumAttribute(llvm.AttributeKindID("noinline"), 0)
		b.llvmFn.AddFunctionAttr(noinline)
	}

	if b.info.interrupt {
		// Mark this function as an interrupt.
		// This is necessary on MCUs that don't push caller saved registers when
		// entering an interrupt, such as on AVR.
		if strings.HasPrefix(b.Triple, "avr") {
			b.llvmFn.AddFunctionAttr(b.ctx.CreateStringAttribute("signal", ""))
		} else {
			b.addError(b.mxFn.Pos(), "//go:interrupt not supported on this architecture")
		}
	}

	// Add debug info, if needed.
	if b.Debug {
		if b.mxFn.Synthetic == "package initializer" {
			// Package initializer functions have no debug info. Create some
			// fake debug info to at least have *something*.
			b.difunc = b.attachDebugInfoRaw(b.mxFn, b.llvmFn, "", b.packageDir, 0)
		} else if b.mxFn.Syntax() != nil {
			// Create debug info file if needed.
			b.difunc = b.attachDebugInfo(b.mxFn)
		}
		b.setDebugLocation(b.mxFn.Pos())
	}

	// Pre-create all basic blocks in the function.
	var entryBlock llvm.BasicBlock
	if intrinsic {
		// This function isn't defined in Go SSA. It is probably a compiler
		// intrinsic (like an atomic operation). Create the entry block
		// manually.
		entryBlock = b.ctx.AddBasicBlock(b.llvmFn, "entry")
		// Intrinsics may create internal branches (e.g. nil checks).
		// They will attempt to access b.currentBlockInfo to update the exit block.
		// Create some fake block info for them to access.
		blockInfo := []blockInfo{
			{
				entry: entryBlock,
				exit:  entryBlock,
			},
		}
		b.blockInfo = blockInfo
		b.currentBlockInfo = &blockInfo[0]
	} else {
		blocks := b.fn.Blocks
		blockInfo := make([]blockInfo, len(blocks))
		for _, block := range b.fn.DomPreorder() {
			info := &blockInfo[block.Index]
			llvmBlock := b.ctx.AddBasicBlock(b.llvmFn, block.Comment)
			info.entry = llvmBlock
			info.exit = llvmBlock
		}
		b.blockInfo = blockInfo
		// Normal functions have an entry block.
		entryBlock = blockInfo[0].entry
	}
	b.SetInsertPointAtEnd(entryBlock)

	if b.mxFn.Synthetic == "package initializer" {
		b.initPseudoFuncs = make(map[string]llvm.Metadata)

		// Create a fake 'inlined at' metadata node.
		// See setDebugLocation for details.
		alloca := b.CreateAlloca(b.uintptrType, "")
		b.initInlinedAt = alloca.InstructionDebugLoc()
		alloca.EraseFromParentAsInstruction()
	}

	// Load function parameters
	llvmParamIndex := 0
	for i, param := range b.fn.Params {
		llvmType := b.getLLVMType(param.Type())
		mxType := b.mxTranslator.ForwardType(param.Type())
		fields := make([]llvm.Value, 0, 1)
		for _, info := range b.expandFormalParamTypeTC(llvmType, param.Name(), mxType) {
			param := b.llvmFn.Param(llvmParamIndex)
			param.SetName(info.name)
			fields = append(fields, param)
			llvmParamIndex++
		}
		paramValue := b.collapseFormalParam(llvmType, fields)
		if b.mxFn != nil && i < len(b.mxFn.Params) {
			b.mxLocals[b.mxFn.Params[i]] = paramValue
		}

		// Add debug information to this parameter (if available)
		if b.Debug && b.mxFn.Syntax() != nil {
			dbgParam := b.getLocalVariable(param.Object().(*types.Var))
			loc := b.GetCurrentDebugLocation()
			if len(fields) == 1 {
				expr := b.dibuilder.CreateExpression(nil)
				b.dibuilder.InsertValueAtEnd(fields[0], dbgParam, expr, loc, entryBlock)
			} else {
				fieldOffsets := b.expandFormalParamOffsets(llvmType)
				for i, field := range fields {
					expr := b.dibuilder.CreateExpression([]uint64{
						0x1000,              // DW_OP_LLVM_fragment
						fieldOffsets[i] * 8, // offset in bits
						b.targetData.TypeAllocSize(field.Type()) * 8, // size in bits
					})
					b.dibuilder.InsertValueAtEnd(field, dbgParam, expr, loc, entryBlock)
				}
			}
		}
	}

	// Load free variables from the context. This is a closure (or bound
	// method).
	var context llvm.Value
	if !b.info.exported {
		context = b.llvmFn.LastParam()
		context.SetName("context")
	}
	if len(b.fn.FreeVars) != 0 {
		// Get a list of all variable types in the context.
		freeVarTypes := make([]llvm.Type, len(b.fn.FreeVars))
		for i, freeVar := range b.fn.FreeVars {
			freeVarTypes[i] = b.getLLVMType(freeVar.Type())
		}

		// Load each free variable from the context pointer.
		// A free variable is always a pointer when this is a closure, but it
		// can be another type when it is a wrapper for a bound method (these
		// wrappers are generated by the ssa package).
		for i, val := range b.emitPointerUnpack(context, freeVarTypes) {
			if b.mxFn != nil && i < len(b.mxFn.FreeVars) {
				b.mxLocals[b.mxFn.FreeVars[i]] = val
			}
		}
	}

	if b.mxFn.Recover != nil {
		// This function has deferred function calls. Set some things up for
		// them.
		b.deferInitFunc()
	}

	if b.NeedsStackObjects {
		// Create a dummy alloca that will be used in runtime.trackPointer.
		// It is necessary to pass a dummy alloca to runtime.trackPointer
		// because runtime.trackPointer is replaced by an alloca store.
		b.stackChainAlloca = b.CreateAlloca(b.ctx.Int8Type(), "stackalloc")
	}
}

// createFunctionDispatch routes to the TC implementation. The SSA path is no
// longer reachable in current builds (verified via MOXIE_SSA_TRACE 2026-04 and
// confirmed by removing complex64/128 cases from runtime/ that previously
// triggered the invalid-types fallback).
func (b *builder) createFunctionDispatch() {
	if b.mxFn == nil {
		// Cross-package shell that wasn't fully translated. Should not
		// happen in current usage. If hit, the function will simply not
		// have a body — linker error if anything calls it.
		return
	}
	b.createFunctionTC()
}

func (b *builder) createFunctionTC() {
	if len(b.mxFn.Blocks) == 0 {
		// External decl with no body — nothing to emit.
		return
	}
	if mxFnHasInvalidTypes(b.mxFn) {
		// Runtime helpers with complex64/128 cases (now removed from
		// runtime sources). If anything still hits this, it's dead code
		// the linker will DCE.
		return
	}
	b.createFunctionStart(false)
	b.checkMoxieRestrictions()

	for _, block := range b.fn.DomPreorder() {
		if b.DumpSSA {
			fmt.Printf("%d: %s:\n", block.Index, block.Comment)
		}
		mxBlock := b.mxFn.Blocks[block.Index]
		b.currentBlock = mxBlock
		b.currentBlockInfo = &b.blockInfo[block.Index]
		b.SetInsertPointAtEnd(b.currentBlockInfo.entry)

		for _, instr := range mxBlock.Instrs {
			if b.DumpSSA {
				if val, ok := instr.(mxssa.Value); ok && val.Name() != "" {
					fmt.Printf("\t%s = %s\n", val.Name(), val.String())
				} else {
					fmt.Printf("\t%s\n", instr.String())
				}
			}
			b.createInstructionTC(instr)
		}
		if b.mxFn.Name() == "init" && len(mxBlock.Instrs) == 0 {
			b.CreateRetVoid()
		}
	}

	for i, bb := range b.runDefersBlock {
		b.SetInsertPointAtEnd(bb)
		b.createRunDefers()
		b.CreateBr(b.afterDefersBlock[i])
	}

	if b.hasDeferFrame() {
		b.createLandingPad()
	}

	// Resolve mxssa phi nodes.
	for _, phi := range b.mxPhis {
		block := phi.phi.Block()
		phiType := phi.llvm.Type()
		for i, edge := range phi.phi.Edges {
			llvmVal := b.getValueTC(edge, phi.phi.Pos())
			llvmBlock := b.blockInfo[block.Preds[i].Index].exit
			if llvmVal.Type() != phiType {
				// Coercion emits instructions — place them before the
				// predecessor block's terminator.
				term := llvmBlock.LastInstruction()
				b.SetInsertPointBefore(term)
				llvmVal = b.coerceStructType(llvmVal, phiType)
			}
			phi.llvm.AddIncoming([]llvm.Value{llvmVal}, []llvm.BasicBlock{llvmBlock})
		}
	}

	if b.NeedsStackObjects {
		for _, phi := range b.mxPhis {
			insertPoint := llvm.NextInstruction(phi.llvm)
			for !insertPoint.IsAPHINode().IsNil() {
				insertPoint = llvm.NextInstruction(insertPoint)
			}
			b.SetInsertPointBefore(insertPoint)
			b.trackValue(phi.llvm)
		}
	}

	for _, sub := range b.fn.AnonFuncs {
		b := newBuilder(b.compilerContext, b.Builder, sub)
		b.llvmFn.SetLinkage(llvm.InternalLinkage)
		b.createFunctionDispatch()
	}

	if b.info.wasmExport != "" {
		b.createWasmExport()
	}
}

func (b *builder) createInstructionTC(instr mxssa.Instruction) {
	if b.Debug {
		b.setDebugLocation(instr.Pos())
	}

	switch instr := instr.(type) {
	case mxssa.Value:
		if value, err := b.createExprTC(instr); err != nil {
			b.diagnostics = append(b.diagnostics, err)
			b.mxLocals[instr] = llvm.Undef(b.getLLVMTypeTC(instr.Type()))
		} else {
			b.mxLocals[instr] = value
			if len(*instr.Referrers()) != 0 && b.NeedsStackObjects {
				b.trackExprTC(instr, value)
			}
		}
	case *mxssa.DebugRef:
		// ignore
	case *mxssa.Defer:
		b.createDeferTC(instr)
	case *mxssa.Go:
		if b.mxFn != nil && isUserPackageTC(b.mxFn.Pkg) {
			b.addError(instr.Pos(), "moxie: the go keyword is not supported (there are no goroutines)")
			return
		}
		b.addError(instr.Pos(), "todo: go TC path not yet implemented")
	case *mxssa.If:
		cond := b.getValueTC(instr.Cond, instr.Pos())
		block := instr.Block()
		blockThen := b.blockInfo[block.Succs[0].Index].entry
		blockElse := b.blockInfo[block.Succs[1].Index].entry
		b.CreateCondBr(cond, blockThen, blockElse)
	case *mxssa.Jump:
		blockJump := b.blockInfo[instr.Block().Succs[0].Index].entry
		b.CreateBr(blockJump)
	case *mxssa.MapUpdate:
		m := b.getValueTC(instr.Map, instr.Pos())
		key := b.getValueTC(instr.Key, instr.Pos())
		value := b.getValueTC(instr.Value, instr.Pos())
		mapType := instr.Map.Type().Underlying().(*typecheck.Map)
		b.createMapUpdateTC(mapType.Key(), m, key, value, instr.Pos())
	case *mxssa.Panic:
		value := b.getValueTC(instr.X, instr.Pos())
		b.createRuntimeInvoke("_panic", []llvm.Value{value}, "")
		b.CreateUnreachable()
	case *mxssa.Return:
		if b.hasDeferFrame() {
			b.createRuntimeCall("destroyDeferFrame", []llvm.Value{b.deferFrame}, "")
		}
		if len(instr.Results) == 0 {
			b.CreateRetVoid()
		} else if len(instr.Results) == 1 {
			val := b.getValueTC(instr.Results[0], instr.Pos())
			val = b.fixReturnType(val)
			b.CreateRet(val)
		} else {
			retType := b.llvmFn.GlobalValueType().ReturnType()
			retVal := llvm.ConstNull(retType)
			retFields := retType.StructElementTypes()
			for i, result := range instr.Results {
				val := b.getValueTC(result, instr.Pos())
				if i < len(retFields) {
					val = b.coerceStructType(val, retFields[i])
				}
				retVal = b.CreateInsertValue(retVal, val, i, "")
			}
			b.CreateRet(retVal)
		}
	case *mxssa.RunDefers:
		run := b.insertBasicBlock("rundefers.block")
		b.CreateBr(run)
		b.runDefersBlock = append(b.runDefersBlock, run)
		after := b.insertBasicBlock("rundefers.after")
		b.SetInsertPointAtEnd(after)
		b.afterDefersBlock = append(b.afterDefersBlock, after)
	case *mxssa.Send:
		b.createChanSendTC(instr)
	case *mxssa.Store:
		llvmAddr := b.getValueTC(instr.Addr, instr.Pos())
		llvmVal := b.getValueTC(instr.Val, instr.Pos())
		b.createNilCheckTC(instr.Addr, llvmAddr, "store")
		if b.targetData.TypeAllocSize(llvmVal.Type()) == 0 {
			return
		}
		b.CreateStore(llvmVal, llvmAddr)
	default:
		b.addError(instr.Pos(), "unknown instruction: "+instr.String())
	}
}

// createBuiltin lowers a builtin Go function (append, close, delete, etc.) to
// LLVM IR. It uses runtime calls for some builtins.

func (b *builder) createBuiltinTC(argTypes []typecheck.Type, argValues []llvm.Value, callName string, pos token.Pos) (llvm.Value, error) {
	switch callName {
	case "append":
		src := argValues[0]
		elems := argValues[1]
		srcBuf := b.CreateExtractValue(src, 0, "append.srcBuf")
		srcLen := b.CreateExtractValue(src, 1, "append.srcLen")
		srcCap := b.CreateExtractValue(src, 2, "append.srcCap")
		elemsBuf := b.CreateExtractValue(elems, 0, "append.elemsBuf")
		elemsLen := b.CreateExtractValue(elems, 1, "append.elemsLen")
		var elemType llvm.Type
		switch ut := argTypes[0].Underlying().(type) {
		case *typecheck.Slice:
			elemType = b.getLLVMTypeTC(ut.Elem())
		case *typecheck.Basic:
			elemType = b.ctx.Int8Type()
		default:
			panic("append on non-slice type: " + argTypes[0].String())
		}
		elemSize := llvm.ConstInt(b.uintptrType, b.targetData.TypeAllocSize(elemType), false)
		result := b.createRuntimeCall("sliceAppend", []llvm.Value{srcBuf, elemsBuf, srcLen, srcCap, elemsLen, elemSize}, "append.new")
		newPtr := b.CreateExtractValue(result, 0, "append.newPtr")
		newLen := b.CreateExtractValue(result, 1, "append.newLen")
		newCap := b.CreateExtractValue(result, 2, "append.newCap")
		newSlice := llvm.Undef(src.Type())
		newSlice = b.CreateInsertValue(newSlice, newPtr, 0, "")
		newSlice = b.CreateInsertValue(newSlice, newLen, 1, "")
		newSlice = b.CreateInsertValue(newSlice, newCap, 2, "")
		return newSlice, nil
	case "cap":
		value := argValues[0]
		var llvmCap llvm.Value
		switch argTypes[0].Underlying().(type) {
		case *typecheck.Chan:
			llvmCap = b.createRuntimeCall("chanCap", []llvm.Value{value}, "cap")
		case *typecheck.Slice:
			llvmCap = b.CreateExtractValue(value, 2, "cap")
		default:
			return llvm.Value{}, b.makeError(pos, "todo: cap: unknown type")
		}
		capSize := b.targetData.TypeAllocSize(llvmCap.Type())
		intSize := b.targetData.TypeAllocSize(b.intType)
		if capSize < intSize {
			llvmCap = b.CreateZExt(llvmCap, b.intType, "cap.int")
		} else if capSize > intSize {
			llvmCap = b.CreateTrunc(llvmCap, b.intType, "cap.int")
		}
		return llvmCap, nil
	case "close":
		b.createChanClose(argValues[0])
		return llvm.Value{}, nil
	case "clear":
		value := argValues[0]
		switch typ := argTypes[0].Underlying().(type) {
		case *typecheck.Slice:
			elementType := b.getLLVMTypeTC(typ.Elem())
			elementSize := b.targetData.TypeAllocSize(elementType)
			elementAlign := b.targetData.ABITypeAlignment(elementType)
			llvmBuf := b.CreateExtractValue(value, 0, "buf")
			llvmLen := b.CreateExtractValue(value, 1, "len")
			llvmLen = b.CreateMul(llvmLen, llvm.ConstInt(llvmLen.Type(), elementSize, false), "")
			memset := b.getMemsetFunc()
			call := b.createCall(memset.GlobalValueType(), memset, []llvm.Value{
				llvmBuf,
				llvm.ConstInt(b.ctx.Int8Type(), 0, false),
				llvmLen,
				llvm.ConstInt(b.ctx.Int1Type(), 0, false),
			}, "")
			call.AddCallSiteAttribute(1, b.ctx.CreateEnumAttribute(llvm.AttributeKindID("align"), uint64(elementAlign)))
			return llvm.Value{}, nil
		case *typecheck.Map:
			b.createMapClear(argValues[0])
			return llvm.Value{}, nil
		default:
			return llvm.Value{}, b.makeError(pos, "unsupported type in clear builtin: "+typ.String())
		}
	case "copy":
		dst := argValues[0]
		src := argValues[1]
		dstLen := b.CreateExtractValue(dst, 1, "copy.dstLen")
		srcLen := b.CreateExtractValue(src, 1, "copy.srcLen")
		dstBuf := b.CreateExtractValue(dst, 0, "copy.dstArray")
		srcBuf := b.CreateExtractValue(src, 0, "copy.srcArray")
		elemType := b.getLLVMTypeTC(argTypes[0].Underlying().(*typecheck.Slice).Elem())
		elemSize := llvm.ConstInt(b.uintptrType, b.targetData.TypeAllocSize(elemType), false)
		return b.createRuntimeCall("sliceCopy", []llvm.Value{dstBuf, srcBuf, dstLen, srcLen, elemSize}, "copy.n"), nil
	case "delete":
		m := argValues[0]
		key := argValues[1]
		return llvm.Value{}, b.createMapDeleteTC(argTypes[1], m, key, pos)
	case "len":
		value := argValues[0]
		var llvmLen llvm.Value
		switch argTypes[0].Underlying().(type) {
		case *typecheck.Basic, *typecheck.Slice:
			llvmLen = b.CreateExtractValue(value, 1, "len")
		case *typecheck.Chan:
			llvmLen = b.createRuntimeCall("chanLen", []llvm.Value{value}, "len")
		case *typecheck.Map:
			llvmLen = b.createRuntimeCall("hashmapLen", []llvm.Value{value}, "len")
		default:
			return llvm.Value{}, b.makeError(pos, "todo: len: unknown type")
		}
		lenSize := b.targetData.TypeAllocSize(llvmLen.Type())
		intSize := b.targetData.TypeAllocSize(b.intType)
		if lenSize < intSize {
			llvmLen = b.CreateZExt(llvmLen, b.intType, "len.int")
		} else if lenSize > intSize {
			llvmLen = b.CreateTrunc(llvmLen, b.intType, "len.int")
		}
		return llvmLen, nil
	case "min", "max":
		tok := token.LSS
		if callName == "max" {
			tok = token.GTR
		}
		result := argValues[0]
		typ := argTypes[0]
		for _, arg := range argValues[1:] {
			cmp, err := b.createBinOpTC(tok, typ, typ, result, arg, pos)
			if err != nil {
				return result, err
			}
			result = b.CreateSelect(cmp, result, arg, "")
		}
		return result, nil
	case "panic":
		b.createRuntimeInvoke("_panic", argValues, "")
		return llvm.Value{}, nil
	case "print", "println":
		b.createRuntimeCall("printlock", nil, "")
		for i, value := range argValues {
			if i >= 1 && callName == "println" {
				b.createRuntimeCall("printspace", nil, "")
			}
			typ := argTypes[i].Underlying()
			switch typ := typ.(type) {
			case *typecheck.Basic:
				switch {
				case typ.Kind() == typecheck.String:
					b.createRuntimeCall("printstring", []llvm.Value{value}, "")
				case typ.Kind() == typecheck.UnsafePointer:
					ptrValue := b.CreatePtrToInt(value, b.uintptrType, "")
					b.createRuntimeCall("printptr", []llvm.Value{ptrValue}, "")
				case typ.Info()&typecheck.IsInteger != 0:
					name := "print"
					if typ.Info()&typecheck.IsUnsigned != 0 {
						name += "uint"
					} else {
						name += "int"
					}
					name += strconv.FormatUint(b.targetData.TypeAllocSize(value.Type())*8, 10)
					b.createRuntimeCall(name, []llvm.Value{value}, "")
				case typ.Kind() == typecheck.Bool:
					b.createRuntimeCall("printbool", []llvm.Value{value}, "")
				case typ.Kind() == typecheck.Float32:
					b.createRuntimeCall("printfloat32", []llvm.Value{value}, "")
				case typ.Kind() == typecheck.Float64:
					b.createRuntimeCall("printfloat64", []llvm.Value{value}, "")
				default:
					return llvm.Value{}, b.makeError(pos, "unknown basic arg type: "+typ.String())
				}
			case *typecheck.Interface:
				b.createRuntimeCall("printitf", []llvm.Value{value}, "")
			case *typecheck.Map:
				b.createRuntimeCall("printmap", []llvm.Value{value}, "")
			case *typecheck.Pointer:
				ptrValue := b.CreatePtrToInt(value, b.uintptrType, "")
				b.createRuntimeCall("printptr", []llvm.Value{ptrValue}, "")
			case *typecheck.Slice:
				if basic, ok := typ.Elem().(*typecheck.Basic); ok && basic.Kind() == typecheck.Uint8 {
					b.createRuntimeCall("printbytes", []llvm.Value{value}, "")
				} else {
					bufptr := b.CreateExtractValue(value, 0, "")
					buflen := b.CreateExtractValue(value, 1, "")
					bufcap := b.CreateExtractValue(value, 2, "")
					ptrValue := b.CreatePtrToInt(bufptr, b.uintptrType, "")
					b.createRuntimeCall("printslice", []llvm.Value{ptrValue, buflen, bufcap}, "")
				}
			default:
				return llvm.Value{}, b.makeError(pos, "unknown arg type: "+typ.String())
			}
		}
		if callName == "println" {
			b.createRuntimeCall("printnl", nil, "")
		}
		b.createRuntimeCall("printunlock", nil, "")
		return llvm.Value{}, nil
	case "recover":
		useParentFrame := uint64(0)
		if b.hasDeferFrame() {
			useParentFrame = 1
		}
		return b.createRuntimeCall("_recover", []llvm.Value{llvm.ConstInt(b.ctx.Int1Type(), useParentFrame, false)}, ""), nil
	case "ssa:wrapnilchk":
		return argValues[0], nil
	case "Add":
		ptr := argValues[0]
		len := argValues[1]
		return b.CreateGEP(b.ctx.Int8Type(), ptr, []llvm.Value{len}, ""), nil
	case "Alignof":
		align := b.targetData.ABITypeAlignment(argValues[0].Type())
		return llvm.ConstInt(b.uintptrType, uint64(align), false), nil
	case "Offsetof":
		return llvm.Value{}, b.makeError(pos, "todo: unsafe.Offsetof")
	case "Sizeof":
		size := b.targetData.TypeAllocSize(argValues[0].Type())
		return llvm.ConstInt(b.uintptrType, size, false), nil
	case "Slice", "String":
		ptr := argValues[0]
		len := argValues[1]
		var elementType llvm.Type
		if callName == "Slice" {
			elementType = b.getLLVMTypeTC(argTypes[0].Underlying().(*typecheck.Pointer).Elem())
		} else {
			elementType = b.ctx.Int8Type()
		}
		b.createUnsafeSliceStringCheckTC("unsafe."+callName, ptr, len, elementType, argTypes[1].Underlying().(*typecheck.Basic))
		if len.Type().IntTypeWidth() < b.uintptrType.IntTypeWidth() {
			len = b.CreateZExt(len, b.uintptrType, "")
		} else if len.Type().IntTypeWidth() > b.uintptrType.IntTypeWidth() {
			len = b.CreateTrunc(len, b.uintptrType, "")
		}
		if callName == "Slice" {
			var sliceType llvm.Type
			if elementType == b.ctx.Int8Type() {
				sliceType = b.getLLVMRuntimeType("_string")
			} else {
				sliceType = b.ctx.StructType([]llvm.Type{
					ptr.Type(),
					b.uintptrType,
					b.uintptrType,
				}, false)
			}
			slice := llvm.Undef(sliceType)
			slice = b.CreateInsertValue(slice, ptr, 0, "")
			slice = b.CreateInsertValue(slice, len, 1, "")
			slice = b.CreateInsertValue(slice, len, 2, "")
			return slice, nil
		} else {
			str := llvm.Undef(b.getLLVMRuntimeType("_string"))
			str = b.CreateInsertValue(str, argValues[0], 0, "")
			str = b.CreateInsertValue(str, len, 1, "")
			str = b.CreateInsertValue(str, len, 2, "")
			return str, nil
		}
	case "SliceData", "StringData":
		return b.CreateExtractValue(argValues[0], 0, "slice.data"), nil
	default:
		return llvm.Value{}, b.makeError(pos, "todo: builtin: "+callName)
	}
}

// createFunctionCall lowers a Go SSA call instruction (to a simple function,
// closure, function pointer, builtin, method, etc.) to LLVM IR, usually a call
// instruction.
//
// This is also where compiler intrinsics are implemented.

func (b *builder) createFunctionCallTC(instr *mxssa.CallCommon) (llvm.Value, error) {
	if fn, ok := instr.Value.(*mxssa.Function); ok {
		name := fn.RelString(nil)
		switch {
		case strings.HasPrefix(name, "device.") || strings.HasPrefix(name, "device/") || strings.HasPrefix(name, "(device/"):
			return llvm.Value{}, b.makeError(instr.Pos(), "device intrinsic not available in TC path: "+name)
		case name == "runtime.supportsRecover":
			supportsRecover := uint64(0)
			if b.supportsRecover() {
				supportsRecover = 1
			}
			return llvm.ConstInt(b.ctx.Int1Type(), supportsRecover, false), nil
		case name == "runtime.panicStrategy":
			panicStrategy := map[string]uint64{
				"print": moxie.PanicStrategyPrint,
				"trap":  moxie.PanicStrategyTrap,
			}[b.Config.PanicStrategy]
			return llvm.ConstInt(b.ctx.Int8Type(), panicStrategy, false), nil
		case name == "runtime.exportedFuncPtr":
			fnArg := instr.Args[0].(*mxssa.Function)
			_, ptr := b.getFunction(fnArg)
			return b.CreatePtrToInt(ptr, b.uintptrType, ""), nil
		case strings.HasPrefix(fn.Name(), "__moxie_concat"):
			return b.createMoxieConcatTC(instr), nil
		case strings.HasPrefix(fn.Name(), "__moxie_eq"):
			return b.createMoxieEqTC(instr), nil
		case strings.HasPrefix(fn.Name(), "__moxie_lt"):
			return b.createMoxieLtTC(instr), nil
		case strings.HasPrefix(fn.Name(), "__moxie_secalloc"):
			return b.createMoxieSecallocTC(instr), nil
		case strings.HasPrefix(name, "syscall.Syscall") || strings.HasPrefix(name, "syscall.RawSyscall") || strings.HasPrefix(name, "golang.org/x/sys/unix.Syscall") || strings.HasPrefix(name, "golang.org/x/sys/unix.RawSyscall"):
			if b.GOOS != "darwin" {
				return b.createSyscallTC(instr)
			}
		case strings.HasPrefix(name, "syscall.rawSyscallNoError") || strings.HasPrefix(name, "golang.org/x/sys/unix.RawSyscallNoError"):
			return b.createRawSyscallNoErrorTC(instr)
		case name == "internal/abi.FuncPCABI0":
			retval := b.createDarwinFuncPCABI0CallTC(instr)
			if !retval.IsNil() {
				return retval, nil
			}
		}
	}

	var params []llvm.Value
	for _, param := range instr.Args {
		params = append(params, b.getValueTC(param, instr.Pos()))
	}

	var callee, context llvm.Value
	var calleeType llvm.Type
	exported := false
	var staticFn *mxssa.Function
	switch v := instr.Value.(type) {
	case *mxssa.Function:
		staticFn = v
	case *mxssa.MakeClosure:
		if f, ok := v.Fn.(*mxssa.Function); ok {
			staticFn = f
		}
	}
	if staticFn != nil {
		calleeType, callee = b.getFunction(staticFn)
		info := b.getFunctionInfo(staticFn)
		if callee.IsNil() {
			return llvm.Value{}, b.makeError(instr.Pos(), "undefined function: "+info.linkName)
		}
		if _, ok := instr.Value.(*mxssa.MakeClosure); ok {
			funcValue := b.getValueTC(instr.Value, instr.Pos())
			context = b.extractFuncContext(funcValue)
		} else {
			context = llvm.Undef(b.dataPtrType)
			if info.variadic && len(staticFn.Params) == 0 {
				calleeType = llvm.FunctionType(callee.GlobalValueType().ReturnType(), nil, false)
			}
		}
		exported = info.exported || strings.HasPrefix(info.linkName, "llvm.")
	} else if call, ok := instr.Value.(*mxssa.Builtin); ok {
		if call.Name() == "spawn" {
			return b.createSpawnTC(instr)
		}
		var argTypes []typecheck.Type
		for _, arg := range instr.Args {
			argTypes = append(argTypes, arg.Type())
		}
		return b.createBuiltinTC(argTypes, params, call.Name(), instr.Pos())
	} else if instr.IsInvoke {
		itf := b.getValueTC(instr.Value, instr.Pos())
		typecode := b.CreateExtractValue(itf, 0, "invoke.func.typecode")
		value := b.CreateExtractValue(itf, 1, "invoke.func.value")
		params = append([]llvm.Value{value}, params...)
		params = append(params, typecode)
		callee = b.getInvokeFunctionTC(instr)
		calleeType = callee.GlobalValueType()
		context = llvm.Undef(b.dataPtrType)
	} else {
		value := b.getValueTC(instr.Value, instr.Pos())
		callee, context = b.decodeFuncValue(value)
		calleeType = b.getLLVMFunctionTypeTC(instr.Value.Type().Underlying().(*typecheck.Signature))
		b.createNilCheckTC(instr.Value, callee, "fpcall")
	}

	if !exported {
		params = append(params, context)
	}

	return b.createInvoke(calleeType, callee, params, ""), nil
}

// getValue returns the LLVM value of a constant, function value, global, or
// already processed SSA expression.

// maxSliceSize determines the maximum size a slice of the given element type
// can be.
func (c *compilerContext) maxSliceSize(elementType llvm.Type) uint64 {
	// Calculate ^uintptr(0), which is the max value that fits in uintptr.
	maxPointerValue := llvm.ConstNot(llvm.ConstInt(c.uintptrType, 0, false)).ZExtValue()
	// Calculate (^uint(0))/2, which is the max value that fits in an int.
	maxIntegerValue := llvm.ConstNot(llvm.ConstInt(c.intType, 0, false)).ZExtValue() / 2

	// Determine the maximum allowed size for a slice. The biggest possible
	// pointer (starting from 0) would be maxPointerValue*sizeof(elementType) so
	// divide by the element type to get the real maximum size.
	elementSize := c.targetData.TypeAllocSize(elementType)
	if elementSize == 0 {
		elementSize = 1
	}
	maxSize := maxPointerValue / elementSize

	// len(slice) is an int. Make sure the length remains small enough to fit in
	// an int.
	if maxSize > maxIntegerValue {
		maxSize = maxIntegerValue
	}

	return maxSize
}


func (b *builder) createExprTC(expr mxssa.Value) (llvm.Value, error) {
	if _, ok := b.mxLocals[expr]; ok {
		panic("instruction has already been created: " + expr.String())
	}

	switch expr := expr.(type) {
	case *mxssa.Alloc:
		typ := b.getLLVMTypeTC(expr.Type().Underlying().(*typecheck.Pointer).Elem())
		size := b.targetData.TypeAllocSize(typ)
		if expr.Heap || size > b.MaxStackAlloc {
			maxSize := llvm.ConstNot(llvm.ConstInt(b.uintptrType, 0, false)).ZExtValue()
			if size > maxSize {
				return llvm.Value{}, b.makeError(expr.Pos(), fmt.Sprintf("value is too big (%v bytes)", size))
			}
			sizeValue := llvm.ConstInt(b.uintptrType, size, false)
			layoutValue := b.createObjectLayout(typ, expr.Pos())
			buf := b.createRuntimeCall("alloc", []llvm.Value{sizeValue, layoutValue}, expr.Comment)
			align := b.targetData.ABITypeAlignment(typ)
			buf.AddCallSiteAttribute(0, b.ctx.CreateEnumAttribute(llvm.AttributeKindID("align"), uint64(align)))
			return buf, nil
		} else {
			buf := llvmutil.CreateEntryBlockAlloca(b.Builder, typ, expr.Comment)
			if b.targetData.TypeAllocSize(typ) != 0 {
				b.CreateStore(llvm.ConstNull(typ), buf)
			}
			return buf, nil
		}
	case *mxssa.BinOp:
		x := b.getValueTC(expr.X, expr.Pos())
		y := b.getValueTC(expr.Y, expr.Pos())
		return b.createBinOpTC(expr.Op, expr.X.Type(), expr.Y.Type(), x, y, expr.Pos())
	case *mxssa.Call:
		return b.createFunctionCallTC(expr.Common())
	case *mxssa.ChangeInterface:
		return b.getValueTC(expr.X, expr.Pos()), nil
	case *mxssa.ChangeType:
		x := b.getValueTC(expr.X, expr.Pos())
		llvmType := b.getLLVMTypeTC(expr.Type())
		if x.Type() == llvmType {
			return x, nil
		}
		switch llvmType.TypeKind() {
		case llvm.StructTypeKind:
			value := llvm.Undef(llvmType)
			for i := 0; i < llvmType.StructElementTypesCount(); i++ {
				field := b.CreateExtractValue(x, i, "changetype.field")
				value = b.CreateInsertValue(value, field, i, "changetype.struct")
			}
			return value, nil
		default:
			return llvm.Value{}, errors.New("todo: unknown ChangeType type: " + expr.X.Type().String())
		}
	case *mxssa.Const:
		panic("const is not an expression")
	case *mxssa.Convert:
		x := b.getValueTC(expr.X, expr.Pos())
		return b.createConvertTC(expr.X.Type(), expr.Type(), x, expr.Pos())
	case *mxssa.Extract:
		if _, ok := expr.Tuple.(*mxssa.Select); ok {
			return b.getChanSelectResultTC(expr), nil
		}
		value := b.getValueTC(expr.Tuple, expr.Pos())
		return b.CreateExtractValue(value, expr.Index, ""), nil
	case *mxssa.Field:
		value := b.getValueTC(expr.X, expr.Pos())
		return b.CreateExtractValue(value, expr.Field, ""), nil
	case *mxssa.FieldAddr:
		val := b.getValueTC(expr.X, expr.Pos())
		b.createNilCheckTC(expr.X, val, "gep")
		indices := []llvm.Value{
			llvm.ConstInt(b.ctx.Int32Type(), 0, false),
			llvm.ConstInt(b.ctx.Int32Type(), uint64(expr.Field), false),
		}
		elementType := b.getLLVMTypeTC(expr.X.Type().Underlying().(*typecheck.Pointer).Elem())
		return b.CreateInBoundsGEP(elementType, val, indices, ""), nil
	case *mxssa.Function:
		panic("function is not an expression")
	case *mxssa.Global:
		panic("global is not an expression")
	case *mxssa.Index:
		collection := b.getValueTC(expr.X, expr.Pos())
		index := b.getValueTC(expr.Index, expr.Pos())
		switch xType := expr.X.Type().Underlying().(type) {
		case *typecheck.Basic:
			if xType.Info()&typecheck.IsString == 0 {
				panic("lookup on non-string?")
			}
			index = b.extendIntegerTC(index, expr.Index.Type(), b.uintptrType)
			length := b.CreateExtractValue(collection, 1, "len")
			b.createLookupBoundsCheck(length, index)
			buf := b.CreateExtractValue(collection, 0, "")
			bufElemType := b.ctx.Int8Type()
			bufPtr := b.CreateInBoundsGEP(bufElemType, buf, []llvm.Value{index}, "")
			return b.CreateLoad(bufElemType, bufPtr, ""), nil
		case *typecheck.Slice:
			if basic, ok := xType.Elem().(*typecheck.Basic); ok && basic.Kind() == typecheck.Uint8 {
				index = b.extendIntegerTC(index, expr.Index.Type(), b.uintptrType)
				length := b.CreateExtractValue(collection, 1, "len")
				b.createLookupBoundsCheck(length, index)
				buf := b.CreateExtractValue(collection, 0, "")
				bufElemType := b.ctx.Int8Type()
				bufPtr := b.CreateInBoundsGEP(bufElemType, buf, []llvm.Value{index}, "")
				return b.CreateLoad(bufElemType, bufPtr, ""), nil
			}
			panic("unexpected slice type in *mxssa.Index")
		case *typecheck.Array:
			index = b.extendIntegerTC(index, expr.Index.Type(), b.uintptrType)
			arrayLen := llvm.ConstInt(b.uintptrType, uint64(xType.Len()), false)
			b.createLookupBoundsCheck(arrayLen, index)
			arrayType := collection.Type()
			alloca, allocaSize := b.createTemporaryAlloca(arrayType, "index.alloca")
			b.CreateStore(collection, alloca)
			zero := llvm.ConstInt(b.ctx.Int32Type(), 0, false)
			ptr := b.CreateInBoundsGEP(arrayType, alloca, []llvm.Value{zero, index}, "index.gep")
			result := b.CreateLoad(arrayType.ElementType(), ptr, "index.load")
			b.emitLifetimeEnd(alloca, allocaSize)
			return result, nil
		default:
			panic("unknown *mxssa.Index type")
		}
	case *mxssa.IndexAddr:
		val := b.getValueTC(expr.X, expr.Pos())
		index := b.getValueTC(expr.Index, expr.Pos())
		var bufptr, buflen llvm.Value
		var bufType llvm.Type
		switch ptrTyp := expr.X.Type().Underlying().(type) {
		case *typecheck.Pointer:
			typ := ptrTyp.Elem().Underlying()
			switch typ := typ.(type) {
			case *typecheck.Array:
				bufptr = val
				buflen = llvm.ConstInt(b.uintptrType, uint64(typ.Len()), false)
				bufType = b.getLLVMTypeTC(typ)
				b.createNilCheckTC(expr.X, bufptr, "gep")
			default:
				return llvm.Value{}, b.makeError(expr.Pos(), "todo: indexaddr: "+typ.String())
			}
		case *typecheck.Slice:
			bufptr = b.CreateExtractValue(val, 0, "indexaddr.ptr")
			buflen = b.CreateExtractValue(val, 1, "indexaddr.len")
			bufType = b.getLLVMTypeTC(ptrTyp.Elem())
		case *typecheck.Basic:
			bufptr = b.CreateExtractValue(val, 0, "indexaddr.ptr")
			buflen = b.CreateExtractValue(val, 1, "indexaddr.len")
			bufType = b.ctx.Int8Type()
		default:
			return llvm.Value{}, b.makeError(expr.Pos(), "todo: indexaddr: "+ptrTyp.String())
		}
		index = b.extendIntegerTC(index, expr.Index.Type(), b.uintptrType)
		b.createLookupBoundsCheck(buflen, index)
		switch expr.X.Type().Underlying().(type) {
		case *typecheck.Pointer:
			indices := []llvm.Value{llvm.ConstInt(b.ctx.Int32Type(), 0, false), index}
			return b.CreateInBoundsGEP(bufType, bufptr, indices, ""), nil
		case *typecheck.Slice:
			return b.CreateInBoundsGEP(bufType, bufptr, []llvm.Value{index}, ""), nil
		case *typecheck.Basic:
			return b.CreateInBoundsGEP(bufType, bufptr, []llvm.Value{index}, ""), nil
		default:
			panic("unreachable")
		}
	case *mxssa.Lookup:
		value := b.getValueTC(expr.X, expr.Pos())
		index := b.getValueTC(expr.Index, expr.Pos())
		valueType := expr.Type()
		if expr.CommaOk {
			valueType = valueType.(*typecheck.Tuple).At(0).Type()
		}
		return b.createMapLookupTC(expr.X.Type().Underlying().(*typecheck.Map).Key(), valueType, value, index, expr.CommaOk, expr.Pos())
	case *mxssa.MakeChan:
		return b.createMakeChanTC(expr), nil
	case *mxssa.MakeClosure:
		return b.parseMakeClosureTC(expr)
	case *mxssa.MakeInterface:
		val := b.getValueTC(expr.X, expr.Pos())
		return b.createMakeInterfaceTC(val, expr.X.Type(), expr.Pos()), nil
	case *mxssa.MakeMap:
		return b.createMakeMapTC(expr)
	case *mxssa.MakeSlice:
		sliceLen := b.getValueTC(expr.Len, expr.Pos())
		sliceCap := b.getValueTC(expr.Cap, expr.Pos())
		sliceType := expr.Type().Underlying().(*typecheck.Slice)
		llvmElemType := b.getLLVMTypeTC(sliceType.Elem())
		elemSize := b.targetData.TypeAllocSize(llvmElemType)
		elemAlign := b.targetData.ABITypeAlignment(llvmElemType)
		elemSizeValue := llvm.ConstInt(b.uintptrType, elemSize, false)
		maxSize := b.maxSliceSize(llvmElemType)
		if elemSize > maxSize {
			return llvm.Value{}, b.makeError(expr.Pos(), fmt.Sprintf("slice element type is too big (%v bytes)", elemSize))
		}
		lenType := expr.Len.Type().Underlying().(*typecheck.Basic)
		capType := expr.Cap.Type().Underlying().(*typecheck.Basic)
		maxSizeValue := llvm.ConstInt(b.uintptrType, maxSize, false)
		b.createSliceBoundsCheckTC(maxSizeValue, sliceLen, sliceCap, sliceCap, lenType, capType, capType)
		sliceCapCast, err := b.createConvertTC(expr.Cap.Type(), typecheck.Typ[typecheck.Uint64], sliceCap, expr.Pos())
		if err != nil {
			return llvm.Value{}, err
		}
		sliceSize := b.CreateBinOp(llvm.Mul, elemSizeValue, sliceCapCast, "makeslice.cap")
		layoutValue := b.createObjectLayout(llvmElemType, expr.Pos())
		slicePtr := b.createRuntimeCall("alloc", []llvm.Value{sliceSize, layoutValue}, "makeslice.buf")
		slicePtr.AddCallSiteAttribute(0, b.ctx.CreateEnumAttribute(llvm.AttributeKindID("align"), uint64(elemAlign)))
		sliceLen, err = b.createConvertTC(expr.Len.Type(), typecheck.Typ[typecheck.Uint64], sliceLen, expr.Pos())
		if err != nil {
			return llvm.Value{}, err
		}
		sliceCap, err = b.createConvertTC(expr.Cap.Type(), typecheck.Typ[typecheck.Uint64], sliceCap, expr.Pos())
		if err != nil {
			return llvm.Value{}, err
		}
		slice := llvm.Undef(b.getLLVMTypeTC(sliceType))
		slice = b.CreateInsertValue(slice, slicePtr, 0, "")
		slice = b.CreateInsertValue(slice, sliceLen, 1, "")
		slice = b.CreateInsertValue(slice, sliceCap, 2, "")
		return slice, nil
	case *mxssa.Next:
		rangeVal := expr.Iter.(*mxssa.Range).X
		llvmRangeVal := b.getValueTC(rangeVal, expr.Pos())
		it := b.getValueTC(expr.Iter, expr.Pos())
		if expr.IsString {
			return b.createRuntimeCall("stringNext", []llvm.Value{llvmRangeVal, it}, "range.next"), nil
		} else {
			return b.createMapIteratorNextTC(rangeVal, llvmRangeVal, it), nil
		}
	case *mxssa.Phi:
		phi := b.CreatePHI(b.getLLVMTypeTC(expr.Type()), "")
		b.mxPhis = append(b.mxPhis, phiNodeTC{expr, phi})
		return phi, nil
	case *mxssa.Range:
		var iteratorType llvm.Type
		switch typ := expr.X.Type().Underlying().(type) {
		case *typecheck.Basic:
			iteratorType = b.getLLVMRuntimeType("stringIterator")
		case *typecheck.Map:
			iteratorType = b.getLLVMRuntimeType("hashmapIterator")
		default:
			panic("unknown type in range: " + typ.String())
		}
		it, _ := b.createTemporaryAlloca(iteratorType, "range.it")
		b.CreateStore(llvm.ConstNull(iteratorType), it)
		return it, nil
	case *mxssa.Select:
		return b.createSelectTC(expr), nil
	case *mxssa.Slice:
		value := b.getValueTC(expr.X, expr.Pos())
		var lowType, highType, maxType *typecheck.Basic
		var low, high, max llvm.Value
		if expr.Low != nil {
			lowType = expr.Low.Type().Underlying().(*typecheck.Basic)
			low = b.getValueTC(expr.Low, expr.Pos())
			low = b.extendIntegerTC(low, lowType, b.uintptrType)
		} else {
			lowType = typecheck.Typ[typecheck.Uint64]
			low = llvm.ConstInt(b.uintptrType, 0, false)
		}
		if expr.High != nil {
			highType = expr.High.Type().Underlying().(*typecheck.Basic)
			high = b.getValueTC(expr.High, expr.Pos())
			high = b.extendIntegerTC(high, highType, b.uintptrType)
		} else {
			highType = typecheck.Typ[typecheck.Uint64]
		}
		if expr.Max != nil {
			maxType = expr.Max.Type().Underlying().(*typecheck.Basic)
			max = b.getValueTC(expr.Max, expr.Pos())
			max = b.extendIntegerTC(max, maxType, b.uintptrType)
		} else {
			maxType = typecheck.Typ[typecheck.Uint64]
		}
		switch typ := expr.X.Type().Underlying().(type) {
		case *typecheck.Pointer:
			arrayType := typ.Elem().Underlying().(*typecheck.Array)
			length := arrayType.Len()
			llvmLen := llvm.ConstInt(b.uintptrType, uint64(length), false)
			if high.IsNil() {
				high = llvmLen
			}
			if max.IsNil() {
				max = llvmLen
			}
			indices := []llvm.Value{llvm.ConstInt(b.ctx.Int32Type(), 0, false), low}
			b.createNilCheckTC(expr.X, value, "slice")
			b.createSliceBoundsCheckTC(llvmLen, low, high, max, lowType, highType, maxType)
			if b.targetData.TypeAllocSize(low.Type()) > b.targetData.TypeAllocSize(b.uintptrType) {
				low = b.CreateTrunc(low, b.uintptrType, "")
			}
			if b.targetData.TypeAllocSize(high.Type()) > b.targetData.TypeAllocSize(b.uintptrType) {
				high = b.CreateTrunc(high, b.uintptrType, "")
			}
			if b.targetData.TypeAllocSize(max.Type()) > b.targetData.TypeAllocSize(b.uintptrType) {
				max = b.CreateTrunc(max, b.uintptrType, "")
			}
			sliceLen := b.CreateSub(high, low, "slice.len")
			slicePtr := b.CreateInBoundsGEP(b.getLLVMTypeTC(arrayType), value, indices, "slice.ptr")
			sliceCap := b.CreateSub(max, low, "slice.cap")
			slice := llvm.Undef(b.getLLVMTypeTC(expr.Type()))
			slice = b.CreateInsertValue(slice, slicePtr, 0, "")
			slice = b.CreateInsertValue(slice, sliceLen, 1, "")
			slice = b.CreateInsertValue(slice, sliceCap, 2, "")
			return slice, nil
		case *typecheck.Slice:
			oldPtr := b.CreateExtractValue(value, 0, "")
			oldLen := b.CreateExtractValue(value, 1, "")
			oldCap := b.CreateExtractValue(value, 2, "")
			if high.IsNil() {
				high = oldLen
			}
			if max.IsNil() {
				max = oldCap
			}
			b.createSliceBoundsCheckTC(oldCap, low, high, max, lowType, highType, maxType)
			if b.targetData.TypeAllocSize(low.Type()) > b.targetData.TypeAllocSize(b.uintptrType) {
				low = b.CreateTrunc(low, b.uintptrType, "")
			}
			if b.targetData.TypeAllocSize(high.Type()) > b.targetData.TypeAllocSize(b.uintptrType) {
				high = b.CreateTrunc(high, b.uintptrType, "")
			}
			if b.targetData.TypeAllocSize(max.Type()) > b.targetData.TypeAllocSize(b.uintptrType) {
				max = b.CreateTrunc(max, b.uintptrType, "")
			}
			ptrElemType := b.getLLVMTypeTC(typ.Elem())
			newPtr := b.CreateInBoundsGEP(ptrElemType, oldPtr, []llvm.Value{low}, "")
			newLen := b.CreateSub(high, low, "")
			newCap := b.CreateSub(max, low, "")
			slice := llvm.Undef(b.getLLVMTypeTC(typ))
			slice = b.CreateInsertValue(slice, newPtr, 0, "")
			slice = b.CreateInsertValue(slice, newLen, 1, "")
			slice = b.CreateInsertValue(slice, newCap, 2, "")
			return slice, nil
		case *typecheck.Basic:
			if typ.Info()&typecheck.IsString == 0 {
				return llvm.Value{}, b.makeError(expr.Pos(), "unknown slice type: "+typ.String())
			}
			if expr.Max != nil {
				return llvm.Value{}, b.makeError(expr.Pos(), "slicing a string with a max parameter is not allowed by the spec")
			}
			oldPtr := b.CreateExtractValue(value, 0, "")
			oldLen := b.CreateExtractValue(value, 1, "")
			if high.IsNil() {
				high = oldLen
			}
			b.createSliceBoundsCheckTC(oldLen, low, high, high, lowType, highType, maxType)
			if b.targetData.TypeAllocSize(low.Type()) > b.targetData.TypeAllocSize(b.uintptrType) {
				low = b.CreateTrunc(low, b.uintptrType, "")
			}
			if b.targetData.TypeAllocSize(high.Type()) > b.targetData.TypeAllocSize(b.uintptrType) {
				high = b.CreateTrunc(high, b.uintptrType, "")
			}
			newPtr := b.CreateInBoundsGEP(b.ctx.Int8Type(), oldPtr, []llvm.Value{low}, "")
			newLen := b.CreateSub(high, low, "")
			str := llvm.Undef(b.getLLVMRuntimeType("_string"))
			str = b.CreateInsertValue(str, newPtr, 0, "")
			str = b.CreateInsertValue(str, newLen, 1, "")
			str = b.CreateInsertValue(str, newLen, 2, "")
			return str, nil
		default:
			return llvm.Value{}, b.makeError(expr.Pos(), "unknown slice type: "+typ.String())
		}
	case *mxssa.SliceToArrayPointer:
		slice := b.getValueTC(expr.X, expr.Pos())
		sliceLen := b.CreateExtractValue(slice, 1, "")
		arrayLen := expr.Type().Underlying().(*typecheck.Pointer).Elem().Underlying().(*typecheck.Array).Len()
		b.createSliceToArrayPointerCheck(sliceLen, int64(arrayLen))
		return b.CreateExtractValue(slice, 0, ""), nil
	case *mxssa.TypeAssert:
		return b.createTypeAssertTC(expr), nil
	case *mxssa.UnOp:
		return b.createUnOpTC(expr)
	default:
		return llvm.Value{}, b.makeError(expr.Pos(), "todo: unknown expression: "+expr.String())
	}
}

// createBinOp creates a LLVM binary operation (add, sub, mul, etc) for a Go
// binary operation. This is almost a direct mapping, but there are some subtle
// differences such as the requirement in LLVM IR that both sides must have the
// same type, even for bitshifts. Also, signedness in Go is encoded in the type
// and is encoded in the operation in LLVM IR: this is important for some
// operations such as divide.

func (b *builder) createBinOpTC(op token.Token, typ, ytyp typecheck.Type, x, y llvm.Value, pos token.Pos) (llvm.Value, error) {
	switch typ := typ.Underlying().(type) {
	case *typecheck.Basic:
		if typ.Info()&typecheck.IsInteger != 0 {
			signed := typ.Info()&typecheck.IsUnsigned == 0
			switch op {
			case token.ADD:
				return b.CreateAdd(x, y, ""), nil
			case token.SUB:
				return b.CreateSub(x, y, ""), nil
			case token.MUL:
				return b.CreateMul(x, y, ""), nil
			case token.QUO, token.REM:
				b.createDivideByZeroCheck(y)
				if signed {
					llvmType := x.Type()
					minusOne := llvm.ConstSub(llvm.ConstInt(llvmType, 0, false), llvm.ConstInt(llvmType, 1, false))
					lowestInteger := llvm.ConstInt(x.Type(), 1<<(llvmType.IntTypeWidth()-1), false)
					yIsMinusOne := b.CreateICmp(llvm.IntEQ, y, minusOne, "")
					xIsLowestInteger := b.CreateICmp(llvm.IntEQ, x, lowestInteger, "")
					hasOverflow := b.CreateAnd(yIsMinusOne, xIsLowestInteger, "")
					y = b.CreateSelect(hasOverflow, llvm.ConstInt(llvmType, 1, true), y, "")
					if op == token.QUO {
						return b.CreateSDiv(x, y, ""), nil
					}
					return b.CreateSRem(x, y, ""), nil
				}
				if op == token.QUO {
					return b.CreateUDiv(x, y, ""), nil
				}
				return b.CreateURem(x, y, ""), nil
			case token.AND:
				return b.CreateAnd(x, y, ""), nil
			case token.OR:
				return b.CreateOr(x, y, ""), nil
			case token.XOR:
				return b.CreateXor(x, y, ""), nil
			case token.SHL, token.SHR:
				if ytyp.Underlying().(*typecheck.Basic).Info()&typecheck.IsUnsigned == 0 {
					b.createNegativeShiftCheck(y)
				}
				sizeX := b.targetData.TypeAllocSize(x.Type())
				sizeY := b.targetData.TypeAllocSize(y.Type())
				overshifted := b.CreateICmp(llvm.IntUGE, y, llvm.ConstInt(y.Type(), 8*sizeX, false), "shift.overflow")
				switch {
				case sizeX > sizeY:
					y = b.CreateZExt(y, x.Type(), "")
				case sizeX < sizeY:
					y = b.CreateTrunc(y, x.Type(), "")
				}
				var val llvm.Value
				switch op {
				case token.SHL:
					val = b.CreateShl(x, y, "")
				case token.SHR:
					if signed {
						y = b.CreateSelect(overshifted, llvm.ConstInt(y.Type(), 8*sizeX-1, false), y, "shift.offset")
						return b.CreateAShr(x, y, ""), nil
					}
					val = b.CreateLShr(x, y, "")
				default:
					panic("unreachable")
				}
				return b.CreateSelect(overshifted, llvm.ConstInt(val.Type(), 0, false), val, "shift.result"), nil
			case token.EQL:
				return b.CreateICmp(llvm.IntEQ, x, y, ""), nil
			case token.NEQ:
				return b.CreateICmp(llvm.IntNE, x, y, ""), nil
			case token.AND_NOT:
				inv := b.CreateNot(y, "")
				return b.CreateAnd(x, inv, ""), nil
			case token.LSS:
				if signed {
					return b.CreateICmp(llvm.IntSLT, x, y, ""), nil
				}
				return b.CreateICmp(llvm.IntULT, x, y, ""), nil
			case token.LEQ:
				if signed {
					return b.CreateICmp(llvm.IntSLE, x, y, ""), nil
				}
				return b.CreateICmp(llvm.IntULE, x, y, ""), nil
			case token.GTR:
				if signed {
					return b.CreateICmp(llvm.IntSGT, x, y, ""), nil
				}
				return b.CreateICmp(llvm.IntUGT, x, y, ""), nil
			case token.GEQ:
				if signed {
					return b.CreateICmp(llvm.IntSGE, x, y, ""), nil
				}
				return b.CreateICmp(llvm.IntUGE, x, y, ""), nil
			default:
				panic("binop on integer: " + op.String())
			}
		} else if typ.Info()&typecheck.IsFloat != 0 {
			switch op {
			case token.ADD:
				return b.CreateFAdd(x, y, ""), nil
			case token.SUB:
				return b.CreateFSub(x, y, ""), nil
			case token.MUL:
				return b.CreateFMul(x, y, ""), nil
			case token.QUO:
				return b.CreateFDiv(x, y, ""), nil
			case token.EQL:
				return b.CreateFCmp(llvm.FloatOEQ, x, y, ""), nil
			case token.NEQ:
				return b.CreateFCmp(llvm.FloatUNE, x, y, ""), nil
			case token.LSS:
				return b.CreateFCmp(llvm.FloatOLT, x, y, ""), nil
			case token.LEQ:
				return b.CreateFCmp(llvm.FloatOLE, x, y, ""), nil
			case token.GTR:
				return b.CreateFCmp(llvm.FloatOGT, x, y, ""), nil
			case token.GEQ:
				return b.CreateFCmp(llvm.FloatOGE, x, y, ""), nil
			default:
				panic("binop on float: " + op.String())
			}
		} else if typ.Info()&typecheck.IsBoolean != 0 {
			switch op {
			case token.EQL:
				return b.CreateICmp(llvm.IntEQ, x, y, ""), nil
			case token.NEQ:
				return b.CreateICmp(llvm.IntNE, x, y, ""), nil
			default:
				panic("binop on bool: " + op.String())
			}
		} else if typ.Kind() == typecheck.UnsafePointer {
			switch op {
			case token.EQL:
				return b.CreateICmp(llvm.IntEQ, x, y, ""), nil
			case token.NEQ:
				return b.CreateICmp(llvm.IntNE, x, y, ""), nil
			default:
				panic("binop on pointer: " + op.String())
			}
		} else if typ.Info()&typecheck.IsString != 0 {
			switch op {
			case token.ADD:
				return b.createRuntimeCall("stringConcat", []llvm.Value{x, y}, ""), nil
			case token.EQL:
				return b.createRuntimeCall("stringEqual", []llvm.Value{x, y}, ""), nil
			case token.NEQ:
				result := b.createRuntimeCall("stringEqual", []llvm.Value{x, y}, "")
				return b.CreateNot(result, ""), nil
			case token.LSS:
				return b.createRuntimeCall("stringLess", []llvm.Value{x, y}, ""), nil
			case token.LEQ:
				result := b.createRuntimeCall("stringLess", []llvm.Value{y, x}, "")
				return b.CreateNot(result, ""), nil
			case token.GTR:
				return b.createRuntimeCall("stringLess", []llvm.Value{y, x}, ""), nil
			case token.GEQ:
				result := b.createRuntimeCall("stringLess", []llvm.Value{x, y}, "")
				return b.CreateNot(result, ""), nil
			default:
				panic("binop on string: " + op.String())
			}
		} else {
			return llvm.Value{}, b.makeError(pos, "todo: unknown basic type in binop: "+typ.String())
		}
	case *typecheck.Signature:
		x = b.extractFuncScalar(x)
		y = b.extractFuncScalar(y)
		switch op {
		case token.EQL:
			return b.CreateICmp(llvm.IntEQ, x, y, ""), nil
		case token.NEQ:
			return b.CreateICmp(llvm.IntNE, x, y, ""), nil
		default:
			return llvm.Value{}, b.makeError(pos, "binop on signature: "+op.String())
		}
	case *typecheck.Interface:
		switch op {
		case token.EQL, token.NEQ:
			nilInterface := llvm.ConstNull(x.Type())
			var result llvm.Value
			if x == nilInterface || y == nilInterface {
				typecodeX := b.CreateExtractValue(x, 0, "")
				typecodeY := b.CreateExtractValue(y, 0, "")
				result = b.CreateICmp(llvm.IntEQ, typecodeX, typecodeY, "")
			} else {
				result = b.createRuntimeCall("interfaceEqual", []llvm.Value{x, y}, "")
			}
			if op == token.NEQ {
				result = b.CreateNot(result, "")
			}
			return result, nil
		default:
			return llvm.Value{}, b.makeError(pos, "binop on interface: "+op.String())
		}
	case *typecheck.Chan, *typecheck.Map, *typecheck.Pointer:
		switch op {
		case token.EQL:
			return b.CreateICmp(llvm.IntEQ, x, y, ""), nil
		case token.NEQ:
			return b.CreateICmp(llvm.IntNE, x, y, ""), nil
		default:
			return llvm.Value{}, b.makeError(pos, "todo: binop on pointer: "+op.String())
		}
	case *typecheck.Slice:
		if basic, ok := typ.Elem().(*typecheck.Basic); ok && basic.Kind() == typecheck.Uint8 {
			switch op {
			case token.ADD:
				return b.createRuntimeCall("stringConcat", []llvm.Value{x, y}, ""), nil
			case token.EQL:
				return b.createRuntimeCall("stringEqual", []llvm.Value{x, y}, ""), nil
			case token.NEQ:
				result := b.createRuntimeCall("stringEqual", []llvm.Value{x, y}, "")
				return b.CreateNot(result, ""), nil
			case token.LSS:
				return b.createRuntimeCall("stringLess", []llvm.Value{x, y}, ""), nil
			case token.LEQ:
				result := b.createRuntimeCall("stringLess", []llvm.Value{y, x}, "")
				return b.CreateNot(result, ""), nil
			case token.GTR:
				return b.createRuntimeCall("stringLess", []llvm.Value{y, x}, ""), nil
			case token.GEQ:
				result := b.createRuntimeCall("stringLess", []llvm.Value{x, y}, "")
				return b.CreateNot(result, ""), nil
			default:
				return llvm.Value{}, b.makeError(pos, "todo: binop on []byte: "+op.String())
			}
		}
		if op == token.OR {
			xPtr := b.CreateExtractValue(x, 0, "concat.xPtr")
			xLen := b.CreateExtractValue(x, 1, "concat.xLen")
			yPtr := b.CreateExtractValue(y, 0, "concat.yPtr")
			yLen := b.CreateExtractValue(y, 1, "concat.yLen")
			elemType := b.getLLVMTypeTC(typ.Elem())
			elemSize := llvm.ConstInt(b.uintptrType, b.targetData.TypeAllocSize(elemType), false)
			result := b.createRuntimeCall("sliceAppend", []llvm.Value{xPtr, yPtr, xLen, xLen, yLen, elemSize}, "concat.new")
			newPtr := b.CreateExtractValue(result, 0, "concat.ptr")
			newLen := b.CreateExtractValue(result, 1, "concat.len")
			newCap := b.CreateExtractValue(result, 2, "concat.cap")
			newSlice := llvm.Undef(x.Type())
			newSlice = b.CreateInsertValue(newSlice, newPtr, 0, "")
			newSlice = b.CreateInsertValue(newSlice, newLen, 1, "")
			newSlice = b.CreateInsertValue(newSlice, newCap, 2, "")
			return newSlice, nil
		}
		if op != token.EQL && op != token.NEQ {
			return llvm.Value{}, b.makeError(pos, "todo: binop on slice: "+op.String())
		}
		xPtr := b.CreateExtractValue(x, 0, "")
		xLen := b.CreateExtractValue(x, 1, "")
		yPtr := b.CreateExtractValue(y, 0, "")
		yLen := b.CreateExtractValue(y, 1, "")
		elemSize := b.targetData.TypeAllocSize(b.getLLVMTypeTC(typ.Elem()))
		elemSizeVal := llvm.ConstInt(b.uintptrType, elemSize, false)
		result := b.createRuntimeCall("sliceEqual", []llvm.Value{xPtr, yPtr, xLen, yLen, elemSizeVal}, "")
		switch op {
		case token.EQL:
			return result, nil
		case token.NEQ:
			return b.CreateNot(result, ""), nil
		default:
			panic("unreachable")
		}
	case *typecheck.Array:
		result := llvm.ConstInt(b.ctx.Int1Type(), 1, true)
		for i := 0; i < int(typ.Len()); i++ {
			xField := b.CreateExtractValue(x, i, "")
			yField := b.CreateExtractValue(y, i, "")
			fieldEqual, err := b.createBinOpTC(token.EQL, typ.Elem(), typ.Elem(), xField, yField, pos)
			if err != nil {
				return llvm.Value{}, err
			}
			result = b.CreateAnd(result, fieldEqual, "")
		}
		switch op {
		case token.EQL:
			return result, nil
		case token.NEQ:
			return b.CreateNot(result, ""), nil
		default:
			return llvm.Value{}, b.makeError(pos, "unknown: binop on array: "+op.String())
		}
	case *typecheck.Struct:
		result := llvm.ConstInt(b.ctx.Int1Type(), 1, true)
		for i := 0; i < typ.NumFields(); i++ {
			if typ.Field(i).Name() == "_" {
				continue
			}
			fieldType := typ.Field(i).Type()
			xField := b.CreateExtractValue(x, i, "")
			yField := b.CreateExtractValue(y, i, "")
			fieldEqual, err := b.createBinOpTC(token.EQL, fieldType, fieldType, xField, yField, pos)
			if err != nil {
				return llvm.Value{}, err
			}
			result = b.CreateAnd(result, fieldEqual, "")
		}
		switch op {
		case token.EQL:
			return result, nil
		case token.NEQ:
			return b.CreateNot(result, ""), nil
		default:
			return llvm.Value{}, b.makeError(pos, "unknown: binop on struct: "+op.String())
		}
	default:
		return llvm.Value{}, b.makeError(pos, "todo: binop type: "+typ.String())
	}
}

// createConst creates a LLVM constant value from a Go constant.
func (c *compilerContext) createConst(expr *ssa.Const, pos token.Pos) llvm.Value {
	switch typ := expr.Type().Underlying().(type) {
	case *types.Basic:
		llvmType := c.getLLVMType(typ)
		if typ.Info()&types.IsBoolean != 0 {
			n := uint64(0)
			if constant.BoolVal(expr.Value) {
				n = 1
			}
			return llvm.ConstInt(llvmType, n, false)
		} else if typ.Info()&types.IsString != 0 {
			str := constant.StringVal(expr.Value)
			strLen := llvm.ConstInt(c.uintptrType, uint64(len(str)), false)
			var strPtr llvm.Value
			if str != "" {
				objname := c.mxPkg.Path() + "$string"
				globalType := llvm.ArrayType(c.ctx.Int8Type(), len(str))
				global := llvm.AddGlobal(c.mod, globalType, objname)
				global.SetInitializer(c.ctx.ConstString(str, false))
				global.SetLinkage(llvm.InternalLinkage)
				global.SetGlobalConstant(true)
				global.SetUnnamedAddr(true)
				global.SetAlignment(1)
				if c.Debug {
					// Unfortunately, expr.Pos() is always token.NoPos.
					position := c.mxProgram.Fset.Position(pos)
					diglobal := c.dibuilder.CreateGlobalVariableExpression(llvm.Metadata{}, llvm.DIGlobalVariableExpression{
						File:        c.getDIFile(position.Filename),
						Line:        position.Line,
						Type:        c.getDIType(types.NewArray(types.Typ[types.Byte], int64(len(str)))),
						LocalToUnit: true,
						Expr:        c.dibuilder.CreateExpression(nil),
					})
					global.AddMetadata(0, diglobal)
				}
				zero := llvm.ConstInt(c.ctx.Int32Type(), 0, false)
				strPtr = llvm.ConstInBoundsGEP(globalType, global, []llvm.Value{zero, zero})
			} else {
				strPtr = llvm.ConstNull(c.dataPtrType)
			}
			strObj := llvm.ConstNamedStruct(c.getLLVMRuntimeType("_string"), []llvm.Value{strPtr, strLen, strLen})
			return strObj
		} else if typ.Kind() == types.UnsafePointer {
			if !expr.IsNil() {
				value, _ := constant.Uint64Val(constant.ToInt(expr.Value))
				return llvm.ConstIntToPtr(llvm.ConstInt(c.uintptrType, value, false), c.dataPtrType)
			}
			return llvm.ConstNull(c.dataPtrType)
		} else if typ.Info()&types.IsUnsigned != 0 {
			n, _ := constant.Uint64Val(constant.ToInt(expr.Value))
			return llvm.ConstInt(llvmType, n, false)
		} else if typ.Info()&types.IsInteger != 0 { // signed
			n, _ := constant.Int64Val(constant.ToInt(expr.Value))
			return llvm.ConstInt(llvmType, uint64(n), true)
		} else if typ.Info()&types.IsFloat != 0 {
			n, _ := constant.Float64Val(expr.Value)
			return llvm.ConstFloat(llvmType, n)
		} else if typ.Kind() == types.Complex64 {
			r := c.createConst(ssa.NewConst(constant.Real(expr.Value), types.Typ[types.Float32]), pos)
			i := c.createConst(ssa.NewConst(constant.Imag(expr.Value), types.Typ[types.Float32]), pos)
			cplx := llvm.Undef(c.ctx.StructType([]llvm.Type{c.ctx.FloatType(), c.ctx.FloatType()}, false))
			cplx = c.builder.CreateInsertValue(cplx, r, 0, "")
			cplx = c.builder.CreateInsertValue(cplx, i, 1, "")
			return cplx
		} else if typ.Kind() == types.Complex128 {
			r := c.createConst(ssa.NewConst(constant.Real(expr.Value), types.Typ[types.Float64]), pos)
			i := c.createConst(ssa.NewConst(constant.Imag(expr.Value), types.Typ[types.Float64]), pos)
			cplx := llvm.Undef(c.ctx.StructType([]llvm.Type{c.ctx.DoubleType(), c.ctx.DoubleType()}, false))
			cplx = c.builder.CreateInsertValue(cplx, r, 0, "")
			cplx = c.builder.CreateInsertValue(cplx, i, 1, "")
			return cplx
		} else {
			panic("unknown constant of basic type: " + expr.String())
		}
	case *types.Chan:
		if expr.Value != nil {
			panic("expected nil chan constant")
		}
		return llvm.ConstNull(c.getLLVMType(expr.Type()))
	case *types.Signature:
		if expr.Value != nil {
			panic("expected nil signature constant")
		}
		return llvm.ConstNull(c.getLLVMType(expr.Type()))
	case *types.Interface:
		if expr.Value != nil {
			panic("expected nil interface constant")
		}
		// Create a generic nil interface with no dynamic type (typecode=0).
		fields := []llvm.Value{
			llvm.ConstInt(c.uintptrType, 0, false),
			llvm.ConstPointerNull(c.dataPtrType),
		}
		return llvm.ConstNamedStruct(c.getLLVMRuntimeType("_interface"), fields)
	case *types.Pointer:
		if expr.Value != nil {
			panic("expected nil pointer constant")
		}
		return llvm.ConstPointerNull(c.getLLVMType(typ))
	case *types.Array:
		if expr.Value != nil {
			panic("expected nil array constant")
		}
		return llvm.ConstNull(c.getLLVMType(expr.Type()))
	case *types.Slice:
		if expr.Value != nil {
			panic("expected nil slice constant")
		}
		// Moxie: use ConstNull with correct type (named for []byte).
		return llvm.ConstNull(c.getLLVMType(expr.Type()))
	case *types.Struct:
		if expr.Value != nil {
			panic("expected nil struct constant")
		}
		return llvm.ConstNull(c.getLLVMType(expr.Type()))
	case *types.Map:
		if !expr.IsNil() {
			// I believe this is not allowed by the Go spec.
			panic("non-nil map constant")
		}
		llvmType := c.getLLVMType(typ)
		return llvm.ConstNull(llvmType)
	default:
		panic("unknown constant: " + expr.String())
	}
}

// createConvert creates a Go type conversion instruction.

func (b *builder) createConvertTC(typeFrom, typeTo typecheck.Type, value llvm.Value, pos token.Pos) (llvm.Value, error) {
	llvmTypeFrom := value.Type()
	llvmTypeTo := b.getLLVMTypeTC(typeTo)

	isPtrFrom := isPointerTC(typeFrom.Underlying())
	isPtrTo := isPointerTC(typeTo.Underlying())
	if isPtrFrom && !isPtrTo {
		return b.CreatePtrToInt(value, llvmTypeTo, ""), nil
	} else if !isPtrFrom && isPtrTo {
		return b.CreateIntToPtr(value, llvmTypeTo, ""), nil
	}

	if isPtrFrom && isPtrTo {
		return value, nil
	}

	switch typeTo := typeTo.Underlying().(type) {
	case *typecheck.Basic:
		sizeFrom := b.targetData.TypeAllocSize(llvmTypeFrom)

		if typeTo.Info()&typecheck.IsString != 0 {
			switch typeFrom := typeFrom.Underlying().(type) {
			case *typecheck.Basic:
				if sizeFrom > 4 {
					value = b.CreateTrunc(value, b.ctx.Int32Type(), "")
				} else if sizeFrom < 4 && typeTo.Info()&typecheck.IsUnsigned != 0 {
					value = b.CreateZExt(value, b.ctx.Int32Type(), "")
				} else if sizeFrom < 4 {
					value = b.CreateSExt(value, b.ctx.Int32Type(), "")
				}
				return b.createRuntimeCall("stringFromUnicode", []llvm.Value{value}, ""), nil
			case *typecheck.Slice:
				switch typeFrom.Elem().(*typecheck.Basic).Kind() {
				case typecheck.Uint8:
					ptr := b.CreateExtractValue(value, 0, "")
					ln := b.CreateExtractValue(value, 1, "")
					cp := b.CreateExtractValue(value, 2, "")
					str := llvm.Undef(b.getLLVMRuntimeType("_string"))
					str = b.CreateInsertValue(str, ptr, 0, "")
					str = b.CreateInsertValue(str, ln, 1, "")
					str = b.CreateInsertValue(str, cp, 2, "")
					return str, nil
				case typecheck.Int32:
					return b.createRuntimeCall("stringFromRunes", []llvm.Value{value}, ""), nil
				default:
					return llvm.Value{}, b.makeError(pos, "todo: convert to string: "+typeFrom.String())
				}
			default:
				return llvm.Value{}, b.makeError(pos, "todo: convert to string: "+typeFrom.String())
			}
		}

		typeFrom := typeFrom.Underlying().(*typecheck.Basic)
		sizeTo := b.targetData.TypeAllocSize(llvmTypeTo)

		if typeFrom.Info()&typecheck.IsInteger != 0 && typeTo.Info()&typecheck.IsInteger != 0 {
			if sizeFrom > sizeTo {
				return b.CreateTrunc(value, llvmTypeTo, ""), nil
			} else if typeFrom.Info()&typecheck.IsUnsigned != 0 {
				return b.CreateZExt(value, llvmTypeTo, ""), nil
			} else {
				return b.CreateSExt(value, llvmTypeTo, ""), nil
			}
		}

		if typeFrom.Info()&typecheck.IsFloat != 0 && typeTo.Info()&typecheck.IsFloat != 0 {
			if sizeFrom > sizeTo {
				return b.CreateFPTrunc(value, llvmTypeTo, ""), nil
			} else if sizeFrom < sizeTo {
				return b.CreateFPExt(value, llvmTypeTo, ""), nil
			} else {
				return value, nil
			}
		}

		if typeFrom.Info()&typecheck.IsFloat != 0 && typeTo.Info()&typecheck.IsInteger != 0 {
			var significandBits int
			switch typeFrom.Kind() {
			case typecheck.Float32:
				significandBits = 23
			case typecheck.Float64:
				significandBits = 52
			}
			if typeTo.Info()&typecheck.IsUnsigned != 0 {
				max := ^(^uint64(0) << uint(llvmTypeTo.IntTypeWidth()))
				maxFloat := float64(max)
				if bits.Len64(max) > significandBits {
					maxFloat = float64(max & (^uint64(0) << uint(bits.Len64(max)-significandBits)))
				}
				positive := b.CreateFCmp(llvm.FloatOLE, llvm.ConstNull(llvmTypeFrom), value, "positive")
				withinMax := b.CreateFCmp(llvm.FloatOLE, value, llvm.ConstFloat(llvmTypeFrom, maxFloat), "withinmax")
				inBounds := b.CreateAnd(positive, withinMax, "inbounds")
				saturated := b.CreateSelect(positive,
					llvm.ConstInt(llvmTypeTo, max, false),
					llvm.ConstNull(llvmTypeTo),
					"saturated",
				)
				normal := b.CreateFPToUI(value, llvmTypeTo, "normal")
				return b.CreateSelect(inBounds, normal, saturated, ""), nil
			} else {
				min := uint64(1) << uint(llvmTypeTo.IntTypeWidth()-1)
				minFloat := -float64(min)
				max := ^(^uint64(0) << uint(llvmTypeTo.IntTypeWidth()-1))
				maxFloat := float64(max)
				if bits.Len64(max) > significandBits {
					maxFloat = float64(max & (^uint64(0) << uint(bits.Len64(max)-significandBits)))
				}
				aboveMin := b.CreateFCmp(llvm.FloatOLE, llvm.ConstFloat(llvmTypeFrom, minFloat), value, "abovemin")
				belowMax := b.CreateFCmp(llvm.FloatOLE, value, llvm.ConstFloat(llvmTypeFrom, maxFloat), "belowmax")
				inBounds := b.CreateAnd(aboveMin, belowMax, "inbounds")
				saturated := b.CreateSelect(aboveMin,
					llvm.ConstInt(llvmTypeTo, max, false),
					llvm.ConstInt(llvmTypeTo, min, false),
					"saturated",
				)
				saturated = b.CreateSelect(b.CreateFCmp(llvm.FloatUNO, value, value, "isnan"),
					llvm.ConstNull(llvmTypeTo),
					saturated,
					"remapped",
				)
				normal := b.CreateFPToSI(value, llvmTypeTo, "normal")
				return b.CreateSelect(inBounds, normal, saturated, ""), nil
			}
		}

		if typeFrom.Info()&typecheck.IsInteger != 0 && typeTo.Info()&typecheck.IsFloat != 0 {
			if typeFrom.Info()&typecheck.IsUnsigned != 0 {
				return b.CreateUIToFP(value, llvmTypeTo, ""), nil
			}
			return b.CreateSIToFP(value, llvmTypeTo, ""), nil
		}

		return llvm.Value{}, b.makeError(pos, "todo: convert: basic non-integer type: "+typeFrom.String()+" -> "+typeTo.String())

	case *typecheck.Slice:
		if !isStringLikeTC(typeFrom.Underlying()) {
			panic("can only convert from a string/[]byte to a slice")
		}
		elemType := typeTo.Elem().Underlying().(*typecheck.Basic)
		switch elemType.Kind() {
		case typecheck.Uint8:
			ptr := b.CreateExtractValue(value, 0, "")
			ln := b.CreateExtractValue(value, 1, "")
			cp := b.CreateExtractValue(value, 2, "")
			slice := llvm.Undef(b.getLLVMTypeTC(typeTo))
			slice = b.CreateInsertValue(slice, ptr, 0, "")
			slice = b.CreateInsertValue(slice, ln, 1, "")
			slice = b.CreateInsertValue(slice, cp, 2, "")
			return slice, nil
		case typecheck.Int32:
			return b.createRuntimeCall("stringToRunes", []llvm.Value{value}, ""), nil
		default:
			panic("unexpected type in string to slice conversion")
		}

	default:
		return llvm.Value{}, b.makeError(pos, "todo: convert "+typeTo.String()+" <- "+typeFrom.String())
	}
}

// createUnOp creates LLVM IR for a given Go unary operation.
// receive operator which is handled in the runtime directly.

func (b *builder) createUnOpTC(unop *mxssa.UnOp) (llvm.Value, error) {
	x := b.getValueTC(unop.X, unop.Pos())
	switch unop.Op {
	case token.NOT:
		return b.CreateNot(x, ""), nil
	case token.SUB:
		if typ, ok := unop.X.Type().Underlying().(*typecheck.Basic); ok {
			if typ.Info()&typecheck.IsInteger != 0 {
				return b.CreateSub(llvm.ConstInt(x.Type(), 0, false), x, ""), nil
			} else if typ.Info()&typecheck.IsFloat != 0 {
				return b.CreateFNeg(x, ""), nil
			} else {
				return llvm.Value{}, b.makeError(unop.Pos(), "todo: unknown basic type for negate: "+typ.String())
			}
		} else {
			return llvm.Value{}, b.makeError(unop.Pos(), "todo: unknown type for negate: "+unop.X.Type().Underlying().String())
		}
	case token.MUL:
		valueType := b.getLLVMTypeTC(unop.X.Type().Underlying().(*typecheck.Pointer).Elem())
		if b.targetData.TypeAllocSize(valueType) == 0 {
			return llvm.ConstNull(valueType), nil
		} else if g, ok := unop.X.(*mxssa.Global); ok && strings.HasSuffix(g.Name(), "$funcaddr") {
			name := strings.TrimSuffix(g.Name(), "$funcaddr")
			pkg := b.mxFn.Pkg
			fn := pkg.Members[name].(*mxssa.Function)
			_, fnVal := b.getFunction(fn)
			if fnVal.IsNil() {
				return llvm.Value{}, b.makeError(unop.Pos(), "cgo function not found: "+name)
			}
			return fnVal, nil
		} else {
			b.createNilCheckTC(unop.X, x, "deref")
			load := b.CreateLoad(valueType, x, "")
			return load, nil
		}
	case token.XOR:
		return b.CreateXor(x, llvm.ConstInt(x.Type(), ^uint64(0), false), ""), nil
	case token.ARROW:
		return b.createChanRecvTC(unop), nil
	default:
		return llvm.Value{}, b.makeError(unop.Pos(), "todo: unknown unop")
	}
}

// isStringLike reports whether t is string or []byte (Moxie: string=[]byte).
func isStringLike(t types.Type) bool {
	switch t := t.(type) {
	case *types.Basic:
		return t.Info()&types.IsString != 0
	case *types.Slice:
		if b, ok := t.Elem().(*types.Basic); ok && b.Kind() == types.Byte {
			return true
		}
	}
	return false
}

func hashmapIsBinaryKey(keyType types.Type) bool {
	switch keyType := keyType.Underlying().(type) {
	case *types.Basic:
		return keyType.Info()&(types.IsBoolean|types.IsInteger) != 0
	case *types.Pointer:
		return true
	case *types.Struct:
		for i := 0; i < keyType.NumFields(); i++ {
			if !hashmapIsBinaryKey(keyType.Field(i).Type().Underlying()) {
				return false
			}
		}
		return true
	case *types.Array:
		return hashmapIsBinaryKey(keyType.Elem())
	default:
		return false
	}
}
