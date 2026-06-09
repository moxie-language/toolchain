package llvmpure

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Function pointers resolved at init time.
var (
	// Context
	symContextCreate  uintptr
	symContextDispose uintptr
	symGetGlobalCtx   uintptr
	symGetMDKindIDCtx uintptr
	symGetMDKindID    uintptr

	// Attribute
	symGetEnumAttrKindForName uintptr
	symCreateEnumAttr         uintptr
	symCreateTypeAttr         uintptr
	symCreateStringAttr       uintptr
	symGetEnumAttrKind        uintptr
	symGetEnumAttrValue       uintptr
	symGetStringAttrKind      uintptr
	symGetStringAttrValue     uintptr

	// Module
	symModuleCreateWithNameInCtx uintptr
	symDisposeModule             uintptr
	symGetDataLayout             uintptr
	symSetDataLayout             uintptr
	symGetTarget                 uintptr
	symSetTarget                 uintptr
	symGetModuleContext          uintptr
	symGetTypeByName             uintptr
	symPrintModuleToString       uintptr
	symDisposeMessage            uintptr
	symSetModuleInlineAsm        uintptr
	symDumpModule                uintptr

	// Integer types
	symInt1TypeInCtx  uintptr
	symInt8TypeInCtx  uintptr
	symInt16TypeInCtx uintptr
	symInt32TypeInCtx uintptr
	symInt64TypeInCtx uintptr
	symIntTypeInCtx   uintptr
	symGetIntTypeWidth uintptr

	// Float types
	symFloatTypeInCtx    uintptr
	symDoubleTypeInCtx   uintptr
	symX86FP80TypeInCtx  uintptr
	symFP128TypeInCtx    uintptr
	symPPCFP128TypeInCtx uintptr

	// Function types
	symFunctionType     uintptr
	symIsFunctionVarArg uintptr
	symGetReturnType    uintptr
	symCountParamTypes  uintptr
	symGetParamTypes    uintptr

	// Struct types
	symStructTypeInCtx         uintptr
	symStructCreateNamed       uintptr
	symGetStructName           uintptr
	symStructSetBody           uintptr
	symIsPackedStruct          uintptr
	symCountStructElementTypes uintptr
	symGetStructElementTypes   uintptr

	// Sequence types
	symArrayType             uintptr
	symPointerType           uintptr
	symVectorType            uintptr
	symGetElementType        uintptr
	symGetArrayLength        uintptr
	symGetPointerAddressSpace uintptr
	symGetVectorSize         uintptr
	symGetNumContainedTypes  uintptr
	symGetSubtypes           uintptr

	// Other types
	symVoidTypeInCtx  uintptr
	symLabelTypeInCtx uintptr
	symTokenTypeInCtx uintptr

	// Type inspection
	symGetTypeKind    uintptr
	symGetTypeContext  uintptr

	// Value operations
	symTypeOf              uintptr
	symGetValueName        uintptr
	symSetValueName        uintptr
	symDumpValue           uintptr
	symReplaceAllUsesWith  uintptr
	symHasMetadata         uintptr
	symGetMetadata         uintptr
	symPrintValueToString  uintptr

	// Constant scalars
	symConstInt          uintptr
	symConstFloat        uintptr
	symConstNull         uintptr
	symConstAllOnes      uintptr
	symGetUndef          uintptr
	symConstPointerNull  uintptr
	symIsConstant        uintptr
	symIsNull            uintptr
	symIsUndef           uintptr
	symConstIntGetZExt   uintptr
	symConstIntGetSExt   uintptr
	symConstFloatGetDouble uintptr

	// Constant expressions
	symConstNeg          uintptr
	symConstNSWNeg       uintptr
	symConstNot          uintptr
	symConstAdd          uintptr
	symConstNSWAdd       uintptr
	symConstSub          uintptr
	symConstNSWSub       uintptr
	symConstMul          uintptr
	symConstNSWMul       uintptr
	symConstXor          uintptr
	symConstGEP2         uintptr
	symConstInBoundsGEP2 uintptr
	symConstTrunc        uintptr
	symConstSExt         uintptr
	symConstZExt         uintptr
	symConstFPTrunc      uintptr
	symConstFPExt        uintptr
	symConstUIToFP       uintptr
	symConstSIToFP       uintptr
	symConstFPToUI       uintptr
	symConstFPToSI       uintptr
	symConstPtrToInt     uintptr
	symConstIntToPtr     uintptr
	symConstBitCast      uintptr
	symConstICmp         uintptr
	symConstFCmp         uintptr
	symConstExtractValue uintptr
	symConstInsertValue  uintptr

	// Constant aggregates
	symConstStringInCtx uintptr
	symConstStructInCtx uintptr
	symConstArray        uintptr
	symConstNamedStruct  uintptr

	// Global values
	symGetLinkage     uintptr
	symSetLinkage     uintptr
	symGetSection     uintptr
	symSetSection     uintptr
	symGetVisibility  uintptr
	symSetVisibility  uintptr
	symGetAlignment   uintptr
	symSetAlignment   uintptr

	// Global variables
	symAddGlobal           uintptr
	symGetNamedGlobal      uintptr
	symGetFirstGlobal      uintptr
	symGetNextGlobal       uintptr
	symGetInitializer      uintptr
	symSetInitializer      uintptr
	symIsGlobalConstant    uintptr
	symSetGlobalConstant   uintptr
	symSetThreadLocal      uintptr
	symIsThreadLocal       uintptr
	symSetExternallyInit   uintptr
	symIsExternallyInit    uintptr
	symDeleteGlobal        uintptr
	symSetUnnamedAddr      uintptr
	symGetUnnamedAddr      uintptr

	// Functions
	symAddFunction       uintptr
	symGetNamedFunction  uintptr
	symGetFirstFunction  uintptr
	symGetNextFunction   uintptr
	symDeleteFunction    uintptr
	symCountParams       uintptr
	symGetParams         uintptr
	symGetParam          uintptr
	symSetFunctionCallConv uintptr
	symGetFunctionCallConv uintptr
	symAddFuncAttr        uintptr
	symGetEnumFuncAttr    uintptr
	symRemoveEnumFuncAttr uintptr
	symAddTargetDepFuncAttr uintptr

	// Basic blocks
	symAppendBasicBlockInCtx uintptr
	symInsertBasicBlockInCtx uintptr
	symGetBasicBlockParent   uintptr
	symGetEntryBasicBlock    uintptr
	symGetFirstBasicBlock    uintptr
	symGetLastBasicBlock     uintptr
	symGetNextBasicBlock     uintptr
	symGetPreviousBasicBlock uintptr
	symCountBasicBlocks      uintptr
	symMoveBasicBlockAfter   uintptr
	symMoveBasicBlockBefore  uintptr
	symDeleteBasicBlock      uintptr
	symBasicBlockAsValue     uintptr

	// Instructions
	symGetFirstInstruction  uintptr
	symGetLastInstruction   uintptr
	symGetNextInstruction   uintptr
	symGetInstructionOpcode uintptr
	symInstructionEraseFromParent uintptr
	symGetInstructionParent uintptr
	symIsATerminatorInst    uintptr

	// Builder
	symCreateBuilderInCtx     uintptr
	symPositionBuilderAtEnd   uintptr
	symPositionBuilderBefore  uintptr
	symGetInsertBlock         uintptr
	symDisposeBuilder         uintptr

	// Builder - terminators
	symBuildRetVoid    uintptr
	symBuildRet        uintptr
	symBuildBr         uintptr
	symBuildCondBr     uintptr
	symBuildSwitch     uintptr
	symBuildUnreachable uintptr

	// Builder - arithmetic
	symBuildAdd     uintptr
	symBuildNSWAdd  uintptr
	symBuildNUWAdd  uintptr
	symBuildFAdd    uintptr
	symBuildSub     uintptr
	symBuildNSWSub  uintptr
	symBuildNUWSub  uintptr
	symBuildFSub    uintptr
	symBuildMul     uintptr
	symBuildNSWMul  uintptr
	symBuildNUWMul  uintptr
	symBuildFMul    uintptr
	symBuildUDiv    uintptr
	symBuildSDiv    uintptr
	symBuildExactSDiv uintptr
	symBuildFDiv    uintptr
	symBuildURem    uintptr
	symBuildSRem    uintptr
	symBuildFRem    uintptr
	symBuildShl     uintptr
	symBuildLShr    uintptr
	symBuildAShr    uintptr
	symBuildAnd     uintptr
	symBuildOr      uintptr
	symBuildXor     uintptr
	symBuildNeg     uintptr
	symBuildNSWNeg  uintptr
	symBuildNot     uintptr
	symBuildFNeg    uintptr

	// Builder - memory
	symBuildAlloca   uintptr
	symBuildLoad2    uintptr
	symBuildStore    uintptr
	symBuildGEP2     uintptr
	symBuildInBoundsGEP2 uintptr
	symBuildStructGEP2   uintptr

	// Builder - casts
	symBuildTrunc        uintptr
	symBuildZExt         uintptr
	symBuildSExt         uintptr
	symBuildFPToUI       uintptr
	symBuildFPToSI       uintptr
	symBuildUIToFP       uintptr
	symBuildSIToFP       uintptr
	symBuildFPTrunc      uintptr
	symBuildFPExt        uintptr
	symBuildPtrToInt     uintptr
	symBuildIntToPtr     uintptr
	symBuildBitCast      uintptr
	symBuildPointerCast  uintptr

	// Builder - comparisons
	symBuildICmp uintptr
	symBuildFCmp uintptr

	// Builder - misc
	symBuildPhi          uintptr
	symBuildCall2        uintptr
	symBuildSelect       uintptr
	symBuildExtractValue uintptr
	symBuildInsertValue  uintptr

	// PHI
	symAddIncoming uintptr

	// Call site
	symSetInstructionCallConv  uintptr
	symGetInstructionCallConv  uintptr
	symAddCallSiteAttribute    uintptr
	symSetInstrParamAlignment  uintptr

	// Metadata (glue functions)
	symMDString2               uintptr
	symMDNode2                 uintptr
	symConstantAsMetadata      uintptr
	symSetMetadata2            uintptr
	symAddNamedMetadataOperand2 uintptr
	symGlobalObjectAddMetadata uintptr

	// Switch
	symAddCase uintptr

	// Landing pad
	symBuildLandingPad uintptr
	symSetCleanup      uintptr
	symAddClause       uintptr

	// Invoke
	symBuildInvoke2 uintptr

	// Resume
	symBuildResume uintptr

	// Atomics
	symBuildFence     uintptr
	symBuildAtomicRMW uintptr
	symBuildAtomicCmpXchg uintptr
	symSetOrdering    uintptr
	symSetAtomicSingleThread uintptr

	// Inline asm (glue)
	symGoGetInlineAsm uintptr

	// Debug location (glue)
	symGoSetCurrentDebugLocation uintptr
	symGoGetCurrentDebugLocation uintptr

	// Module print/verify
	symVerifyModule uintptr
	symVerifyFunction uintptr

	// Comdat
	symGetOrInsertComdat     uintptr
	symComdatSetSelectionKind uintptr
	symSetComdat             uintptr

	// Value type checks (IsA*)
	symIsAAllocaInst  uintptr
	symIsAConstant    uintptr
	symIsAConstantInt uintptr
	symIsAFunction    uintptr
	symIsAGlobalValue uintptr
	symIsAInstruction uintptr
	symIsAPHINode     uintptr
	symIsAUndefValue  uintptr

	// Value misc
	symIsDeclaration    uintptr
	symGlobalGetValType uintptr
	symGetLastParam     uintptr
	symGetOperand       uintptr
	symGetNumOperands   uintptr
	symSetVolatile      uintptr
	symInstructionRemoveFromParent uintptr
	symInstructionGetDebugLoc     uintptr
	symCountIncoming    uintptr
	symGetIncomingValue uintptr
	symGetIncomingBlock uintptr

	// Builder misc
	symInsertIntoBuilder uintptr
	symBuildBinOp        uintptr

	// Const misc
	symConstPointerCast uintptr
)

