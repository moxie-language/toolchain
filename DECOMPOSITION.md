# Compiler Decomposition Plan

Module: `git.smesh.lol/moxie`

## Package Dependency Graph

```
pkg/token/          (no deps)
    |
pkg/syntax/         (imports token/)
    |
pkg/types/          (imports syntax/, token/)
    |
pkg/ssa/            (imports syntax/, types/, token/)
    |
pkg/emit/           (imports syntax/, ssa/, types/, token/)
    |
pkg/mxh/            (imports syntax/, types/, token/)
    |
cmd/mxc/            (imports all of the above)
```

## Package Layout

### pkg/token/ (359 lines, no changes needed)

```
pos.mx        - Pos, PosBase, position_, MakePos, NewFileBase
tokens.mx     - Token, Operator, LitKind constants + Itoa, Itoa64, simpleItoa64
```

Already clean. No split needed.

### pkg/syntax/ (currently 4676 lines -> split to ~5 files)

The Parser struct stays in one package. Split is file-level organization only - all files are `package syntax`.

```
nodes.mx      - (454 lines) Node, Decl, Stmt, Expr interfaces; all AST struct types;
                ConstVal, TypeAndValue, Type, Group, Field, FuncType, etc.
                KEEP AS-IS.

source.mx     - (204 lines) Source struct, initBytes, nextChar, pos, rewind.
                KEEP AS-IS.

syntax.mx     - (82 lines) Error type, CompileExportMap, ErrorHandler, PragmaHandler.
                KEEP AS-IS.

scanner.mx    - (999 lines) Scanner struct, Init/InitBytes, Next, Ident, Number,
                string/char/comment scanning, keyword table.
                KEEP AS-IS.

parser.mx     -> SPLIT INTO:

  parse_init.mx   (~350 lines) Parser struct definition, init, initBytes,
                   error handling (errorAt, syntaxErrorAt, syntaxError, advance),
                   got, want, gotAssign, posAt, pos, trace, print,
                   takePragma, clearPragma, updateBase, commentText, tokstring.

  parse_decl.mx   (~450 lines) fileOrNil, isEmptyFuncDecl, list, appendGroup,
                   importDecl, constDecl, typeDecl, varDecl,
                   funcDeclOrNil, funcBody, extractName, isTypeElem.

  parse_expr.mx   (~400 lines) expr, binaryExpr, unaryExpr, callStmt,
                   operand, pexpr, isValue, bare_complitexpr, complitexpr,
                   argList, name, nameList, qualifiedName, exprList, typeList,
                   Unparen, UnpackListExpr.

  parse_type.mx   (~570 lines) type_, newIndirect, typeOrNil, typeInstance,
                   funcType, arrayType, sliceType, chanElem,
                   structType, interfaceType, funcResult, addField, fieldDecl,
                   arrayOrTArgs, oliteral, methodDecl, embeddedElem, embeddedTerm,
                   paramDeclOrNil, paramList, badExpr.

  parse_stmt.mx   (~600 lines) simpleStmt, newRangeClause, newAssignStmt,
                   labeledStmtOrNil, blockStmt, declStmt,
                   forStmt, header, emphasize, ifStmt,
                   switchStmt, selectStmt, caseClause, commClause,
                   stmtOrNil, stmtList.
```

Exports required from syntax/:
- `Parser`, `Scanner`, `Source` (structs)
- All AST node types (already exported)
- `NewName`, `NewFileBase`, `String`, `StartPos`
- `Error`, `CompileExportMap`
- `ErrorHandler`, `PragmaHandler`, `Pragma`, `Mode`

### pkg/types/ (3991 lines, already split into 13 files)

Already at good granularity. Largest file is `tc_const.mx` at 801 lines.

