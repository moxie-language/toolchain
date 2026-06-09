#include "llvm-c/DebugInfo.h"

#ifdef __cplusplus
extern "C" {
#endif

LLVMMetadataRef MoxieCreateCompileUnit(
    LLVMDIBuilderRef builder,
    LLVMDWARFSourceLanguage lang,
    LLVMMetadataRef fileRef,
    const char *producer, size_t producerLen,
    LLVMBool isOptimized,
    unsigned runtimeVer,
    const char *sysroot, size_t sysrootLen,
    const char *sdk, size_t sdkLen)
{
    return LLVMDIBuilderCreateCompileUnit(
        builder, lang, fileRef,
        producer, producerLen,
        isOptimized,
        "", 0,           /* flags */
        runtimeVer,
        "", 0,           /* splitName */
        LLVMDWARFEmissionFull,
        0,               /* DWOId */
        1,               /* splitDebugInlining */
        0,               /* debugInfoForProfiling */
        sysroot, sysrootLen,
        sdk, sdkLen);
}

LLVMMetadataRef MoxieCreateStructType(
    LLVMDIBuilderRef builder,
    LLVMMetadataRef scope,
    const char *name, size_t nameLen,
    LLVMMetadataRef file,
    unsigned line,
    uint64_t sizeInBits,
    uint32_t alignInBits,
    LLVMDIFlags flags,
    LLVMMetadataRef derivedFrom,
    LLVMMetadataRef *elements, unsigned numElements,
    LLVMMetadataRef vTableHolder,
    const char *uniqueID, size_t uniqueIDLen)
{
    return LLVMDIBuilderCreateStructType(
        builder, scope,
        name, nameLen,
        file, line,
        sizeInBits, alignInBits, flags,
        derivedFrom,
        elements, numElements,
        0,               /* runtimeLang */
        vTableHolder,
        uniqueID, uniqueIDLen);
}

#ifdef __cplusplus
}
#endif