func registerAll() {
	registerTarget()
	registerDIBuilder()

	// Context
	symContextCreate = mustSym(libLLVM, "LLVMContextCreate")
	symContextDispose = mustSym(libLLVM, "LLVMContextDispose")
	symGetGlobalCtx = mustSym(libLLVM, "LLVMGetGlobalContext")
	symGetMDKindIDCtx = mustSym(libLLVM, "LLVMGetMDKindIDInContext")
	symGetMDKindID = mustSym(libLLVM, "LLVMGetMDKindID")

	// Attribute
	symGetEnumAttrKindForName = mustSym(libLLVM, "LLVMGetEnumAttributeKindForName")
	symCreateEnumAttr = mustSym(libLLVM, "LLVMCreateEnumAttribute")
	symCreateTypeAttr = mustSym(libLLVM, "LLVMCreateTypeAttribute")
	symCreateStringAttr = mustSym(libLLVM, "LLVMCreateStringAttribute")
	symGetEnumAttrKind = mustSym(libLLVM, "LLVMGetEnumAttributeKind")
	symGetEnumAttrValue = mustSym(libLLVM, "LLVMGetEnumAttributeValue")
	symGetStringAttrKind = mustSym(libLLVM, "LLVMGetStringAttributeKind")
	symGetStringAttrValue = mustSym(libLLVM, "LLVMGetStringAttributeValue")

	// Module
	symModuleCreateWithNameInCtx = mustSym(libLLVM, "LLVMModuleCreateWithNameInContext")
	symDisposeModule = mustSym(libLLVM, "LLVMDisposeModule")
	symGetDataLayout = mustSym(libLLVM, "LLVMGetDataLayout")
	symSetDataLayout = mustSym(libLLVM, "LLVMSetDataLayout")
	symGetTarget = mustSym(libLLVM, "LLVMGetTarget")
	symSetTarget = mustSym(libLLVM, "LLVMSetTarget")
	symGetModuleContext = mustSym(libLLVM, "LLVMGetModuleContext")
	symGetTypeByName = mustSym(libLLVM, "LLVMGetTypeByName")
	symPrintModuleToString = mustSym(libLLVM, "LLVMPrintModuleToString")
	symDisposeMessage = mustSym(libLLVM, "LLVMDisposeMessage")
	symSetModuleInlineAsm = mustSym(libLLVM, "LLVMSetModuleInlineAsm")
	symDumpModule = mustSym(libLLVM, "LLVMDumpModule")

	// Integer types
	symInt1TypeInCtx = mustSym(libLLVM, "LLVMInt1TypeInContext")
	symInt8TypeInCtx = mustSym(libLLVM, "LLVMInt8TypeInContext")
	symInt16TypeInCtx = mustSym(libLLVM, "LLVMInt16TypeInContext")
	symInt32TypeInCtx = mustSym(libLLVM, "LLVMInt32TypeInContext")
	symInt64TypeInCtx = mustSym(libLLVM, "LLVMInt64TypeInContext")
	symIntTypeInCtx = mustSym(libLLVM, "LLVMIntTypeInContext")
	symGetIntTypeWidth = mustSym(libLLVM, "LLVMGetIntTypeWidth")

	// Float types
	symFloatTypeInCtx = mustSym(libLLVM, "LLVMFloatTypeInContext")
	symDoubleTypeInCtx = mustSym(libLLVM, "LLVMDoubleTypeInContext")
	symX86FP80TypeInCtx = mustSym(libLLVM, "LLVMX86FP80TypeInContext")
	symFP128TypeInCtx = mustSym(libLLVM, "LLVMFP128TypeInContext")
	symPPCFP128TypeInCtx = mustSym(libLLVM, "LLVMPPCFP128TypeInContext")

	// Function types
	symFunctionType = mustSym(libLLVM, "LLVMFunctionType")
	symIsFunctionVarArg = mustSym(libLLVM, "LLVMIsFunctionVarArg")
	symGetReturnType = mustSym(libLLVM, "LLVMGetReturnType")
	symCountParamTypes = mustSym(libLLVM, "LLVMCountParamTypes")
	symGetParamTypes = mustSym(libLLVM, "LLVMGetParamTypes")

	// Struct types
	symStructTypeInCtx = mustSym(libLLVM, "LLVMStructTypeInContext")
	symStructCreateNamed = mustSym(libLLVM, "LLVMStructCreateNamed")
	symGetStructName = mustSym(libLLVM, "LLVMGetStructName")
	symStructSetBody = mustSym(libLLVM, "LLVMStructSetBody")
	symIsPackedStruct = mustSym(libLLVM, "LLVMIsPackedStruct")
	symCountStructElementTypes = mustSym(libLLVM, "LLVMCountStructElementTypes")
	symGetStructElementTypes = mustSym(libLLVM, "LLVMGetStructElementTypes")

	// Sequence types
	symArrayType = mustSym(libLLVM, "LLVMArrayType")
	symPointerType = mustSym(libLLVM, "LLVMPointerType")
	symVectorType = mustSym(libLLVM, "LLVMVectorType")
	symGetElementType = mustSym(libLLVM, "LLVMGetElementType")
	symGetArrayLength = mustSym(libLLVM, "LLVMGetArrayLength")
	symGetPointerAddressSpace = mustSym(libLLVM, "LLVMGetPointerAddressSpace")
	symGetVectorSize = mustSym(libLLVM, "LLVMGetVectorSize")
	symGetNumContainedTypes = mustSym(libLLVM, "LLVMGetNumContainedTypes")
	symGetSubtypes = mustSym(libLLVM, "LLVMGetSubtypes")

	// Other types
	symVoidTypeInCtx = mustSym(libLLVM, "LLVMVoidTypeInContext")
	symLabelTypeInCtx = mustSym(libLLVM, "LLVMLabelTypeInContext")
	symTokenTypeInCtx = mustSym(libLLVM, "LLVMTokenTypeInContext")

	// Type inspection
	symGetTypeKind = mustSym(libLLVM, "LLVMGetTypeKind")
	symGetTypeContext = mustSym(libLLVM, "LLVMGetTypeContext")

	// Value operations
	symTypeOf = mustSym(libLLVM, "LLVMTypeOf")
	symGetValueName = mustSym(libLLVM, "LLVMGetValueName")
	symSetValueName = mustSym(libLLVM, "LLVMSetValueName")
	symDumpValue = mustSym(libLLVM, "LLVMDumpValue")
	symReplaceAllUsesWith = mustSym(libLLVM, "LLVMReplaceAllUsesWith")
	symHasMetadata = mustSym(libLLVM, "LLVMHasMetadata")
	symGetMetadata = mustSym(libLLVM, "LLVMGetMetadata")
	symPrintValueToString = mustSym(libLLVM, "LLVMPrintValueToString")

	// Constant scalars
	symConstInt = mustSym(libLLVM, "LLVMConstInt")
	symConstFloat = mustSym(libLLVM, "LLVMConstReal")
	symConstNull = mustSym(libLLVM, "LLVMConstNull")
	symConstAllOnes = mustSym(libLLVM, "LLVMConstAllOnes")
	symGetUndef = mustSym(libLLVM, "LLVMGetUndef")
	symConstPointerNull = mustSym(libLLVM, "LLVMConstPointerNull")
	symIsConstant = mustSym(libLLVM, "LLVMIsConstant")
	symIsNull = mustSym(libLLVM, "LLVMIsNull")
	symIsUndef = mustSym(libLLVM, "LLVMIsUndef")
	symConstIntGetZExt = mustSym(libLLVM, "LLVMConstIntGetZExtValue")
	symConstIntGetSExt = mustSym(libLLVM, "LLVMConstIntGetSExtValue")
	symConstFloatGetDouble = mustSym(libLLVM, "LLVMConstRealGetDouble")

	// Constant expressions
	symConstNeg = mustSym(libLLVM, "LLVMConstNeg")
	symConstNSWNeg = mustSym(libLLVM, "LLVMConstNSWNeg")
	symConstNot = mustSym(libLLVM, "LLVMConstNot")
	symConstAdd = mustSym(libLLVM, "LLVMConstAdd")
	symConstNSWAdd = mustSym(libLLVM, "LLVMConstNSWAdd")
	symConstSub = mustSym(libLLVM, "LLVMConstSub")
	symConstNSWSub = mustSym(libLLVM, "LLVMConstNSWSub")
	symConstMul = mustSym(libLLVM, "LLVMConstMul")
	symConstNSWMul = mustSym(libLLVM, "LLVMConstNSWMul")
	symConstXor = mustSym(libLLVM, "LLVMConstXor")
	symConstGEP2 = mustSym(libLLVM, "LLVMConstGEP2")
	symConstInBoundsGEP2 = mustSym(libLLVM, "LLVMConstInBoundsGEP2")
	symConstTrunc = mustSym(libLLVM, "LLVMConstTrunc")
	symConstSExt = trySym(libLLVM, "LLVMConstSExt")
	symConstZExt = trySym(libLLVM, "LLVMConstZExt")
	symConstFPTrunc = trySym(libLLVM, "LLVMConstFPTrunc")
	symConstFPExt = trySym(libLLVM, "LLVMConstFPExt")
	symConstUIToFP = trySym(libLLVM, "LLVMConstUIToFP")
	symConstSIToFP = trySym(libLLVM, "LLVMConstSIToFP")
	symConstFPToUI = trySym(libLLVM, "LLVMConstFPToUI")
	symConstFPToSI = trySym(libLLVM, "LLVMConstFPToSI")
	symConstPtrToInt = mustSym(libLLVM, "LLVMConstPtrToInt")
	symConstIntToPtr = mustSym(libLLVM, "LLVMConstIntToPtr")
	symConstBitCast = mustSym(libLLVM, "LLVMConstBitCast")
	symConstICmp = trySym(libLLVM, "LLVMConstICmp")
	symConstFCmp = trySym(libLLVM, "LLVMConstFCmp")
	symConstExtractValue = trySym(libLLVM, "LLVMConstExtractValue")
	symConstInsertValue = trySym(libLLVM, "LLVMConstInsertValue")

	// Constant aggregates
	symConstStringInCtx = mustSym(libLLVM, "LLVMConstStringInContext")
	symConstStructInCtx = mustSym(libLLVM, "LLVMConstStructInContext")
	symConstArray = mustSym(libLLVM, "LLVMConstArray")
	symConstNamedStruct = mustSym(libLLVM, "LLVMConstNamedStruct")

	// Global values
	symGetLinkage = mustSym(libLLVM, "LLVMGetLinkage")
	symSetLinkage = mustSym(libLLVM, "LLVMSetLinkage")
	symGetSection = mustSym(libLLVM, "LLVMGetSection")
	symSetSection = mustSym(libLLVM, "LLVMSetSection")
	symGetVisibility = mustSym(libLLVM, "LLVMGetVisibility")
	symSetVisibility = mustSym(libLLVM, "LLVMSetVisibility")
	symGetAlignment = mustSym(libLLVM, "LLVMGetAlignment")
	symSetAlignment = mustSym(libLLVM, "LLVMSetAlignment")

	// Global variables
	symAddGlobal = mustSym(libLLVM, "LLVMAddGlobal")
	symGetNamedGlobal = mustSym(libLLVM, "LLVMGetNamedGlobal")
	symGetFirstGlobal = mustSym(libLLVM, "LLVMGetFirstGlobal")
	symGetNextGlobal = mustSym(libLLVM, "LLVMGetNextGlobal")
	symGetInitializer = mustSym(libLLVM, "LLVMGetInitializer")
	symSetInitializer = mustSym(libLLVM, "LLVMSetInitializer")
	symIsGlobalConstant = mustSym(libLLVM, "LLVMIsGlobalConstant")
	symSetGlobalConstant = mustSym(libLLVM, "LLVMSetGlobalConstant")
	symSetThreadLocal = mustSym(libLLVM, "LLVMSetThreadLocal")
	symIsThreadLocal = mustSym(libLLVM, "LLVMIsThreadLocal")
	symSetExternallyInit = mustSym(libLLVM, "LLVMSetExternallyInitialized")
	symIsExternallyInit = mustSym(libLLVM, "LLVMIsExternallyInitialized")
	symDeleteGlobal = mustSym(libLLVM, "LLVMDeleteGlobal")
	symSetUnnamedAddr = mustSym(libLLVM, "LLVMSetUnnamedAddress")
	symGetUnnamedAddr = mustSym(libLLVM, "LLVMGetUnnamedAddress")

	// Functions
	symAddFunction = mustSym(libLLVM, "LLVMAddFunction")
	symGetNamedFunction = mustSym(libLLVM, "LLVMGetNamedFunction")
	symGetFirstFunction = mustSym(libLLVM, "LLVMGetFirstFunction")
	symGetNextFunction = mustSym(libLLVM, "LLVMGetNextFunction")
	symDeleteFunction = mustSym(libLLVM, "LLVMDeleteFunction")
	symCountParams = mustSym(libLLVM, "LLVMCountParams")
	symGetParams = mustSym(libLLVM, "LLVMGetParams")
	symGetParam = mustSym(libLLVM, "LLVMGetParam")
	symSetFunctionCallConv = mustSym(libLLVM, "LLVMSetFunctionCallConv")
	symGetFunctionCallConv = mustSym(libLLVM, "LLVMGetFunctionCallConv")
	symAddFuncAttr = mustSym(libLLVM, "LLVMAddAttributeAtIndex")
	symGetEnumFuncAttr = mustSym(libLLVM, "LLVMGetEnumAttributeAtIndex")
	symRemoveEnumFuncAttr = mustSym(libLLVM, "LLVMRemoveEnumAttributeAtIndex")
	symAddTargetDepFuncAttr = mustSym(libLLVM, "LLVMAddTargetDependentFunctionAttr")

	// Basic blocks
	symAppendBasicBlockInCtx = mustSym(libLLVM, "LLVMAppendBasicBlockInContext")
	symInsertBasicBlockInCtx = mustSym(libLLVM, "LLVMInsertBasicBlockInContext")
	symGetBasicBlockParent = mustSym(libLLVM, "LLVMGetBasicBlockParent")
	symGetEntryBasicBlock = mustSym(libLLVM, "LLVMGetEntryBasicBlock")
	symGetFirstBasicBlock = mustSym(libLLVM, "LLVMGetFirstBasicBlock")
	symGetLastBasicBlock = mustSym(libLLVM, "LLVMGetLastBasicBlock")
	symGetNextBasicBlock = mustSym(libLLVM, "LLVMGetNextBasicBlock")
	symGetPreviousBasicBlock = mustSym(libLLVM, "LLVMGetPreviousBasicBlock")
	symCountBasicBlocks = mustSym(libLLVM, "LLVMCountBasicBlocks")
	symMoveBasicBlockAfter = mustSym(libLLVM, "LLVMMoveBasicBlockAfter")
	symMoveBasicBlockBefore = mustSym(libLLVM, "LLVMMoveBasicBlockBefore")
	symDeleteBasicBlock = mustSym(libLLVM, "LLVMDeleteBasicBlock")
	symBasicBlockAsValue = mustSym(libLLVM, "LLVMBasicBlockAsValue")

	// Instructions
	symGetFirstInstruction = mustSym(libLLVM, "LLVMGetFirstInstruction")
	symGetLastInstruction = mustSym(libLLVM, "LLVMGetLastInstruction")
	symGetNextInstruction = mustSym(libLLVM, "LLVMGetNextInstruction")
	symGetInstructionOpcode = mustSym(libLLVM, "LLVMGetInstructionOpcode")
	symInstructionEraseFromParent = mustSym(libLLVM, "LLVMInstructionEraseFromParent")
	symGetInstructionParent = mustSym(libLLVM, "LLVMGetInstructionParent")
	symIsATerminatorInst = mustSym(libLLVM, "LLVMIsATerminatorInst")

	// Builder
	symCreateBuilderInCtx = mustSym(libLLVM, "LLVMCreateBuilderInContext")
	symPositionBuilderAtEnd = mustSym(libLLVM, "LLVMPositionBuilderAtEnd")
	symPositionBuilderBefore = mustSym(libLLVM, "LLVMPositionBuilder")
	symGetInsertBlock = mustSym(libLLVM, "LLVMGetInsertBlock")
	symDisposeBuilder = mustSym(libLLVM, "LLVMDisposeBuilder")

	// Builder - terminators
	symBuildRetVoid = mustSym(libLLVM, "LLVMBuildRetVoid")
	symBuildRet = mustSym(libLLVM, "LLVMBuildRet")
	symBuildBr = mustSym(libLLVM, "LLVMBuildBr")
	symBuildCondBr = mustSym(libLLVM, "LLVMBuildCondBr")
	symBuildSwitch = mustSym(libLLVM, "LLVMBuildSwitch")
	symBuildUnreachable = mustSym(libLLVM, "LLVMBuildUnreachable")

	// Builder - arithmetic
	symBuildAdd = mustSym(libLLVM, "LLVMBuildAdd")
	symBuildNSWAdd = mustSym(libLLVM, "LLVMBuildNSWAdd")
	symBuildNUWAdd = mustSym(libLLVM, "LLVMBuildNUWAdd")
	symBuildFAdd = mustSym(libLLVM, "LLVMBuildFAdd")
	symBuildSub = mustSym(libLLVM, "LLVMBuildSub")
	symBuildNSWSub = mustSym(libLLVM, "LLVMBuildNSWSub")
	symBuildNUWSub = mustSym(libLLVM, "LLVMBuildNUWSub")
	symBuildFSub = mustSym(libLLVM, "LLVMBuildFSub")
	symBuildMul = mustSym(libLLVM, "LLVMBuildMul")
	symBuildNSWMul = mustSym(libLLVM, "LLVMBuildNSWMul")
	symBuildNUWMul = mustSym(libLLVM, "LLVMBuildNUWMul")
	symBuildFMul = mustSym(libLLVM, "LLVMBuildFMul")
	symBuildUDiv = mustSym(libLLVM, "LLVMBuildUDiv")
	symBuildSDiv = mustSym(libLLVM, "LLVMBuildSDiv")
	symBuildExactSDiv = mustSym(libLLVM, "LLVMBuildExactSDiv")
	symBuildFDiv = mustSym(libLLVM, "LLVMBuildFDiv")
	symBuildURem = mustSym(libLLVM, "LLVMBuildURem")
	symBuildSRem = mustSym(libLLVM, "LLVMBuildSRem")
	symBuildFRem = mustSym(libLLVM, "LLVMBuildFRem")
	symBuildShl = mustSym(libLLVM, "LLVMBuildShl")
	symBuildLShr = mustSym(libLLVM, "LLVMBuildLShr")
	symBuildAShr = mustSym(libLLVM, "LLVMBuildAShr")
	symBuildAnd = mustSym(libLLVM, "LLVMBuildAnd")
	symBuildOr = mustSym(libLLVM, "LLVMBuildOr")
	symBuildXor = mustSym(libLLVM, "LLVMBuildXor")
	symBuildNeg = mustSym(libLLVM, "LLVMBuildNeg")
	symBuildNSWNeg = mustSym(libLLVM, "LLVMBuildNSWNeg")
	symBuildNot = mustSym(libLLVM, "LLVMBuildNot")
	symBuildFNeg = mustSym(libLLVM, "LLVMBuildFNeg")

	// Builder - memory
	symBuildAlloca = mustSym(libLLVM, "LLVMBuildAlloca")
	symBuildLoad2 = mustSym(libLLVM, "LLVMBuildLoad2")
	symBuildStore = mustSym(libLLVM, "LLVMBuildStore")
	symBuildGEP2 = mustSym(libLLVM, "LLVMBuildGEP2")
	symBuildInBoundsGEP2 = mustSym(libLLVM, "LLVMBuildInBoundsGEP2")
	symBuildStructGEP2 = mustSym(libLLVM, "LLVMBuildStructGEP2")

	// Builder - casts
	symBuildTrunc = mustSym(libLLVM, "LLVMBuildTrunc")
	symBuildZExt = mustSym(libLLVM, "LLVMBuildZExt")
	symBuildSExt = mustSym(libLLVM, "LLVMBuildSExt")
	symBuildFPToUI = mustSym(libLLVM, "LLVMBuildFPToUI")
	symBuildFPToSI = mustSym(libLLVM, "LLVMBuildFPToSI")
	symBuildUIToFP = mustSym(libLLVM, "LLVMBuildUIToFP")
	symBuildSIToFP = mustSym(libLLVM, "LLVMBuildSIToFP")
	symBuildFPTrunc = mustSym(libLLVM, "LLVMBuildFPTrunc")
	symBuildFPExt = mustSym(libLLVM, "LLVMBuildFPExt")
	symBuildPtrToInt = mustSym(libLLVM, "LLVMBuildPtrToInt")
	symBuildIntToPtr = mustSym(libLLVM, "LLVMBuildIntToPtr")
	symBuildBitCast = mustSym(libLLVM, "LLVMBuildBitCast")
	symBuildPointerCast = mustSym(libLLVM, "LLVMBuildPointerCast")

	// Builder - comparisons
	symBuildICmp = mustSym(libLLVM, "LLVMBuildICmp")
	symBuildFCmp = mustSym(libLLVM, "LLVMBuildFCmp")

	// Builder - misc
	symBuildPhi = mustSym(libLLVM, "LLVMBuildPhi")
	symBuildCall2 = mustSym(libLLVM, "LLVMBuildCall2")
	symBuildSelect = mustSym(libLLVM, "LLVMBuildSelect")
	symBuildExtractValue = mustSym(libLLVM, "LLVMBuildExtractValue")
	symBuildInsertValue = mustSym(libLLVM, "LLVMBuildInsertValue")

	// PHI
	symAddIncoming = mustSym(libLLVM, "LLVMAddIncoming")

	// Call site
	symSetInstructionCallConv = mustSym(libLLVM, "LLVMSetInstructionCallConv")
	symGetInstructionCallConv = mustSym(libLLVM, "LLVMGetInstructionCallConv")
	symAddCallSiteAttribute = mustSym(libLLVM, "LLVMAddCallSiteAttribute")
	symSetInstrParamAlignment = mustSym(libLLVM, "LLVMSetInstrParamAlignment")

	// Switch
	symAddCase = mustSym(libLLVM, "LLVMAddCase")

	// Landing pad
	symBuildLandingPad = mustSym(libLLVM, "LLVMBuildLandingPad")
	symSetCleanup = mustSym(libLLVM, "LLVMSetCleanup")
	symAddClause = mustSym(libLLVM, "LLVMAddClause")

	// Invoke
	symBuildInvoke2 = mustSym(libLLVM, "LLVMBuildInvoke2")

	// Resume
	symBuildResume = mustSym(libLLVM, "LLVMBuildResume")

	// Atomics
	symBuildFence = mustSym(libLLVM, "LLVMBuildFence")
	symBuildAtomicRMW = mustSym(libLLVM, "LLVMBuildAtomicRMW")
	symBuildAtomicCmpXchg = mustSym(libLLVM, "LLVMBuildAtomicCmpXchg")
	symSetOrdering = mustSym(libLLVM, "LLVMSetOrdering")
	symSetAtomicSingleThread = mustSym(libLLVM, "LLVMSetAtomicSingleThread")

	// Module verify
	symVerifyModule = mustSym(libLLVM, "LLVMVerifyModule")
	symVerifyFunction = mustSym(libLLVM, "LLVMVerifyFunction")

	// Comdat
	symGetOrInsertComdat = mustSym(libLLVM, "LLVMGetOrInsertComdat")
	symComdatSetSelectionKind = mustSym(libLLVM, "LLVMSetComdatSelectionKind")
	symSetComdat = mustSym(libLLVM, "LLVMSetComdat")

	// Value type checks
	symIsAAllocaInst = mustSym(libLLVM, "LLVMIsAAllocaInst")
	symIsAConstant = mustSym(libLLVM, "LLVMIsAConstant")
	symIsAConstantInt = mustSym(libLLVM, "LLVMIsAConstantInt")
	symIsAFunction = mustSym(libLLVM, "LLVMIsAFunction")
	symIsAGlobalValue = mustSym(libLLVM, "LLVMIsAGlobalValue")
	symIsAInstruction = mustSym(libLLVM, "LLVMIsAInstruction")
	symIsAPHINode = mustSym(libLLVM, "LLVMIsAPHINode")
	symIsAUndefValue = mustSym(libLLVM, "LLVMIsAUndefValue")

	// Value misc
	symIsDeclaration = mustSym(libLLVM, "LLVMIsDeclaration")
	symGlobalGetValType = mustSym(libLLVM, "LLVMGlobalGetValueType")
	symGetLastParam = mustSym(libLLVM, "LLVMGetLastParam")
	symGetOperand = mustSym(libLLVM, "LLVMGetOperand")
	symGetNumOperands = mustSym(libLLVM, "LLVMGetNumOperands")
	symSetVolatile = mustSym(libLLVM, "LLVMSetVolatile")
	symInstructionRemoveFromParent = mustSym(libLLVM, "LLVMInstructionRemoveFromParent")
	symInstructionGetDebugLoc = mustSym(libLLVM, "LLVMInstructionGetDebugLoc")
	symCountIncoming = mustSym(libLLVM, "LLVMCountIncoming")
	symGetIncomingValue = mustSym(libLLVM, "LLVMGetIncomingValue")
	symGetIncomingBlock = mustSym(libLLVM, "LLVMGetIncomingBlock")

	// Builder misc
	symInsertIntoBuilder = mustSym(libLLVM, "LLVMInsertIntoBuilder")
	symBuildBinOp = mustSym(libLLVM, "LLVMBuildBinOp")

	// Const misc
	symConstPointerCast = trySym(libLLVM, "LLVMConstPointerCast")

	// Glue functions (from libmoxie-llvm-glue.so)
	if libGlue != 0 {
		symMDString2 = mustSym(libGlue, "LLVMMDString2")
		symMDNode2 = mustSym(libGlue, "LLVMMDNode2")
		symConstantAsMetadata = mustSym(libGlue, "LLVMConstantAsMetadata")
		symSetMetadata2 = mustSym(libGlue, "LLVMSetMetadata2")
		symAddNamedMetadataOperand2 = mustSym(libGlue, "LLVMAddNamedMetadataOperand2")
		symGlobalObjectAddMetadata = mustSym(libGlue, "LLVMGlobalObjectAddMetadata")
		symGoGetInlineAsm = mustSym(libGlue, "LLVMGoGetInlineAsm")
		symGoSetCurrentDebugLocation = mustSym(libGlue, "LLVMGoSetCurrentDebugLocation")
		symGoGetCurrentDebugLocation = mustSym(libGlue, "LLVMGoGetCurrentDebugLocation")
	}
}