```
tc_types.mx     - (583 lines) Type interface, Basic, Named, Pointer, Slice, Array,
                  Struct, Map, Chan, Signature, Tuple, Interface, IfaceMethod;
                  NewArray, NewSlice, NewPointer, NewNamed, NewSignature,
                  NewTCStruct, NewTCMap, NewTCChan, NewTCInterface, NewTCIfaceMethod;
                  safeUnderlying, Identical, AssignableTo.

tc_universe.mx  - (307 lines) Universe scope, Typ[], InitUniverse, predeclared types.

tc_checker.mx   - (157 lines) Checker struct, Config, Info, TypeError, errorf, record.

tc_object.mx    - (150 lines) Object interface, TypeName, Var, Func, Const, Label;
                  TCVar, TCFunc, ConstVal.

tc_scope.mx     - (63 lines) Scope struct, Lookup, Insert, Parent.

tc_package.mx   - (44 lines) TCPackage struct.

tc_info.mx      - (150 lines) Info struct, TypeAndValue.

tc_assign.mx    - (284 lines) Identical, Assignable, normaliseKind, isTextType.

tc_expr.mx      - (481 lines) Checker.exprType, typeOperation, evalUnaryOp.

tc_const.mx     - (801 lines) Constant evaluation, parseIntLocal, cvInt, evalConstOp.

tc_decl.mx      - (306 lines) Checker.declStmt, resolveType.

tc_resolve.mx   - (264 lines) Checker.resolve, walkScope.

tc_stmt.mx      - (316 lines) Checker.stmt, checkShortVarDecl.
```

Exports required from types/:
- All Type concrete types (Basic, Named, Pointer, Slice, etc.)
- `Type` interface
- `Checker`, `Config`, `Info`, `TypeError`
- `Scope`, `TCPackage`, `Object`, `TCVar`, `TCFunc`
- `ConstVal` interface
- `InitUniverse`, `Universe`
- `Typ[]` (predeclared type array)
- `Identical`, `AssignableTo`, `safeUnderlying`
- `NewArray`, `NewSlice`, `NewPointer`, etc. (constructors)

### pkg/ssa/ (currently 5743 lines -> split to 7 files)

```
ssa_types.mx       - (969 lines) KEEP AS-IS. All SSA value/instruction types,
                     SSAProgram, SSAPackage, SSAFunction, SSABasicBlock.

ssa_pkg_builder.mx - (~600 lines) ssaPkgBuilder struct, CreatePackage,
                     registerImportsAndTypes, registerFile, registerImport,
                     registerFunc, registerMethod, registerVar, registerType,
                     registerConst, buildFile, buildVarInits,
                     pkgResolveTypeAST, pkgResolveSigAST,
                     ssaRecvTypeName, findMethod,
                     checkInitBody, checkInitStmt, checkInitExpr, isNoContextExtern.

ssa_func.mx        - (~800 lines) ssaFuncBuilder struct definition,
                     newSSAFuncBuilder, newBlock, enterScope, exitScope,
                     labelBlock, emit, nextName, buildBody,
                     emitAlloc, emitStore, emitLoad, emitReturn, blockTerminated,
                     buildBlock, buildStmt, buildAssign, coerceToInterface,
                     buildStore, buildShortVarDecl, buildLocalDecl,
                     evalLocalConstExpr.

ssa_stmt.mx        - (~800 lines) buildIf, buildFor, buildRangeLoop,
                     buildSwitch, buildTypeSwitch, buildSelect,
                     isSelfTailCall, buildReturn, buildBranch,
                     buildGoStmt, buildDeferStmt.

ssa_expr.mx        - (~1300 lines) buildExpr, buildIdent, builtinValue, buildLit,
                     buildOperation, buildCall, coerceArgsToInterface,
                     wrapVariadicArgs, buildArgs, buildBuiltinCall, emitMake,
                     buildSelector, buildMethodValue, ensureImportedSSAPackage,
                     buildPkgMember, buildSelectorAddr, buildIndex, buildSlice,
                     buildAssert, buildCompositeLit, buildArrayLit,
                     buildCompositeLitWithType, buildSliceLit, buildFuncLit,
                     saveVars, removeVar, buildPkgMemberAddr, buildPkgCall,
                     buildUnsafeSlice, lookupObject, getOrCreateFreeVar, lookupVar.

ssa_resolve.mx     - (~700 lines) resolveMethodCallWithRecv, buildIfaceMethodCall,
                     resolveType, resolveTypeAST, resolveInterfaceTypeAST,
                     resolveStructTypeAST, resolveFuncTypeAST,
                     fieldIndex, findEmbeddedField, findEmbeddedFieldPath, fieldType.

ssa_generics.mx    - (~500 lines) mangleGenericName, resolveTypeExprWithSubst,
                     resolveSignatureWithSubst, matchTypeParam,
                     inferFromConstraints, extractConstraintTypeParams,
                     inferTypeArgsFromFunc, monomorphizeAndCall, emitCallToSSAFunc,
                     tryGenericLocalCall, tryGenericPkgCall,
                     tryGenericCallFromIndex, resolveExplicitTypeArgs,
                     exprTypeName, localizeConstVal, ssaExtractConst, ssaExprNames.
```

