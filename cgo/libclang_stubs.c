
// This file implements some small trampoline functions. The signatures
// are slightly different from the ones defined in libclang.go, but they
// should be ABI compatible.

#include <clang-c/Index.h> // If this fails, libclang headers aren't available. Please take a look here: https://moxie.dev/docs/guides/build/

CXCursor moxie_clang_getTranslationUnitCursor(CXTranslationUnit tu) {
	return clang_getTranslationUnitCursor(tu);
}

unsigned moxie_clang_visitChildren(CXCursor parent, CXCursorVisitor visitor, CXClientData client_data) {
	return clang_visitChildren(parent, visitor, client_data);
}

CXString moxie_clang_getCursorSpelling(CXCursor c) {
	return clang_getCursorSpelling(c);
}

CXString moxie_clang_getCursorPrettyPrinted(CXCursor c, CXPrintingPolicy policy) {
	return clang_getCursorPrettyPrinted(c, policy);
}

CXPrintingPolicy moxie_clang_getCursorPrintingPolicy(CXCursor c) {
	return clang_getCursorPrintingPolicy(c);
}

enum CXCursorKind moxie_clang_getCursorKind(CXCursor c) {
	return clang_getCursorKind(c);
}

CXType moxie_clang_getCursorType(CXCursor c) {
	return clang_getCursorType(c);
}

CXCursor moxie_clang_getTypeDeclaration(CXType t) {
	return clang_getTypeDeclaration(t);
}

CXType moxie_clang_getTypedefDeclUnderlyingType(CXCursor c) {
	return clang_getTypedefDeclUnderlyingType(c);
}

CXType moxie_clang_getCursorResultType(CXCursor c) {
	return clang_getCursorResultType(c);
}

int moxie_clang_Cursor_getNumArguments(CXCursor c) {
	return clang_Cursor_getNumArguments(c);
}

CXCursor moxie_clang_Cursor_getArgument(CXCursor c, unsigned i) {
	return clang_Cursor_getArgument(c, i);
}

enum CX_StorageClass moxie_clang_Cursor_getStorageClass(CXCursor c) {
	return clang_Cursor_getStorageClass(c);
}

CXSourceLocation moxie_clang_getCursorLocation(CXCursor c) {
	return clang_getCursorLocation(c);
}

CXSourceRange moxie_clang_getCursorExtent(CXCursor c) {
	return clang_getCursorExtent(c);
}

CXTranslationUnit moxie_clang_Cursor_getTranslationUnit(CXCursor c) {
	return clang_Cursor_getTranslationUnit(c);
}

long long moxie_clang_getEnumConstantDeclValue(CXCursor c) {
	return clang_getEnumConstantDeclValue(c);
}

CXType moxie_clang_getEnumDeclIntegerType(CXCursor c) {
	return clang_getEnumDeclIntegerType(c);
}

unsigned moxie_clang_Cursor_isAnonymous(CXCursor c) {
	return clang_Cursor_isAnonymous(c);
}

unsigned moxie_clang_Cursor_isBitField(CXCursor c) {
	return clang_Cursor_isBitField(c);
}

unsigned moxie_clang_Cursor_isMacroFunctionLike(CXCursor c) {
	return clang_Cursor_isMacroFunctionLike(c);
}