// ---- Context ----

func NewContext() Context {
	r, _, _ := purego.SyscallN(symContextCreate)
	return Context{r}
}

func GlobalContext() Context {
	r, _, _ := purego.SyscallN(symGetGlobalCtx)
	return Context{r}
}

func (c Context) Dispose() {
	purego.SyscallN(symContextDispose, c.ref)
}

func (c Context) MDKindID(name string) int {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symGetMDKindIDCtx, c.ref, cname, uintptr(len(name)))
	runtime.KeepAlive(name)
	return int(r)
}

func MDKindID(name string) int {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symGetMDKindID, cname, uintptr(len(name)))
	runtime.KeepAlive(name)
	return int(r)
}

// ---- Attributes ----

func AttributeKindID(name string) uint {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symGetEnumAttrKindForName, cname, uintptr(len(name)))
	runtime.KeepAlive(name)
	return uint(r)
}

func (c Context) CreateEnumAttribute(kind uint, val uint64) Attribute {
	r, _, _ := purego.SyscallN(symCreateEnumAttr, c.ref, uintptr(kind), uintptr(val))
	return Attribute{r}
}

func (c Context) CreateTypeAttribute(kind uint, t Type) Attribute {
	r, _, _ := purego.SyscallN(symCreateTypeAttr, c.ref, uintptr(kind), t.ref)
	return Attribute{r}
}