Exports required from ssa/:
- `SSAProgram`, `SSAPackage`, `SSAFunction`, `SSABasicBlock`
- All SSA instruction/value types
- `SSAValue`, `SSAInstruction`, `SSAMember` interfaces
- `CreatePackage`
- `SSAOp` constants

### pkg/emit/ (currently 9540 lines -> split to 12 files)

The `irEmitter` struct stays in `pkg/emit` but methods are spread across files.

```
emitter.mx         - (~200 lines) irEmitter struct definition, newIREmitter,
                     releaseAfterEmit, pkgMembersSorted, sortedKeys, w.

types.mx           - (~400 lines) llvmType, llvmBasicType, llvmStructType,
                     ptrType, intptrType, sliceType, ifaceType,
                     llvmTypeBits, typeBits, llvmArrayByteSize, dataLayout,
                     regName, setRegType, resolvedType, nextReg2,
                     operandNoSideEffect, operand, constOperand.

globals.mx         - (~300 lines) emit (top-level orchestrator), emitLibMain,
                     emitAnonFuncs, emitGlobal, globalName, funcSymbol,
                     isPkgFunc, inferGlobalTypeFromLoads, resolveGlobalDeclType,
                     emitAsmAlias, asmAlias, declareRuntime, declareExternalGlobal,
                     declareExternalFunc, addStringConst, strConstGlobal.

function.mx        - (~600 lines) emitFunction, emitFuncDecl, emitBlock,
                     emitInstr, blockLabel, resolveResultType, funcRetType,
                     emitZeroInit.

alloc.mx           - (~600 lines) emitAlloc, emitStore, emitZeroReg,
                     inferAllocTypeFromStores, inferAllocTypeFromReturn,
                     inferAllocTypeFromCallArgs, inferAllocTypeFromAppendUsage,
                     inferAllocTypeFromFieldAddrs, inferAllocTypeFromUsage,
                     parseStructFields, llvmTrimSpace.

ops.mx             - (~1400 lines) emitBinOp, emitUnOp, emitIntCast, coerceInt,
                     emitSliceConcat, emitStringCompare,
                     emitStructCompare, emitArrayCompare,
                     emitConvert, emitChangeType, conversionOp.

calls.mx           - (~600 lines) emitCall, emitBuiltinCall, emitPrintArg,
                     funcCallRetType, callSig, callArgType.

control.mx         - (~400 lines) emitPhi, emitReturn, emitJump, emitIf,
                     blockHasReturn, isComparisonOp.

iface.mx           - (~900 lines) emitMakeInterface, typeIDGlobal,
                     reflectTypeName, findIfaceImpls, findEmbedMethod,
                     implFuncSym, declareExtInvoke, emitExtInvokeCall,
                     implTypeID, emitEmbedChainGEP, prepareInvokeArgs,
                     emitInvokeArgs, emitInvoke, ifaceImpl struct.

collections.mx     - (~800 lines) emitFieldAddr, emitIndexAddr, emitExtract,
                     emitMakeSlice, emitSliceOp, emitMakeChan, emitChanRecv,
                     emitChanSend, sextToIpt, sliceIdxToIpt, chanElemType,
                     emitTypeAssert, extractScalarFromIface,
                     emitMakeMap, emitMapUpdate, emitLookup, isStringLike,
                     emitMakeClosure, closureBindingType, closureContextType,
                     freeVarType, emitFreeVarUnpack, emitPanic,
                     emitRange, emitNext, emitNextSlice, emitNextArray,
                     emitNextPtrArray, emitNextMap.

defer.mx           - (~350 lines) deferRetType, deferArgType, deferIsDirectCall,
                     deferStructType, emitDefer, emitRunDefers,
                     emitDeferBuiltin, emitDeferDispatch.

dealloc.mx         - (~260 lines) deallocSetup, deallocAfterAlloc,
                     deallocBeforeReturn, deallocEscapeResult,
                     scopeTrackAlloc, scopeEscapeReturnResults,
                     emitScopeFreeAll, emitScopeFreesTo,
                     emitScopeSave, emitScopeRelocateOnStore.

util.mx            - (~400 lines) irItoa, irItoa64, irHex64, irFtoa,
                     irParseInt64, irParseIntWidth, floatLitForLLVM,
                     runeToUTF8, irEscapeString, irNeedsQuote,
                     irGlobalSymbol, IrGlobalSymbol, simpleItoa,
                     flattenTypeStr, extractTupleField, parseArrayType.

compile_exports.mx - (497 lines) KEEP AS-IS. C-shared ABI, package registry,
                     ParseTypeDesc, ParseSignatureDesc.
```

