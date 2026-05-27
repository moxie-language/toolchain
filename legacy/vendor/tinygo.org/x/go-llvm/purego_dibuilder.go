//go:build purego

package llvm

import (
	"debug/dwarf"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

type DwarfTag uint32

const (
	DW_TAG_lexical_block   DwarfTag = 0x0b
	DW_TAG_compile_unit    DwarfTag = 0x11
	DW_TAG_variable        DwarfTag = 0x34
	DW_TAG_base_type       DwarfTag = 0x24
	DW_TAG_pointer_type    DwarfTag = 0x0F
	DW_TAG_structure_type  DwarfTag = 0x13
	DW_TAG_subroutine_type DwarfTag = 0x15
	DW_TAG_file_type       DwarfTag = 0x29
	DW_TAG_subprogram      DwarfTag = 0x2E
	DW_TAG_auto_variable   DwarfTag = 0x100
	DW_TAG_arg_variable    DwarfTag = 0x101
)

const (
	FlagPrivate = 1 << iota
	FlagProtected
	FlagFwdDecl
	FlagAppleBlock
	FlagReserved
	FlagVirtual
	FlagArtificial
	FlagExplicit
	FlagPrototyped
	FlagObjcClassComplete
	FlagObjectPointer
	FlagVector
	FlagStaticMember
	FlagIndirectVariable
)

type DwarfLang uint32

const DW_LANG_Go DwarfLang = 0x0016

type DwarfTypeEncoding uint32

const (
	DW_ATE_address         DwarfTypeEncoding = 0x01
	DW_ATE_boolean         DwarfTypeEncoding = 0x02
	DW_ATE_complex_float   DwarfTypeEncoding = 0x03
	DW_ATE_float           DwarfTypeEncoding = 0x04
	DW_ATE_signed          DwarfTypeEncoding = 0x05
	DW_ATE_signed_char     DwarfTypeEncoding = 0x06
	DW_ATE_unsigned        DwarfTypeEncoding = 0x07
	DW_ATE_unsigned_char   DwarfTypeEncoding = 0x08
	DW_ATE_imaginary_float DwarfTypeEncoding = 0x09
)

type MetadataKind int

type DIBuilder struct {
	C uintptr
	m Module
}

var (
	symCreateDIBuilder      uintptr
	symDisposeDIBuilder     uintptr
	symDIBuilderFinalize    uintptr
	symDIBCreateFile        uintptr

	// Shims for >15 arg functions (in libmoxie-llvm-glue.so)
	symMoxieCreateCompileUnit uintptr
	symMoxieCreateStructType  uintptr
	symDIBCreateLexBlock    uintptr
	symDIBCreateLexBlockFile uintptr
	symDIBCreateFunction    uintptr
	symDIBCreateAutoVar     uintptr
	symDIBCreateParamVar    uintptr
	symDIBCreateBasicType   uintptr
	symDIBCreatePointerType uintptr
	symDIBCreateSubrtnType  uintptr
	symDIBCreateMemberType  uintptr
	symDIBCreateArrayType   uintptr
	symDIBCreateTypedef     uintptr
	symDIBCreateReplaceableType uintptr
	symDIBCreateGlobalVarExpr   uintptr
	symDIBGetOrCreateSubrange   uintptr
	symDIBGetOrCreateArray      uintptr
	symDIBGetOrCreateTypeArray  uintptr
	symDIBCreateExpression      uintptr
	symDIBCreateDebugLocation   uintptr

	// Metadata operations
	symSetSubprogram     uintptr
	symGetSubprogram     uintptr
	symTemporaryMDNode   uintptr
	symMDReplaceAllUses  uintptr
	symGetMetadataKind   uintptr

	// DIFile/DILocation accessors
	symDIFileGetDir      uintptr
	symDIFileGetFilename uintptr
	symDIFileGetSource   uintptr
	symDILocGetLine      uintptr
	symDILocGetColumn    uintptr
	symDILocGetScope     uintptr
	symDILocGetInlinedAt uintptr
	symDIScopeGetFile    uintptr
	symDISubprogramGetLine uintptr

	// Glue: insert dbg value record
	symGoInsertDbgValueRecord uintptr
)

func registerDIBuilder() {
	symCreateDIBuilder = mustSym(libLLVM, "LLVMCreateDIBuilder")
	symDisposeDIBuilder = mustSym(libLLVM, "LLVMDisposeDIBuilder")
	symDIBuilderFinalize = mustSym(libLLVM, "LLVMDIBuilderFinalize")
	symMoxieCreateCompileUnit = mustSym(libGlue, "MoxieCreateCompileUnit")
	symDIBCreateFile = mustSym(libLLVM, "LLVMDIBuilderCreateFile")
	symDIBCreateLexBlock = mustSym(libLLVM, "LLVMDIBuilderCreateLexicalBlock")
	symDIBCreateLexBlockFile = mustSym(libLLVM, "LLVMDIBuilderCreateLexicalBlockFile")
	symDIBCreateFunction = mustSym(libLLVM, "LLVMDIBuilderCreateFunction")
	symDIBCreateAutoVar = mustSym(libLLVM, "LLVMDIBuilderCreateAutoVariable")
	symDIBCreateParamVar = mustSym(libLLVM, "LLVMDIBuilderCreateParameterVariable")
	symDIBCreateBasicType = mustSym(libLLVM, "LLVMDIBuilderCreateBasicType")
	symDIBCreatePointerType = mustSym(libLLVM, "LLVMDIBuilderCreatePointerType")
	symDIBCreateSubrtnType = mustSym(libLLVM, "LLVMDIBuilderCreateSubroutineType")
	symMoxieCreateStructType = mustSym(libGlue, "MoxieCreateStructType")
	symDIBCreateMemberType = mustSym(libLLVM, "LLVMDIBuilderCreateMemberType")
	symDIBCreateArrayType = mustSym(libLLVM, "LLVMDIBuilderCreateArrayType")
	symDIBCreateTypedef = mustSym(libLLVM, "LLVMDIBuilderCreateTypedef")
	symDIBCreateReplaceableType = mustSym(libLLVM, "LLVMDIBuilderCreateReplaceableCompositeType")
	symDIBCreateGlobalVarExpr = mustSym(libLLVM, "LLVMDIBuilderCreateGlobalVariableExpression")
	symDIBGetOrCreateSubrange = mustSym(libLLVM, "LLVMDIBuilderGetOrCreateSubrange")
	symDIBGetOrCreateArray = mustSym(libLLVM, "LLVMDIBuilderGetOrCreateArray")
	symDIBGetOrCreateTypeArray = mustSym(libLLVM, "LLVMDIBuilderGetOrCreateTypeArray")
	symDIBCreateExpression = mustSym(libLLVM, "LLVMDIBuilderCreateExpression")
	symDIBCreateDebugLocation = mustSym(libLLVM, "LLVMDIBuilderCreateDebugLocation")

	symSetSubprogram = mustSym(libLLVM, "LLVMSetSubprogram")
	symGetSubprogram = mustSym(libLLVM, "LLVMGetSubprogram")
	symTemporaryMDNode = mustSym(libLLVM, "LLVMTemporaryMDNode")
	symMDReplaceAllUses = mustSym(libLLVM, "LLVMMetadataReplaceAllUsesWith")
	symGetMetadataKind = mustSym(libLLVM, "LLVMGetMetadataKind")

	symDIFileGetDir = mustSym(libLLVM, "LLVMDIFileGetDirectory")
	symDIFileGetFilename = mustSym(libLLVM, "LLVMDIFileGetFilename")
	symDIFileGetSource = mustSym(libLLVM, "LLVMDIFileGetSource")
	symDILocGetLine = mustSym(libLLVM, "LLVMDILocationGetLine")
	symDILocGetColumn = mustSym(libLLVM, "LLVMDILocationGetColumn")
	symDILocGetScope = mustSym(libLLVM, "LLVMDILocationGetScope")
	symDILocGetInlinedAt = mustSym(libLLVM, "LLVMDILocationGetInlinedAt")
	symDIScopeGetFile = mustSym(libLLVM, "LLVMDIScopeGetFile")
	symDISubprogramGetLine = mustSym(libLLVM, "LLVMDISubprogramGetLine")

	if libGlue != 0 {
		symGoInsertDbgValueRecord = trySym(libGlue, "LLVMGoDIBuilderInsertDbgValueRecordAtEnd")
	}
}

func NewDIBuilder(m Module) *DIBuilder {
	r, _, _ := purego.SyscallN(symCreateDIBuilder, m.C)
	return &DIBuilder{C: r, m: m}
}

func (d *DIBuilder) Destroy() {
	purego.SyscallN(symDisposeDIBuilder, d.C)
}

func (d *DIBuilder) Finalize() {
	purego.SyscallN(symDIBuilderFinalize, d.C)
}

type DICompileUnit struct {
	Language       DwarfLang
	File           string
	Dir            string
	Producer       string
	Optimized      bool
	Flags          string
	RuntimeVersion int
	SysRoot        string
	SDK            string
}

func (d *DIBuilder) CreateCompileUnit(cu DICompileUnit) Metadata {
	cfile := cString(cu.File)
	cdir := cString(cu.Dir)
	fileMD, _, _ := purego.SyscallN(symDIBCreateFile, d.C, cfile, uintptr(len(cu.File)), cdir, uintptr(len(cu.Dir)))
	cprod := cString(cu.Producer)
	csysroot := cString(cu.SysRoot)
	csdk := cString(cu.SDK)
	r, _, _ := purego.SyscallN(symMoxieCreateCompileUnit, d.C,
		uintptr(cu.Language), fileMD,
		cprod, uintptr(len(cu.Producer)),
		boolToInt(cu.Optimized),
		uintptr(cu.RuntimeVersion),
		csysroot, uintptr(len(cu.SysRoot)),
		csdk, uintptr(len(cu.SDK)))
	runtime.KeepAlive(cu)
	return Metadata{r}
}

func (d *DIBuilder) CreateFile(filename, dir string) Metadata {
	cfile := cString(filename)
	cdir := cString(dir)
	r, _, _ := purego.SyscallN(symDIBCreateFile, d.C, cfile, uintptr(len(filename)), cdir, uintptr(len(dir)))
	runtime.KeepAlive(filename)
	runtime.KeepAlive(dir)
	return Metadata{r}
}

type DILexicalBlock struct {
	File   Metadata
	Line   int
	Column int
}

func (d *DIBuilder) CreateLexicalBlock(scope Metadata, b DILexicalBlock) Metadata {
	r, _, _ := purego.SyscallN(symDIBCreateLexBlock, d.C, scope.C, b.File.C, uintptr(b.Line), uintptr(b.Column))
	return Metadata{r}
}

func (d *DIBuilder) CreateLexicalBlockFile(scope, file Metadata, discriminator int) Metadata {
	r, _, _ := purego.SyscallN(symDIBCreateLexBlockFile, d.C, scope.C, file.C, uintptr(discriminator))
	return Metadata{r}
}

type DIFunction struct {
	Name         string
	LinkageName  string
	File         Metadata
	Line         int
	Type         Metadata
	LocalToUnit  bool
	IsDefinition bool
	ScopeLine    int
	Flags        int
	Optimized    bool
}

func (d *DIBuilder) CreateFunction(scope Metadata, f DIFunction) Metadata {
	cname := cString(f.Name)
	clinkage := cString(f.LinkageName)
	r, _, _ := purego.SyscallN(symDIBCreateFunction, d.C, scope.C,
		cname, uintptr(len(f.Name)),
		clinkage, uintptr(len(f.LinkageName)),
		f.File.C, uintptr(f.Line), f.Type.C,
		boolToInt(f.LocalToUnit), boolToInt(f.IsDefinition),
		uintptr(f.ScopeLine), uintptr(f.Flags), boolToInt(f.Optimized))
	runtime.KeepAlive(f.Name)
	runtime.KeepAlive(f.LinkageName)
	return Metadata{r}
}

type DIAutoVariable struct {
	Name           string
	File           Metadata
	Line           int
	Type           Metadata
	AlwaysPreserve bool
	Flags          int
	AlignInBits    uint32
}

func (d *DIBuilder) CreateAutoVariable(scope Metadata, v DIAutoVariable) Metadata {
	cname := cString(v.Name)
	r, _, _ := purego.SyscallN(symDIBCreateAutoVar, d.C, scope.C,
		cname, uintptr(len(v.Name)),
		v.File.C, uintptr(v.Line), v.Type.C,
		boolToInt(v.AlwaysPreserve), uintptr(v.Flags), uintptr(v.AlignInBits))
	runtime.KeepAlive(v.Name)
	return Metadata{r}
}

type DIParameterVariable struct {
	Name           string
	File           Metadata
	Line           int
	Type           Metadata
	AlwaysPreserve bool
	Flags          int
	ArgNo          int
}

func (d *DIBuilder) CreateParameterVariable(scope Metadata, v DIParameterVariable) Metadata {
	cname := cString(v.Name)
	r, _, _ := purego.SyscallN(symDIBCreateParamVar, d.C, scope.C,
		cname, uintptr(len(v.Name)),
		uintptr(v.ArgNo),
		v.File.C, uintptr(v.Line), v.Type.C,
		boolToInt(v.AlwaysPreserve), uintptr(v.Flags))
	runtime.KeepAlive(v.Name)
	return Metadata{r}
}

type DIBasicType struct {
	Name       string
	SizeInBits uint64
	Encoding   DwarfTypeEncoding
}

func (d *DIBuilder) CreateBasicType(t DIBasicType) Metadata {
	cname := cString(t.Name)
	r, _, _ := purego.SyscallN(symDIBCreateBasicType, d.C,
		cname, uintptr(len(t.Name)),
		uintptr(t.SizeInBits), uintptr(t.Encoding), 0)
	runtime.KeepAlive(t.Name)
	return Metadata{r}
}

type DIPointerType struct {
	Pointee     Metadata
	SizeInBits  uint64
	AlignInBits uint32
	AddressSpace uint32
	Name        string
}

func (d *DIBuilder) CreatePointerType(t DIPointerType) Metadata {
	cname := cString(t.Name)
	r, _, _ := purego.SyscallN(symDIBCreatePointerType, d.C,
		t.Pointee.C, uintptr(t.SizeInBits), uintptr(t.AlignInBits), uintptr(t.AddressSpace),
		cname, uintptr(len(t.Name)))
	runtime.KeepAlive(t.Name)
	return Metadata{r}
}

type DISubroutineType struct {
	File       Metadata
	Parameters []Metadata
	Flags      int
}

func (d *DIBuilder) CreateSubroutineType(t DISubroutineType) Metadata {
	r, _, _ := purego.SyscallN(symDIBCreateSubrtnType, d.C,
		t.File.C, metadataRefPtr(t.Parameters), uintptr(len(t.Parameters)), uintptr(t.Flags))
	return Metadata{r}
}

type DIStructType struct {
	Name        string
	File        Metadata
	Line        int
	SizeInBits  uint64
	AlignInBits uint32
	Flags       int
	DerivedFrom Metadata
	Elements    []Metadata
	VTableHolder Metadata
	UniqueID    string
}

func (d *DIBuilder) CreateStructType(scope Metadata, t DIStructType) Metadata {
	cname := cString(t.Name)
	cunique := cString(t.UniqueID)
	var elemsPtr uintptr
	if len(t.Elements) > 0 {
		elemsPtr = metadataRefPtr(t.Elements)
	}
	r, _, _ := purego.SyscallN(symMoxieCreateStructType, d.C, scope.C,
		cname, uintptr(len(t.Name)),
		t.File.C, uintptr(t.Line),
		uintptr(t.SizeInBits), uintptr(t.AlignInBits), uintptr(t.Flags),
		t.DerivedFrom.C, elemsPtr, uintptr(len(t.Elements)),
		t.VTableHolder.C,
		cunique, uintptr(len(t.UniqueID)))
	runtime.KeepAlive(t.Name)
	runtime.KeepAlive(t.UniqueID)
	return Metadata{r}
}

type DIMemberType struct {
	Name         string
	File         Metadata
	Line         int
	SizeInBits   uint64
	AlignInBits  uint32
	OffsetInBits uint64
	Flags        int
	Type         Metadata
}

func (d *DIBuilder) CreateMemberType(scope Metadata, t DIMemberType) Metadata {
	cname := cString(t.Name)
	r, _, _ := purego.SyscallN(symDIBCreateMemberType, d.C, scope.C,
		cname, uintptr(len(t.Name)),
		t.File.C, uintptr(t.Line),
		uintptr(t.SizeInBits), uintptr(t.AlignInBits), uintptr(t.OffsetInBits),
		uintptr(t.Flags), t.Type.C)
	runtime.KeepAlive(t.Name)
	return Metadata{r}
}

type DISubrange struct {
	Lo    int64
	Count int64
}

type DIArrayType struct {
	SizeInBits  uint64
	AlignInBits uint32
	ElementType Metadata
	Subscripts  []DISubrange
}

func (d *DIBuilder) CreateArrayType(t DIArrayType) Metadata {
	subscriptsSlice := make([]Metadata, len(t.Subscripts))
	for i, s := range t.Subscripts {
		subscriptsSlice[i] = d.getOrCreateSubrange(s.Lo, s.Count)
	}
	r, _, _ := purego.SyscallN(symDIBCreateArrayType, d.C,
		uintptr(t.SizeInBits), uintptr(t.AlignInBits), t.ElementType.C,
		metadataRefPtr(subscriptsSlice), uintptr(len(subscriptsSlice)))
	return Metadata{r}
}

type DITypedef struct {
	Type        Metadata
	Name        string
	File        Metadata
	Line        int
	Context     Metadata
	AlignInBits uint32
}

func (d *DIBuilder) CreateTypedef(t DITypedef) Metadata {
	cname := cString(t.Name)
	r, _, _ := purego.SyscallN(symDIBCreateTypedef, d.C,
		t.Type.C, cname, uintptr(len(t.Name)),
		t.File.C, uintptr(t.Line), t.Context.C, uintptr(t.AlignInBits))
	runtime.KeepAlive(t.Name)
	return Metadata{r}
}

type DIReplaceableCompositeType struct {
	Tag         dwarf.Tag
	Name        string
	File        Metadata
	Line        int
	RuntimeLang int
	SizeInBits  uint64
	AlignInBits uint32
	Flags       int
	UniqueID    string
}

func (d *DIBuilder) CreateReplaceableCompositeType(scope Metadata, t DIReplaceableCompositeType) Metadata {
	cname := cString(t.Name)
	cunique := cString(t.UniqueID)
	r, _, _ := purego.SyscallN(symDIBCreateReplaceableType, d.C,
		uintptr(t.Tag), cname, uintptr(len(t.Name)),
		scope.C, t.File.C, uintptr(t.Line),
		uintptr(t.RuntimeLang), uintptr(t.SizeInBits), uintptr(t.AlignInBits),
		uintptr(t.Flags), cunique, uintptr(len(t.UniqueID)))
	runtime.KeepAlive(t.Name)
	runtime.KeepAlive(t.UniqueID)
	return Metadata{r}
}

type DIGlobalVariableExpression struct {
	Name        string
	LinkageName string
	File        Metadata
	Line        int
	Type        Metadata
	LocalToUnit bool
	Expr        Metadata
	Decl        Metadata
	AlignInBits uint32
}

func (d *DIBuilder) CreateGlobalVariableExpression(scope Metadata, g DIGlobalVariableExpression) Metadata {
	cname := cString(g.Name)
	clinkage := cString(g.LinkageName)
	r, _, _ := purego.SyscallN(symDIBCreateGlobalVarExpr, d.C, scope.C,
		cname, uintptr(len(g.Name)),
		clinkage, uintptr(len(g.LinkageName)),
		g.File.C, uintptr(g.Line), g.Type.C,
		boolToInt(g.LocalToUnit), g.Expr.C, g.Decl.C, uintptr(g.AlignInBits))
	runtime.KeepAlive(g.Name)
	runtime.KeepAlive(g.LinkageName)
	return Metadata{r}
}

func (d *DIBuilder) CreateExpression(addr []uint64) Metadata {
	var ptr uintptr
	if len(addr) > 0 {
		ptr = uintptr(unsafe.Pointer(&addr[0]))
	}
	r, _, _ := purego.SyscallN(symDIBCreateExpression, d.C, ptr, uintptr(len(addr)))
	return Metadata{r}
}

func (d *DIBuilder) getOrCreateSubrange(lo, count int64) Metadata {
	r, _, _ := purego.SyscallN(symDIBGetOrCreateSubrange, d.C, uintptr(lo), uintptr(count))
	return Metadata{r}
}

func (d *DIBuilder) getOrCreateArray(values []Metadata) Metadata {
	r, _, _ := purego.SyscallN(symDIBGetOrCreateArray, d.C, metadataRefPtr(values), uintptr(len(values)))
	return Metadata{r}
}

func (d *DIBuilder) getOrCreateTypeArray(values []Metadata) Metadata {
	r, _, _ := purego.SyscallN(symDIBGetOrCreateTypeArray, d.C, metadataRefPtr(values), uintptr(len(values)))
	return Metadata{r}
}

func (d *DIBuilder) CreateDebugLocation(ctx Context, line, col uint, scope, inlinedAt Metadata) Metadata {
	r, _, _ := purego.SyscallN(symDIBCreateDebugLocation, ctx.C, uintptr(line), uintptr(col), scope.C, inlinedAt.C)
	return Metadata{r}
}

func (d *DIBuilder) InsertValueAtEnd(v Value, diVarInfo, expr Metadata, l DebugLoc, bb BasicBlock) {
	if symGoInsertDbgValueRecord == 0 {
		return
	}
	loc, _, _ := purego.SyscallN(symDIBCreateDebugLocation, d.m.Context().C,
		uintptr(l.Line), uintptr(l.Col), l.Scope.C, l.InlinedAt.C)
	purego.SyscallN(symGoInsertDbgValueRecord, d.C, v.C, diVarInfo.C, expr.C, loc, bb.C)
}

// Metadata operations

func (v Value) SetSubprogram(sp Metadata) {
	purego.SyscallN(symSetSubprogram, v.C, sp.C)
}

func (v Value) Subprogram() Metadata {
	r, _, _ := purego.SyscallN(symGetSubprogram, v.C)
	return Metadata{r}
}

func (c Context) TemporaryMDNode(mds []Metadata) Metadata {
	r, _, _ := purego.SyscallN(symTemporaryMDNode, c.C, metadataRefPtr(mds), uintptr(len(mds)))
	return Metadata{r}
}

func (md Metadata) ReplaceAllUsesWith(new Metadata) {
	purego.SyscallN(symMDReplaceAllUses, md.C, new.C)
}

func (md Metadata) Kind() MetadataKind {
	r, _, _ := purego.SyscallN(symGetMetadataKind, md.C)
	return MetadataKind(r)
}

// DIFile accessors

func (md Metadata) FileDirectory() string {
	var length uintptr
	r, _, _ := purego.SyscallN(symDIFileGetDir, md.C, uintptr(unsafe.Pointer(&length)))
	return goStringN(r, int(length))
}

func (md Metadata) FileFilename() string {
	var length uintptr
	r, _, _ := purego.SyscallN(symDIFileGetFilename, md.C, uintptr(unsafe.Pointer(&length)))
	return goStringN(r, int(length))
}

func (md Metadata) FileSource() string {
	var length uintptr
	r, _, _ := purego.SyscallN(symDIFileGetSource, md.C, uintptr(unsafe.Pointer(&length)))
	return goStringN(r, int(length))
}

// DILocation accessors

func (md Metadata) LocationLine() uint {
	r, _, _ := purego.SyscallN(symDILocGetLine, md.C)
	return uint(r)
}

func (md Metadata) LocationColumn() uint {
	r, _, _ := purego.SyscallN(symDILocGetColumn, md.C)
	return uint(r)
}

func (md Metadata) LocationScope() Metadata {
	r, _, _ := purego.SyscallN(symDILocGetScope, md.C)
	return Metadata{r}
}

func (md Metadata) LocationInlinedAt() Metadata {
	r, _, _ := purego.SyscallN(symDILocGetInlinedAt, md.C)
	return Metadata{r}
}

func (md Metadata) ScopeFile() Metadata {
	r, _, _ := purego.SyscallN(symDIScopeGetFile, md.C)
	return Metadata{r}
}

func (md Metadata) SubprogramLine() uint {
	r, _, _ := purego.SyscallN(symDISubprogramGetLine, md.C)
	return uint(r)
}

// Encoding helpers for DWARF

func DwarfTypeEncodingForType(tag dwarf.Tag) DwarfTypeEncoding {
	switch tag {
	case dwarf.TagBaseType:
		return DW_ATE_signed
	default:
		return DW_ATE_signed
	}
}