func (c Context) CreateStringAttribute(kind string, val string) Attribute {
	ckind := cString(kind)
	cval := cString(val)
	r, _, _ := purego.SyscallN(symCreateStringAttr, c.ref, ckind, uintptr(len(kind)), cval, uintptr(len(val)))
	runtime.KeepAlive(kind)
	runtime.KeepAlive(val)
	return Attribute{r}
}

func (a Attribute) GetEnumKind() uint {
	r, _, _ := purego.SyscallN(symGetEnumAttrKind, a.ref)
	return uint(r)
}

func (a Attribute) GetEnumValue() uint64 {
	r, _, _ := purego.SyscallN(symGetEnumAttrValue, a.ref)
	return uint64(r)
}

func (a Attribute) GetStringKind() string {
	var length uintptr
	r, _, _ := purego.SyscallN(symGetStringAttrKind, a.ref, uintptr(unsafe.Pointer(&length)))
	return goStringN(r, int(length))
}

func (a Attribute) GetStringValue() string {
	var length uintptr
	r, _, _ := purego.SyscallN(symGetStringAttrValue, a.ref, uintptr(unsafe.Pointer(&length)))
	return goStringN(r, int(length))
}

func goStringN(ptr uintptr, n int) string {
	if ptr == 0 || n == 0 {
		return ""
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), n))
}

// ---- Module ----

func (c Context) NewModule(name string) Module {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symModuleCreateWithNameInCtx, cname, c.ref)
	runtime.KeepAlive(name)
	return Module{r}
}

func (m Module) Dispose() {
	purego.SyscallN(symDisposeModule, m.ref)
}

func (m Module) DataLayout() string {
	r, _, _ := purego.SyscallN(symGetDataLayout, m.ref)
	return goString(r)
}

func (m Module) SetDataLayout(layout string) {
	cl := cString(layout)
	purego.SyscallN(symSetDataLayout, m.ref, cl)
	runtime.KeepAlive(layout)
}

func (m Module) Target() string {
	r, _, _ := purego.SyscallN(symGetTarget, m.ref)
	return goString(r)
}

func (m Module) SetTarget(target string) {
	ct := cString(target)
	purego.SyscallN(symSetTarget, m.ref, ct)
	runtime.KeepAlive(target)
}

func (m Module) Context() Context {
	r, _, _ := purego.SyscallN(symGetModuleContext, m.ref)
	return Context{r}
}

func (m Module) GetTypeByName(name string) Type {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symGetTypeByName, m.ref, cname)
	runtime.KeepAlive(name)
	return Type{r}
}

func (m Module) String() string {
	r, _, _ := purego.SyscallN(symPrintModuleToString, m.ref)
	s := goString(r)
	purego.SyscallN(symDisposeMessage, r)
	return s
}

func (m Module) Dump() {
	purego.SyscallN(symDumpModule, m.ref)
}

func (m Module) SetInlineAsm(asm string) {
	casm := cString(asm)
	purego.SyscallN(symSetModuleInlineAsm, m.ref, casm)
	runtime.KeepAlive(asm)
}

func (m Module) AddNamedMetadataOperand(name string, operand Metadata) {
	if symAddNamedMetadataOperand2 == 0 {
		return
	}
	cname := cString(name)
	purego.SyscallN(symAddNamedMetadataOperand2, m.ref, cname, operand.ref)
	runtime.KeepAlive(name)
}

// ---- Type inspection ----