Exports required from emit/:
- `CompileToIR(pkg *ssa.SSAPackage, triple string) string`
- `TypeCheckOnly(src []byte, pkgName string)`
- `IrGlobalSymbol(pkg, name string) string`
- `ImportRegistry` (package-level var)
- `ParseTypeDesc`, `ParseSignatureDesc`, `SplitSemicolon`
- `EnsureImportRegistry`, `ScanGenericDecls`

### pkg/mxh/ (NEW - extracted from _mxc_stage4/main.mx and compile_exports.mx)

```
mxh.mx         - (~400 lines) typeDescPkgPath, typeDescStr, sigDescStr,
                 GenerateMxh, LoadMxh, CachedMxhPath.
                 Extracted from _mxc_stage4/main.mx lines 1447-1843.
```

Exports:
- `GenerateMxh(pkg *types.TCPackage) string`
- `LoadMxh(path string, registry map[string]*types.TCPackage)`
- `CachedMxhPath(cacheBase, pkgPath, hash, version string) string`

### pkg/rewrite/ (NEW - extracted from emit/ inline typecheck)

The inline type-checker in ir_emit.mx (Group 17, ~1200 lines) mixes source
rewriting with type inference. Split into:

```
rewrite.mx     - (~400 lines) stripDuplicatePackageClauses,
                 rewriteSliceMakeLiterals, findSliceTypeStart,
                 findMatchingBrace, rewriteChanMakeLiterals.
                 Source-to-source transforms that run before parsing.

resolve.mx     - (~800 lines) tcResolveNameInline, tcResolveInterfaceInline,
                 tcResolveFuncInline, tcResolveFuncInlineWithRecv,
                 tcResolveRecvType, tcResolveFieldList,
                 tcInferTypeFromExpr, tcEvalConstExpr,
                 resolveArrayLenFromSrc, resolveArrayLenFromConstMap,
                 newTCPackageWithUniverse.
                 Inline type resolution that bridges syntax AST -> types.Type.
```

Exports:
- `RewriteSliceMakeLiterals(src []byte) []byte`
- `RewriteChanMakeLiterals(src []byte) []byte`
- `StripDuplicatePackageClauses(src []byte) []byte`
- `ResolveNameInline(expr syntax.Expr, scope *types.Scope) types.Type`
- `EvalConstExpr(expr syntax.Expr, scope *types.Scope, depth int32) types.ConstVal`

### cmd/mxc/ (NEW - the compiler CLI, extracted from _mxc_stage4/main.mx)

```
main.mx        - (~200 lines) main(), argument parsing, subcommand dispatch.

os_shims.mx    - (~140 lines) cRun, cGetenv, cListdir, cWrite, cWritefile,
                 cMkdir, cReadfile, cFilesize + Go wrappers
                 (writeStr, writeFile, mkdirAll, readFile, fatal, getenv,
                 getArgs, listFiles, run).

util.mx        - (~120 lines) splitLines, hasPrefix, hasSuffix, trimSpace,
                 joinPath, pathBase, pathDir, splitSlash, cleanPath,
                 joinStrings, appendUniq, fnvHash, splitBySpace, safeName.

module.mx      - (~350 lines) findModuleRoot, mergeModReplaces,
                 parseReplaceLine, modRequire, parseModRequires,
                 isRemotePath, repoLockPath, acquireRepoLock,
                 releaseRepoLock, autoFetchRepo, gitURLToModPath,
                 getMoxiePath, moxieCacheDir.

discover.mx    - (~400 lines) pkgInfo struct, discoverPkg, resolver struct,
                 walk, resolveAll, extractPkgName, extractImports,
                 extractQuoted, shouldSkipFile, hasBuildConstraint,
                 fileContainsPart, hasImport.

compile.mx     - (~300 lines) compileSource, registerBuiltins, addPtrMethod,
                 pkgCacheDir, pkgCacheKey, versionedCacheName, cachedBcPath,
                 isStdlib, isBuiltinOnly, loadSysroot, concatSources,
                 concatSourceMap, concatLineToFile, remapErrorLines,
                 remapOneLine, srcSpan struct.

build.mx       - (~400 lines) cmdBuild (the main build pipeline),
                 compileBc, link step with per-file .bc -> .o.

cache.mx       - (~300 lines) cmdCacheWarm, cacheClean, cmdUpdate,
                 removeAll, walkStdlibPkgs.

test.mx        - (~300 lines) cmdTest, scanTestFuncs, generateTestMain,
                 hasImportStr, resolveTestDeps, concatTestSources.

fetch.mx       - (~100 lines) cmdFetch.
```