func (t Type) TypeKind() TypeKind {
	r, _, _ := purego.SyscallN(symGetTypeKind, t.ref)
	return TypeKind(r)
}

func (t Type) Context() Context {
	r, _, _ := purego.SyscallN(symGetTypeContext, t.ref)
	return Context{r}
}

// ---- Integer types ----

func (c Context) Int1Type() Type {
	r, _, _ := purego.SyscallN(symInt1TypeInCtx, c.ref)
	return Type{r}
}

func (c Context) Int8Type() Type {
	r, _, _ := purego.SyscallN(symInt8TypeInCtx, c.ref)
	return Type{r}
}

func (c Context) Int16Type() Type {
	r, _, _ := purego.SyscallN(symInt16TypeInCtx, c.ref)
	return Type{r}
}

func (c Context) Int32Type() Type {
	r, _, _ := purego.SyscallN(symInt32TypeInCtx, c.ref)
	return Type{r}
}

func (c Context) Int64Type() Type {
	r, _, _ := purego.SyscallN(symInt64TypeInCtx, c.ref)
	return Type{r}
}

func (c Context) IntType(numbits int) Type {
	r, _, _ := purego.SyscallN(symIntTypeInCtx, c.ref, uintptr(numbits))
	return Type{r}
}

func (t Type) IntTypeWidth() int {
	r, _, _ := purego.SyscallN(symGetIntTypeWidth, t.ref)
	return int(r)
}

// ---- Float types ----

func (c Context) FloatType() Type {
	r, _, _ := purego.SyscallN(symFloatTypeInCtx, c.ref)
	return Type{r}
}

func (c Context) DoubleType() Type {
	r, _, _ := purego.SyscallN(symDoubleTypeInCtx, c.ref)
	return Type{r}
}

func (c Context) X86FP80Type() Type {
	r, _, _ := purego.SyscallN(symX86FP80TypeInCtx, c.ref)
	return Type{r}
}

func (c Context) FP128Type() Type {
	r, _, _ := purego.SyscallN(symFP128TypeInCtx, c.ref)
	return Type{r}
}

func (c Context) PPCFP128Type() Type {
	r, _, _ := purego.SyscallN(symPPCFP128TypeInCtx, c.ref)
	return Type{r}
}

// ---- Function types ----

func FunctionType(returnType Type, paramTypes []Type, isVarArg bool) Type {
	r, _, _ := purego.SyscallN(symFunctionType, returnType.ref, typeRefPtr(paramTypes), uintptr(len(paramTypes)), boolToInt(isVarArg))
	return Type{r}
}

func (t Type) IsFunctionVarArg() bool {
	r, _, _ := purego.SyscallN(symIsFunctionVarArg, t.ref)
	return r != 0
}

func (t Type) ReturnType() Type {
	r, _, _ := purego.SyscallN(symGetReturnType, t.ref)
	return Type{r}
}

func (t Type) ParamTypesCount() int {
	r, _, _ := purego.SyscallN(symCountParamTypes, t.ref)
	return int(r)
}

func (t Type) ParamTypes() []Type {
	count := t.ParamTypesCount()
	if count == 0 {
		return nil
	}
	out := make([]Type, count)
	purego.SyscallN(symGetParamTypes, t.ref, uintptr(unsafe.Pointer(&out[0])))
	return out
}

// ---- Struct types ----

func (c Context) StructType(elementTypes []Type, packed bool) Type {
	r, _, _ := purego.SyscallN(symStructTypeInCtx, c.ref, typeRefPtr(elementTypes), uintptr(len(elementTypes)), boolToInt(packed))
	return Type{r}
}

func (c Context) StructCreateNamed(name string) Type {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symStructCreateNamed, c.ref, cname)
	runtime.KeepAlive(name)
	return Type{r}
}

func (t Type) StructName() string {
	r, _, _ := purego.SyscallN(symGetStructName, t.ref)
	return goString(r)
}

func (t Type) StructSetBody(elementTypes []Type, packed bool) {
	purego.SyscallN(symStructSetBody, t.ref, typeRefPtr(elementTypes), uintptr(len(elementTypes)), boolToInt(packed))
}

func (t Type) IsStructPacked() bool {
	r, _, _ := purego.SyscallN(symIsPackedStruct, t.ref)
	return r != 0
}

func (t Type) StructElementTypesCount() int {
	r, _, _ := purego.SyscallN(symCountStructElementTypes, t.ref)
	return int(r)
}

func (t Type) StructElementTypes() []Type {
	count := t.StructElementTypesCount()
	if count == 0 {
		return nil
	}
	out := make([]Type, count)
	purego.SyscallN(symGetStructElementTypes, t.ref, uintptr(unsafe.Pointer(&out[0])))
	return out
}

// ---- Sequence types ----

func ArrayType(elementType Type, elementCount int) Type {
	r, _, _ := purego.SyscallN(symArrayType, elementType.ref, uintptr(elementCount))
	return Type{r}
}

func PointerType(elementType Type, addressSpace int) Type {
	r, _, _ := purego.SyscallN(symPointerType, elementType.ref, uintptr(addressSpace))
	return Type{r}
}

func VectorType(elementType Type, elementCount int) Type {
	r, _, _ := purego.SyscallN(symVectorType, elementType.ref, uintptr(elementCount))
	return Type{r}
}

func (t Type) ElementType() Type {
	r, _, _ := purego.SyscallN(symGetElementType, t.ref)
	return Type{r}
}

func (t Type) ArrayLength() int {
	r, _, _ := purego.SyscallN(symGetArrayLength, t.ref)
	return int(r)
}

func (t Type) PointerAddressSpace() int {
	r, _, _ := purego.SyscallN(symGetPointerAddressSpace, t.ref)
	return int(r)
}

func (t Type) VectorSize() int {
	r, _, _ := purego.SyscallN(symGetVectorSize, t.ref)
	return int(r)
}

func (t Type) Subtypes() []Type {
	n, _, _ := purego.SyscallN(symGetNumContainedTypes, t.ref)
	if n == 0 {
		return nil
	}
	out := make([]Type, int(n))
	purego.SyscallN(symGetSubtypes, t.ref, uintptr(unsafe.Pointer(&out[0])))
	return out
}

// ---- Other types ----

func (c Context) VoidType() Type {
	r, _, _ := purego.SyscallN(symVoidTypeInCtx, c.ref)
	return Type{r}
}

func (c Context) LabelType() Type {
	r, _, _ := purego.SyscallN(symLabelTypeInCtx, c.ref)
	return Type{r}
}

func (c Context) TokenType() Type {
	r, _, _ := purego.SyscallN(symTokenTypeInCtx, c.ref)
	return Type{r}
}

// ---- Value operations ----

func (v Value) Type() Type {
	r, _, _ := purego.SyscallN(symTypeOf, v.ref)
	return Type{r}
}

func (v Value) Name() string {
	r, _, _ := purego.SyscallN(symGetValueName, v.ref)
	return goString(r)
}

func (v Value) SetName(name string) {
	cname := cString(name)
	purego.SyscallN(symSetValueName, v.ref, cname)
	runtime.KeepAlive(name)
}

func (v Value) Dump() {
	purego.SyscallN(symDumpValue, v.ref)
}

func (v Value) ReplaceAllUsesWith(nv Value) {
	purego.SyscallN(symReplaceAllUsesWith, v.ref, nv.ref)
}

func (v Value) HasMetadata() bool {
	r, _, _ := purego.SyscallN(symHasMetadata, v.ref)
	return r != 0
}

func (v Value) InstructionDebugLoc() Metadata {
	r, _, _ := purego.SyscallN(symInstructionGetDebugLoc, v.ref)
	return Metadata{r}
}

func (v Value) SetMetadata(kind int, node Metadata) {
	if symSetMetadata2 == 0 {
		return
	}
	purego.SyscallN(symSetMetadata2, v.ref, uintptr(kind), node.ref)
}

func (v Value) String() string {
	r, _, _ := purego.SyscallN(symPrintValueToString, v.ref)
	s := goString(r)
	purego.SyscallN(symDisposeMessage, r)
	return s
}

// ---- Constant scalars ----

func ConstInt(t Type, n uint64, signExtend bool) Value {
	r, _, _ := purego.SyscallN(symConstInt, t.ref, uintptr(n), boolToInt(signExtend))
	return Value{r}
}

func ConstFloat(t Type, n float64) Value {
	r, _, _ := purego.SyscallN(symConstFloat, t.ref, *(*uintptr)(unsafe.Pointer(&n)))
	return Value{r}
}

func ConstNull(t Type) Value {
	r, _, _ := purego.SyscallN(symConstNull, t.ref)
	return Value{r}
}

func ConstAllOnes(t Type) Value {
	r, _, _ := purego.SyscallN(symConstAllOnes, t.ref)
	return Value{r}
}

func Undef(t Type) Value {
	r, _, _ := purego.SyscallN(symGetUndef, t.ref)
	return Value{r}
}

func ConstPointerNull(t Type) Value {
	r, _, _ := purego.SyscallN(symConstPointerNull, t.ref)
	return Value{r}
}

func (v Value) IsConstant() bool {
	r, _, _ := purego.SyscallN(symIsConstant, v.ref)
	return r != 0
}

func (v Value) IsNull() bool {
	r, _, _ := purego.SyscallN(symIsNull, v.ref)
	return r != 0
}

func (v Value) IsUndef() bool {
	r, _, _ := purego.SyscallN(symIsUndef, v.ref)
	return r != 0
}

func (v Value) ZExtValue() uint64 {
	r, _, _ := purego.SyscallN(symConstIntGetZExt, v.ref)
	return uint64(r)
}

func (v Value) SExtValue() int64 {
	r, _, _ := purego.SyscallN(symConstIntGetSExt, v.ref)
	return int64(r)
}

func (v Value) DoubleValue() (float64, bool) {
	var lossy uintptr
	r, _, _ := purego.SyscallN(symConstFloatGetDouble, v.ref, uintptr(unsafe.Pointer(&lossy)))
	return *(*float64)(unsafe.Pointer(&r)), lossy != 0
}

// ---- Constant expressions ----

func (v Value) ConstNeg() Value {
	r, _, _ := purego.SyscallN(symConstNeg, v.ref)
	return Value{r}
}

func (v Value) ConstNSWNeg() Value {
	r, _, _ := purego.SyscallN(symConstNSWNeg, v.ref)
	return Value{r}
}

func ConstNot(v Value) Value {
	r, _, _ := purego.SyscallN(symConstNot, v.ref)
	return Value{r}
}

func ConstAdd(lhs, rhs Value) Value {
	r, _, _ := purego.SyscallN(symConstAdd, lhs.ref, rhs.ref)
	return Value{r}
}

func ConstNSWAdd(lhs, rhs Value) Value {
	r, _, _ := purego.SyscallN(symConstNSWAdd, lhs.ref, rhs.ref)
	return Value{r}
}

func ConstSub(lhs, rhs Value) Value {
	r, _, _ := purego.SyscallN(symConstSub, lhs.ref, rhs.ref)
	return Value{r}
}

func ConstNSWSub(lhs, rhs Value) Value {
	r, _, _ := purego.SyscallN(symConstNSWSub, lhs.ref, rhs.ref)
	return Value{r}
}

func ConstMul(lhs, rhs Value) Value {
	r, _, _ := purego.SyscallN(symConstMul, lhs.ref, rhs.ref)
	return Value{r}
}

func ConstNSWMul(lhs, rhs Value) Value {
	r, _, _ := purego.SyscallN(symConstNSWMul, lhs.ref, rhs.ref)
	return Value{r}
}

func ConstXor(lhs, rhs Value) Value {
	r, _, _ := purego.SyscallN(symConstXor, lhs.ref, rhs.ref)
	return Value{r}
}

func ConstGEP(pointeeType Type, val Value, indices []Value) Value {
	r, _, _ := purego.SyscallN(symConstGEP2, pointeeType.ref, val.ref, valueRefPtr(indices), uintptr(len(indices)))
	return Value{r}
}

func ConstInBoundsGEP(pointeeType Type, val Value, indices []Value) Value {
	r, _, _ := purego.SyscallN(symConstInBoundsGEP2, pointeeType.ref, val.ref, valueRefPtr(indices), uintptr(len(indices)))
	return Value{r}
}

func ConstTrunc(val Value, t Type) Value {
	r, _, _ := purego.SyscallN(symConstTrunc, val.ref, t.ref)
	return Value{r}
}

func ConstSExt(val Value, t Type) Value {
	r, _, _ := purego.SyscallN(symConstSExt, val.ref, t.ref)
	return Value{r}
}

func ConstZExt(val Value, t Type) Value {
	r, _, _ := purego.SyscallN(symConstZExt, val.ref, t.ref)
	return Value{r}
}

func ConstFPTrunc(val Value, t Type) Value {
	r, _, _ := purego.SyscallN(symConstFPTrunc, val.ref, t.ref)
	return Value{r}
}

func ConstFPExt(val Value, t Type) Value {
	r, _, _ := purego.SyscallN(symConstFPExt, val.ref, t.ref)
	return Value{r}
}

func ConstUIToFP(val Value, t Type) Value {
	r, _, _ := purego.SyscallN(symConstUIToFP, val.ref, t.ref)
	return Value{r}
}

func ConstSIToFP(val Value, t Type) Value {
	r, _, _ := purego.SyscallN(symConstSIToFP, val.ref, t.ref)
	return Value{r}
}

func ConstFPToUI(val Value, t Type) Value {
	r, _, _ := purego.SyscallN(symConstFPToUI, val.ref, t.ref)
	return Value{r}
}

func ConstFPToSI(val Value, t Type) Value {
	r, _, _ := purego.SyscallN(symConstFPToSI, val.ref, t.ref)
	return Value{r}
}

func ConstPtrToInt(val Value, t Type) Value {
	r, _, _ := purego.SyscallN(symConstPtrToInt, val.ref, t.ref)
	return Value{r}
}

func ConstIntToPtr(val Value, t Type) Value {
	r, _, _ := purego.SyscallN(symConstIntToPtr, val.ref, t.ref)
	return Value{r}
}

func ConstBitCast(val Value, t Type) Value {
	r, _, _ := purego.SyscallN(symConstBitCast, val.ref, t.ref)
	return Value{r}
}

func ConstPointerCast(val Value, t Type) Value {
	if symConstPointerCast == 0 {
		return ConstBitCast(val, t)
	}
	r, _, _ := purego.SyscallN(symConstPointerCast, val.ref, t.ref)
	return Value{r}
}

func ConstICmp(pred IntPredicate, lhs, rhs Value) Value {
	r, _, _ := purego.SyscallN(symConstICmp, uintptr(pred), lhs.ref, rhs.ref)
	return Value{r}
}

func ConstFCmp(pred FloatPredicate, lhs, rhs Value) Value {
	r, _, _ := purego.SyscallN(symConstFCmp, uintptr(pred), lhs.ref, rhs.ref)
	return Value{r}
}

func ConstExtractValue(agg Value, indices []uint32) Value {
	r, _, _ := purego.SyscallN(symConstExtractValue, agg.ref, uintptr(unsafe.Pointer(&indices[0])), uintptr(len(indices)))
	return Value{r}
}

func ConstInsertValue(agg, val Value, indices []uint32) Value {
	r, _, _ := purego.SyscallN(symConstInsertValue, agg.ref, val.ref, uintptr(unsafe.Pointer(&indices[0])), uintptr(len(indices)))
	return Value{r}
}

// ---- Constant aggregates ----

func (c Context) ConstString(str string, addnull bool) Value {
	cstr := cString(str)
	r, _, _ := purego.SyscallN(symConstStringInCtx, c.ref, cstr, uintptr(len(str)), boolToInt(!addnull))
	runtime.KeepAlive(str)
	return Value{r}
}

func (c Context) ConstStruct(constVals []Value, packed bool) Value {
	r, _, _ := purego.SyscallN(symConstStructInCtx, c.ref, valueRefPtr(constVals), uintptr(len(constVals)), boolToInt(packed))
	return Value{r}
}

func ConstArray(t Type, constVals []Value) Value {
	r, _, _ := purego.SyscallN(symConstArray, t.ref, valueRefPtr(constVals), uintptr(len(constVals)))
	return Value{r}
}

func ConstNamedStruct(t Type, constVals []Value) Value {
	r, _, _ := purego.SyscallN(symConstNamedStruct, t.ref, valueRefPtr(constVals), uintptr(len(constVals)))
	return Value{r}
}

// ---- Global values ----

func (v Value) Linkage() Linkage {
	r, _, _ := purego.SyscallN(symGetLinkage, v.ref)
	return Linkage(r)
}

func (v Value) SetLinkage(l Linkage) {
	purego.SyscallN(symSetLinkage, v.ref, uintptr(l))
}

func (v Value) Section() string {
	r, _, _ := purego.SyscallN(symGetSection, v.ref)
	return goString(r)
}

func (v Value) SetSection(s string) {
	cs := cString(s)
	purego.SyscallN(symSetSection, v.ref, cs)
	runtime.KeepAlive(s)
}

func (v Value) Visibility() Visibility {
	r, _, _ := purego.SyscallN(symGetVisibility, v.ref)
	return Visibility(r)
}

func (v Value) SetVisibility(vis Visibility) {
	purego.SyscallN(symSetVisibility, v.ref, uintptr(vis))
}

func (v Value) Alignment() int {
	r, _, _ := purego.SyscallN(symGetAlignment, v.ref)
	return int(r)
}

func (v Value) SetAlignment(a int) {
	purego.SyscallN(symSetAlignment, v.ref, uintptr(a))
}

// ---- Global variables ----

func (m Module) AddGlobal(t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symAddGlobal, m.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (m Module) NamedGlobal(name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symGetNamedGlobal, m.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (m Module) FirstGlobal() Value {
	r, _, _ := purego.SyscallN(symGetFirstGlobal, m.ref)
	return Value{r}
}

func (v Value) NextGlobal() Value {
	r, _, _ := purego.SyscallN(symGetNextGlobal, v.ref)
	return Value{r}
}

func (v Value) Initializer() Value {
	r, _, _ := purego.SyscallN(symGetInitializer, v.ref)
	return Value{r}
}

func (v Value) SetInitializer(c Value) {
	purego.SyscallN(symSetInitializer, v.ref, c.ref)
}

func (v Value) IsGlobalConstant() bool {
	r, _, _ := purego.SyscallN(symIsGlobalConstant, v.ref)
	return r != 0
}

func (v Value) SetGlobalConstant(b bool) {
	purego.SyscallN(symSetGlobalConstant, v.ref, boolToInt(b))
}

func (v Value) SetThreadLocal(b bool) {
	purego.SyscallN(symSetThreadLocal, v.ref, boolToInt(b))
}

func (v Value) IsThreadLocal() bool {
	r, _, _ := purego.SyscallN(symIsThreadLocal, v.ref)
	return r != 0
}

func (v Value) SetExternallyInitialized(b bool) {
	purego.SyscallN(symSetExternallyInit, v.ref, boolToInt(b))
}

func (v Value) IsExternallyInitialized() bool {
	r, _, _ := purego.SyscallN(symIsExternallyInit, v.ref)
	return r != 0
}

func (v Value) EraseFromParentAsGlobal() {
	purego.SyscallN(symDeleteGlobal, v.ref)
}

func (v Value) SetUnnamedAddr(b bool) {
	val := uintptr(0)
	if b {
		val = 1 // LLVMGlobalUnnamedAddr
	}
	purego.SyscallN(symSetUnnamedAddr, v.ref, val)
}

func (v Value) UnnamedAddr() bool {
	r, _, _ := purego.SyscallN(symGetUnnamedAddr, v.ref)
	return r != 0
}

// ---- Functions ----

func (m Module) AddFunction(name string, t Type) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symAddFunction, m.ref, cname, t.ref)
	runtime.KeepAlive(name)
	return Value{r}
}

func AddFunction(m Module, name string, ft Type) Value {
	return m.AddFunction(name, ft)
}

func AddGlobal(m Module, t Type, name string) Value {
	return m.AddGlobal(t, name)
}

func (m Module) NamedFunction(name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symGetNamedFunction, m.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (m Module) FirstFunction() Value {
	r, _, _ := purego.SyscallN(symGetFirstFunction, m.ref)
	return Value{r}
}

func (v Value) NextFunction() Value {
	r, _, _ := purego.SyscallN(symGetNextFunction, v.ref)
	return Value{r}
}

func (v Value) EraseFromParentAsFunction() {
	purego.SyscallN(symDeleteFunction, v.ref)
}

func (v Value) ParamsCount() int {
	r, _, _ := purego.SyscallN(symCountParams, v.ref)
	return int(r)
}

func (v Value) Params() []Value {
	count := v.ParamsCount()
	if count == 0 {
		return nil
	}
	out := make([]Value, count)
	purego.SyscallN(symGetParams, v.ref, uintptr(unsafe.Pointer(&out[0])))
	return out
}

func (v Value) Param(i int) Value {
	r, _, _ := purego.SyscallN(symGetParam, v.ref, uintptr(i))
	return Value{r}
}

func (v Value) SetFunctionCallConv(cc CallConv) {
	purego.SyscallN(symSetFunctionCallConv, v.ref, uintptr(cc))
}

func (v Value) FunctionCallConv() CallConv {
	r, _, _ := purego.SyscallN(symGetFunctionCallConv, v.ref)
	return CallConv(r)
}

func (v Value) AddFunctionAttr(attr Attribute) {
	// LLVMAttributeFunctionIndex = -1, but passed as unsigned = 0xFFFFFFFF
	purego.SyscallN(symAddFuncAttr, v.ref, ^uintptr(0), attr.ref)
}

func (v Value) GetEnumFunctionAttribute(index int, kindID uint) Attribute {
	r, _, _ := purego.SyscallN(symGetEnumFuncAttr, v.ref, uintptr(index), uintptr(kindID))
	return Attribute{r}
}

func (v Value) RemoveEnumFunctionAttribute(index int, kindID uint) {
	purego.SyscallN(symRemoveEnumFuncAttr, v.ref, uintptr(index), uintptr(kindID))
}

func (v Value) AddTargetDependentFunctionAttr(attr, val string) {
	cattr := cString(attr)
	cval := cString(val)
	purego.SyscallN(symAddTargetDepFuncAttr, v.ref, cattr, cval)
	runtime.KeepAlive(attr)
	runtime.KeepAlive(val)
}

// ---- Basic blocks ----

func (c Context) AddBasicBlock(f Value, name string) BasicBlock {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symAppendBasicBlockInCtx, c.ref, f.ref, cname)
	runtime.KeepAlive(name)
	return BasicBlock{r}
}