## Symbols Requiring Export

### From pkg/token/ (already exported)
- `Pos`, `PosBase`, `Token`, `Operator`, `LitKind`
- `MakePos`, `NewFileBase`, `NewLinePosBase`
- `Itoa`, `Itoa64`
- All token/operator constants

### From pkg/syntax/ (needs work)
- `Parser`, `Scanner`, `Source` structs
- All AST node types (ImportDecl, FuncDecl, etc.)
- `Node`, `Decl`, `Stmt`, `SimpleStmt`, `Expr` interfaces
- `Type`, `ConstVal` interfaces
- `NewName`, `String`, `StartPos`
- `Error`, `CompileExportMap`
- `ErrorHandler`, `PragmaHandler`
- Fields on `node` struct: `pos` -> embedded `Pos` or use `SetPos()`/`Pos()`

### From pkg/types/ (needs work)
- `Type` interface + all concrete types
- `Checker`, `Config`, `Info`
- `Scope`, `TCPackage`, `Object`
- `InitUniverse`, `Universe` (function + var)
- `Typ` array
- All `New*` constructors
- `Identical`, `AssignableTo`, `safeUnderlying` -> `SafeUnderlying`
- `ConstVal` interface (shared with syntax/)

### From pkg/ssa/
- `SSAProgram`, `SSAPackage`, `SSAFunction`, `SSABasicBlock`
- All 32 instruction types
- `SSAValue`, `SSAInstruction`, `SSAMember` interfaces
- `CreatePackage`
- `SSAOp` and all op constants

### From pkg/emit/
- `CompileToIR`, `TypeCheckOnly`
- `IrGlobalSymbol`
- `ImportRegistry`
- `ParseTypeDesc`, `ParseSignatureDesc`, `SplitSemicolon`
- `EnsureImportRegistry`, `ScanGenericDecls`

### From pkg/mxh/
- `GenerateMxh`, `LoadMxh`, `CachedMxhPath`

### From pkg/rewrite/
- `RewriteSliceMakeLiterals`, `RewriteChanMakeLiterals`
- `StripDuplicatePackageClauses`
- `ResolveNameInline`, `EvalConstExpr`

## Unexported -> Exported Renames

Fields and functions that must be exported for cross-package access:

### pkg/syntax/
- `node.pos` -> embed `token.Pos` directly (use `SetPos()`/`Pos()` methods)
- `expr.exprFlags` -> either embed or export
- `stmt`, `simpleStmt`, `decl` base structs -> currently used via embedding, fine as-is
- `compileExportMap` -> already renamed to `CompileExportMap`

### pkg/types/
- `safeUnderlying` -> `SafeUnderlying`
- Checker internal methods that emit/ needs to call - audit per call site

### pkg/ssa/
- `SSAFunction.name` -> `SSAFunction.Name` (field, not method - currently has SSAName() accessor)
- `ssaPkgBuilder`, `ssaFuncBuilder` -> can stay unexported if CreatePackage is the entry point

### pkg/emit/
- `irEmitter` struct -> can stay unexported, `CompileToIR` is the public API
- All `emit*` methods -> stay unexported (methods on unexported struct)
- `irGlobalSymbol` -> already has exported `IrGlobalSymbol` wrapper

## Execution Order

1. Split `pkg/syntax/parser.mx` into 5 files (same package, no export changes)
2. Split `pkg/ssa/ssa_builder.mx` into 6 files (same package, no export changes)
3. Split `pkg/emit/ir_emit.mx` into 12 files (same package, no export changes)
4. Extract `pkg/mxh/` from stage4 main.mx
5. Extract `pkg/rewrite/` from emit/ir_emit.mx (the inline typecheck)
6. Export required symbols in pkg/types/, pkg/syntax/, pkg/ssa/
7. Build `cmd/mxc/` that imports pkg/* instead of vendoring flat copies
8. Verify each package compiles independently via legacy Go compiler
9. Delete `_mxc_stage4/` flat copy once `cmd/mxc/` works

Steps 1-3 are safe refactors (same package, just file splits). Steps 4-5
create new packages. Steps 6-7 are the interface boundary work. Step 8
verifies nothing exceeds LLVM memory limits per compilation unit.