func AddBasicBlock(f Value, name string) BasicBlock {
	return GlobalContext().AddBasicBlock(f, name)
}

func (c Context) InsertBasicBlock(ref BasicBlock, name string) BasicBlock {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symInsertBasicBlockInCtx, c.ref, ref.ref, cname)
	runtime.KeepAlive(name)
	return BasicBlock{r}
}

func (bb BasicBlock) Parent() Value {
	r, _, _ := purego.SyscallN(symGetBasicBlockParent, bb.ref)
	return Value{r}
}

func (v Value) EntryBasicBlock() BasicBlock {
	r, _, _ := purego.SyscallN(symGetEntryBasicBlock, v.ref)
	return BasicBlock{r}
}

func (v Value) FirstBasicBlock() BasicBlock {
	r, _, _ := purego.SyscallN(symGetFirstBasicBlock, v.ref)
	return BasicBlock{r}
}

func (v Value) LastBasicBlock() BasicBlock {
	r, _, _ := purego.SyscallN(symGetLastBasicBlock, v.ref)
	return BasicBlock{r}
}

func (bb BasicBlock) NextBasicBlock() BasicBlock {
	r, _, _ := purego.SyscallN(symGetNextBasicBlock, bb.ref)
	return BasicBlock{r}
}

func (bb BasicBlock) PrevBasicBlock() BasicBlock {
	r, _, _ := purego.SyscallN(symGetPreviousBasicBlock, bb.ref)
	return BasicBlock{r}
}

func (v Value) BasicBlocksCount() int {
	r, _, _ := purego.SyscallN(symCountBasicBlocks, v.ref)
	return int(r)
}

func (bb BasicBlock) MoveAfter(other BasicBlock) {
	purego.SyscallN(symMoveBasicBlockAfter, bb.ref, other.ref)
}

func (bb BasicBlock) MoveBefore(other BasicBlock) {
	purego.SyscallN(symMoveBasicBlockBefore, bb.ref, other.ref)
}

func (bb BasicBlock) EraseFromParent() {
	purego.SyscallN(symDeleteBasicBlock, bb.ref)
}

func (bb BasicBlock) AsValue() Value {
	r, _, _ := purego.SyscallN(symBasicBlockAsValue, bb.ref)
	return Value{r}
}

// ---- Instructions ----

func (bb BasicBlock) FirstInstruction() Value {
	r, _, _ := purego.SyscallN(symGetFirstInstruction, bb.ref)
	return Value{r}
}

func (bb BasicBlock) LastInstruction() Value {
	r, _, _ := purego.SyscallN(symGetLastInstruction, bb.ref)
	return Value{r}
}

func (v Value) NextInstruction() Value {
	r, _, _ := purego.SyscallN(symGetNextInstruction, v.ref)
	return Value{r}
}

func (v Value) InstructionOpcode() Opcode {
	r, _, _ := purego.SyscallN(symGetInstructionOpcode, v.ref)
	return Opcode(r)
}

func (v Value) EraseFromParentAsInstruction() {
	purego.SyscallN(symInstructionEraseFromParent, v.ref)
}

func (v Value) InstructionParent() BasicBlock {
	r, _, _ := purego.SyscallN(symGetInstructionParent, v.ref)
	return BasicBlock{r}
}

func (v Value) IsATerminatorInst() Value {
	r, _, _ := purego.SyscallN(symIsATerminatorInst, v.ref)
	return Value{r}
}

func (v Value) IsAAllocaInst() Value {
	r, _, _ := purego.SyscallN(symIsAAllocaInst, v.ref)
	return Value{r}
}

func (v Value) IsAConstant() Value {
	r, _, _ := purego.SyscallN(symIsAConstant, v.ref)
	return Value{r}
}

func (v Value) IsAConstantInt() Value {
	r, _, _ := purego.SyscallN(symIsAConstantInt, v.ref)
	return Value{r}
}

func (v Value) IsAFunction() Value {
	r, _, _ := purego.SyscallN(symIsAFunction, v.ref)
	return Value{r}
}

func (v Value) IsAGlobalValue() Value {
	r, _, _ := purego.SyscallN(symIsAGlobalValue, v.ref)
	return Value{r}
}

func (v Value) IsAInstruction() Value {
	r, _, _ := purego.SyscallN(symIsAInstruction, v.ref)
	return Value{r}
}

func (v Value) IsAPHINode() Value {
	r, _, _ := purego.SyscallN(symIsAPHINode, v.ref)
	return Value{r}
}

func (v Value) IsAUndefValue() Value {
	r, _, _ := purego.SyscallN(symIsAUndefValue, v.ref)
	return Value{r}
}

func (v Value) IsDeclaration() bool {
	r, _, _ := purego.SyscallN(symIsDeclaration, v.ref)
	return r != 0
}

func (v Value) GlobalValueType() Type {
	r, _, _ := purego.SyscallN(symGlobalGetValType, v.ref)
	return Type{r}
}

func (v Value) LastParam() Value {
	r, _, _ := purego.SyscallN(symGetLastParam, v.ref)
	return Value{r}
}

func (v Value) Operand(i int) Value {
	r, _, _ := purego.SyscallN(symGetOperand, v.ref, uintptr(i))
	return Value{r}
}

func (v Value) OperandsCount() int {
	r, _, _ := purego.SyscallN(symGetNumOperands, v.ref)
	return int(r)
}

func (v Value) SetVolatile(volatile bool) {
	purego.SyscallN(symSetVolatile, v.ref, boolToInt(volatile))
}

func (v Value) RemoveFromParentAsInstruction() {
	purego.SyscallN(symInstructionRemoveFromParent, v.ref)
}

func (v Value) IncomingCount() int {
	r, _, _ := purego.SyscallN(symCountIncoming, v.ref)
	return int(r)
}

func (v Value) IncomingValue(i int) Value {
	r, _, _ := purego.SyscallN(symGetIncomingValue, v.ref, uintptr(i))
	return Value{r}
}

func (v Value) IncomingBlock(i int) BasicBlock {
	r, _, _ := purego.SyscallN(symGetIncomingBlock, v.ref, uintptr(i))
	return BasicBlock{r}
}

func (v Value) AddAttributeAtIndex(i int, a Attribute) {
	purego.SyscallN(symAddFuncAttr, v.ref, uintptr(i), a.ref)
}

func (v Value) AddMetadata(kind int, md Metadata) {
	if symGlobalObjectAddMetadata == 0 {
		return
	}
	purego.SyscallN(symGlobalObjectAddMetadata, v.ref, uintptr(kind), md.ref)
}

// Free-function wrappers for iteration (compiler uses these)

func NextInstruction(v Value) Value         { return v.NextInstruction() }
func NextBasicBlock(bb BasicBlock) BasicBlock { return bb.NextBasicBlock() }
func NextFunction(v Value) Value            { return v.NextFunction() }
func NextGlobal(v Value) Value              { return v.NextGlobal() }

// ---- Builder ----

func (c Context) NewBuilder() Builder {
	r, _, _ := purego.SyscallN(symCreateBuilderInCtx, c.ref)
	return Builder{r}
}

func (b Builder) SetInsertPointAtEnd(bb BasicBlock) {
	purego.SyscallN(symPositionBuilderAtEnd, b.ref, bb.ref)
}

func (b Builder) SetInsertPointBefore(instr Value) {
	parent := instr.InstructionParent()
	purego.SyscallN(symPositionBuilderBefore, b.ref, parent.ref, instr.ref)
}

func (b Builder) GetInsertBlock() BasicBlock {
	r, _, _ := purego.SyscallN(symGetInsertBlock, b.ref)
	return BasicBlock{r}
}

func (b Builder) Dispose() {
	purego.SyscallN(symDisposeBuilder, b.ref)
}

func (b Builder) Insert(instr Value) {
	purego.SyscallN(symInsertIntoBuilder, b.ref, instr.ref)
}

func (b Builder) CreateBinOp(op Opcode, lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildBinOp, b.ref, uintptr(op), lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

// Builder - terminators

func (b Builder) CreateRetVoid() Value {
	r, _, _ := purego.SyscallN(symBuildRetVoid, b.ref)
	return Value{r}
}

func (b Builder) CreateRet(v Value) Value {
	r, _, _ := purego.SyscallN(symBuildRet, b.ref, v.ref)
	return Value{r}
}

func (b Builder) CreateBr(dest BasicBlock) Value {
	r, _, _ := purego.SyscallN(symBuildBr, b.ref, dest.ref)
	return Value{r}
}

func (b Builder) CreateCondBr(cond Value, thenBB, elseBB BasicBlock) Value {
	r, _, _ := purego.SyscallN(symBuildCondBr, b.ref, cond.ref, thenBB.ref, elseBB.ref)
	return Value{r}
}

func (b Builder) CreateSwitch(v Value, elseBB BasicBlock, numCases int) Value {
	r, _, _ := purego.SyscallN(symBuildSwitch, b.ref, v.ref, elseBB.ref, uintptr(numCases))
	return Value{r}
}

func (b Builder) CreateUnreachable() Value {
	r, _, _ := purego.SyscallN(symBuildUnreachable, b.ref)
	return Value{r}
}

// Builder - arithmetic

func (b Builder) CreateAdd(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildAdd, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateNSWAdd(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildNSWAdd, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateNUWAdd(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildNUWAdd, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateFAdd(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildFAdd, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateSub(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildSub, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateNSWSub(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildNSWSub, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateNUWSub(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildNUWSub, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateFSub(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildFSub, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateMul(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildMul, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateNSWMul(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildNSWMul, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateNUWMul(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildNUWMul, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateFMul(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildFMul, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateUDiv(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildUDiv, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateSDiv(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildSDiv, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateExactSDiv(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildExactSDiv, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateFDiv(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildFDiv, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateURem(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildURem, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateSRem(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildSRem, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateFRem(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildFRem, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateShl(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildShl, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateLShr(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildLShr, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateAShr(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildAShr, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateAnd(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildAnd, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateOr(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildOr, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateXor(lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildXor, b.ref, lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateNeg(v Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildNeg, b.ref, v.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateNSWNeg(v Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildNSWNeg, b.ref, v.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateNot(v Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildNot, b.ref, v.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateFNeg(v Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildFNeg, b.ref, v.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

// Builder - memory

func (b Builder) CreateAlloca(t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildAlloca, b.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateLoad(t Type, ptr Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildLoad2, b.ref, t.ref, ptr.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateStore(val, ptr Value) Value {
	r, _, _ := purego.SyscallN(symBuildStore, b.ref, val.ref, ptr.ref)
	return Value{r}
}

func (b Builder) CreateGEP(t Type, ptr Value, indices []Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildGEP2, b.ref, t.ref, ptr.ref, valueRefPtr(indices), uintptr(len(indices)), cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateInBoundsGEP(t Type, ptr Value, indices []Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildInBoundsGEP2, b.ref, t.ref, ptr.ref, valueRefPtr(indices), uintptr(len(indices)), cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateStructGEP(t Type, ptr Value, idx int, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildStructGEP2, b.ref, t.ref, ptr.ref, uintptr(idx), cname)
	runtime.KeepAlive(name)
	return Value{r}
}

// Builder - casts

func (b Builder) CreateTrunc(val Value, t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildTrunc, b.ref, val.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateZExt(val Value, t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildZExt, b.ref, val.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateSExt(val Value, t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildSExt, b.ref, val.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateFPToUI(val Value, t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildFPToUI, b.ref, val.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateFPToSI(val Value, t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildFPToSI, b.ref, val.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateUIToFP(val Value, t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildUIToFP, b.ref, val.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateSIToFP(val Value, t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildSIToFP, b.ref, val.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateFPTrunc(val Value, t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildFPTrunc, b.ref, val.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateFPExt(val Value, t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildFPExt, b.ref, val.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreatePtrToInt(val Value, t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildPtrToInt, b.ref, val.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateIntToPtr(val Value, t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildIntToPtr, b.ref, val.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateBitCast(val Value, t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildBitCast, b.ref, val.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreatePointerCast(val Value, t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildPointerCast, b.ref, val.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

// Builder - comparisons

func (b Builder) CreateICmp(pred IntPredicate, lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildICmp, b.ref, uintptr(pred), lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateFCmp(pred FloatPredicate, lhs, rhs Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildFCmp, b.ref, uintptr(pred), lhs.ref, rhs.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

// Builder - misc

func (b Builder) CreatePHI(t Type, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildPhi, b.ref, t.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateCall(fnType Type, fn Value, args []Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildCall2, b.ref, fnType.ref, fn.ref, valueRefPtr(args), uintptr(len(args)), cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateSelect(cond, thenVal, elseVal Value, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildSelect, b.ref, cond.ref, thenVal.ref, elseVal.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateExtractValue(agg Value, index int, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildExtractValue, b.ref, agg.ref, uintptr(index), cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateInsertValue(agg, val Value, index int, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildInsertValue, b.ref, agg.ref, val.ref, uintptr(index), cname)
	runtime.KeepAlive(name)
	return Value{r}
}

// ---- PHI ----

func (v Value) AddIncoming(vals []Value, blocks []BasicBlock) {
	if len(vals) == 0 {
		return
	}
	purego.SyscallN(symAddIncoming, v.ref, valueRefPtr(vals), uintptr(unsafe.Pointer(&blocks[0])), uintptr(len(vals)))
}

// ---- Call site ----

func (v Value) SetInstructionCallConv(cc CallConv) {
	purego.SyscallN(symSetInstructionCallConv, v.ref, uintptr(cc))
}

func (v Value) InstructionCallConv() CallConv {
	r, _, _ := purego.SyscallN(symGetInstructionCallConv, v.ref)
	return CallConv(r)
}

func (v Value) AddCallSiteAttribute(index int, attr Attribute) {
	purego.SyscallN(symAddCallSiteAttribute, v.ref, uintptr(index), attr.ref)
}

func (v Value) SetInstrParamAlignment(index, align int) {
	purego.SyscallN(symSetInstrParamAlignment, v.ref, uintptr(index), uintptr(align))
}

// ---- Switch ----

func (v Value) AddCase(onVal Value, dest BasicBlock) {
	purego.SyscallN(symAddCase, v.ref, onVal.ref, dest.ref)
}

// ---- Landing pad ----

func (b Builder) CreateLandingPad(t Type, numClauses int, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildLandingPad, b.ref, t.ref, 0, uintptr(numClauses), cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (v Value) SetCleanup(b bool) {
	purego.SyscallN(symSetCleanup, v.ref, boolToInt(b))
}

func (v Value) AddClause(clauseVal Value) {
	purego.SyscallN(symAddClause, v.ref, clauseVal.ref)
}

// ---- Invoke ----

func (b Builder) CreateInvoke(fnType Type, fn Value, args []Value, thenBB, catchBB BasicBlock, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildInvoke2, b.ref, fnType.ref, fn.ref, valueRefPtr(args), uintptr(len(args)), thenBB.ref, catchBB.ref, cname)
	runtime.KeepAlive(name)
	return Value{r}
}

// ---- Resume ----

func (b Builder) CreateResume(v Value) Value {
	r, _, _ := purego.SyscallN(symBuildResume, b.ref, v.ref)
	return Value{r}
}

// ---- Atomics ----

func (b Builder) CreateFence(ordering AtomicOrdering, singleThread bool, name string) Value {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symBuildFence, b.ref, uintptr(ordering), boolToInt(singleThread), cname)
	runtime.KeepAlive(name)
	return Value{r}
}

func (b Builder) CreateAtomicRMW(op AtomicRMWBinOp, ptr, val Value, ordering AtomicOrdering, singleThread bool) Value {
	r, _, _ := purego.SyscallN(symBuildAtomicRMW, b.ref, uintptr(op), ptr.ref, val.ref, uintptr(ordering), boolToInt(singleThread))
	return Value{r}
}

func (b Builder) CreateAtomicCmpXchg(ptr, cmp, new_ Value, successOrdering, failureOrdering AtomicOrdering, singleThread bool) Value {
	r, _, _ := purego.SyscallN(symBuildAtomicCmpXchg, b.ref, ptr.ref, cmp.ref, new_.ref, uintptr(successOrdering), uintptr(failureOrdering), boolToInt(singleThread))
	return Value{r}
}

func (v Value) SetOrdering(ordering AtomicOrdering) {
	purego.SyscallN(symSetOrdering, v.ref, uintptr(ordering))
}

func (v Value) SetAtomicSingleThread(singleThread bool) {
	purego.SyscallN(symSetAtomicSingleThread, v.ref, boolToInt(singleThread))
}

// ---- Metadata (glue functions) ----

func (c Context) MDString(str string) Metadata {
	if symMDString2 == 0 {
		return Metadata{}
	}
	cstr := cString(str)
	r, _, _ := purego.SyscallN(symMDString2, c.ref, cstr, uintptr(len(str)))
	runtime.KeepAlive(str)
	return Metadata{r}
}

func (c Context) MDNode(mds []Metadata) Metadata {
	if symMDNode2 == 0 {
		return Metadata{}
	}
	r, _, _ := purego.SyscallN(symMDNode2, c.ref, metadataRefPtr(mds), uintptr(len(mds)))
	return Metadata{r}
}

func ConstantAsMetadata(v Value) Metadata {
	if symConstantAsMetadata == 0 {
		return Metadata{}
	}
	r, _, _ := purego.SyscallN(symConstantAsMetadata, v.ref)
	return Metadata{r}
}

func (v Value) GlobalObjectAddMetadata(kind uint, md Metadata) {
	if symGlobalObjectAddMetadata == 0 {
		return
	}
	purego.SyscallN(symGlobalObjectAddMetadata, v.ref, uintptr(kind), md.ref)
}

// ---- Inline asm (glue) ----

func InlineAsm(t Type, asmString, constraints string, hasSideEffects, isAlignStack bool, dialect InlineAsmDialect, canThrow bool) Value {
	if symGoGetInlineAsm == 0 {
		return Value{}
	}
	casm := cString(asmString)
	ccon := cString(constraints)
	r, _, _ := purego.SyscallN(symGoGetInlineAsm, t.ref,
		casm, uintptr(len(asmString)),
		ccon, uintptr(len(constraints)),
		boolToInt(hasSideEffects), boolToInt(isAlignStack),
		uintptr(dialect), boolToInt(canThrow))
	runtime.KeepAlive(asmString)
	runtime.KeepAlive(constraints)
	return Value{r}
}

// ---- Debug location (glue) ----

func (b Builder) SetCurrentDebugLocation(line, col uint, scope, inlinedAt Metadata) {
	if symGoSetCurrentDebugLocation == 0 {
		return
	}
	purego.SyscallN(symGoSetCurrentDebugLocation, b.ref, uintptr(line), uintptr(col), scope.ref, inlinedAt.ref)
}

func (b Builder) GetCurrentDebugLocation() DebugLoc {
	if symGoGetCurrentDebugLocation == 0 {
		return DebugLoc{}
	}
	var dl [4]uintptr
	purego.SyscallN(symGoGetCurrentDebugLocation, b.ref,
		uintptr(unsafe.Pointer(&dl[0])),
		uintptr(unsafe.Pointer(&dl[1])),
		uintptr(unsafe.Pointer(&dl[2])),
		uintptr(unsafe.Pointer(&dl[3])))
	return DebugLoc{Line: uint(dl[0]), Col: uint(dl[1]), Scope: Metadata{dl[2]}, InlinedAt: Metadata{dl[3]}}
}

// ---- Module verify ----

func (m Module) VerifyModule() error {
	var msg uintptr
	r, _, _ := purego.SyscallN(symVerifyModule, m.ref, 2, uintptr(unsafe.Pointer(&msg)))
	if r != 0 {
		s := goString(msg)
		purego.SyscallN(symDisposeMessage, msg)
		return fmt.Errorf("%s", s)
	}
	return nil
}

func (v Value) VerifyFunction() error {
	r, _, _ := purego.SyscallN(symVerifyFunction, v.ref, 2)
	if r != 0 {
		return fmt.Errorf("function verification failed")
	}
	return nil
}

// ---- Comdat ----

func (m Module) Comdat(name string) Comdat {
	cname := cString(name)
	r, _, _ := purego.SyscallN(symGetOrInsertComdat, m.ref, cname)
	runtime.KeepAlive(name)
	return Comdat{r}
}

func (c Comdat) SetSelectionKind(kind ComdatSelectionKind) {
	purego.SyscallN(symComdatSetSelectionKind, c.ref, uintptr(kind))
}

func (v Value) SetComdat(c Comdat) {
	purego.SyscallN(symSetComdat, v.ref, c.ref)
}
