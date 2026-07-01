package loader

// Moxie source rewrites — thin wrappers around moxie/mxtext.

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"moxie/mxtext"
)

type pipeRewrite = mxtext.PipeRewrite

func isMoxieStringTarget(importPath string) bool {
	return mxtext.IsMoxieStringTarget(importPath)
}

func rewriteStringLiterals(file *ast.File) {
	mxtext.RewriteStringLiterals(file)
}

func rewriteStringTypes(file *ast.File) {
	mxtext.RewriteStringTypes(file)
}

func rewriteStringConversions(file *ast.File) {
	mxtext.RewriteStringConversions(file)
}

func wrapInterfaceMandatedReturns(file *ast.File) {
	mxtext.WrapInterfaceMandatedReturns(file)
}

func rewriteAddToPipe(file *ast.File) bool {
	return mxtext.RewriteAddToPipe(file)
}

func rewriteConstPipes(files []*ast.File) {
	mxtext.RewriteConstPipes(files)
}

func findPipeConcat(files []*ast.File, info *types.Info) []pipeRewrite {
	return mxtext.FindPipeConcat(files, info)
}

func applyPipeRewrites(files []*ast.File, rewrites []pipeRewrite) {
	mxtext.ApplyPipeRewrites(files, rewrites)
}

func filterPipeErrors(errs []error) []error {
	return mxtext.FilterPipeErrors(errs)
}

func findByteComparisons(files []*ast.File, info *types.Info) []*ast.BinaryExpr {
	return mxtext.FindByteComparisons(files, info)
}

func applyByteComparisonRewrites(files []*ast.File, exprs []*ast.BinaryExpr) {
	mxtext.ApplyByteComparisonRewrites(files, exprs)
}

func findByteSwitches(files []*ast.File, info *types.Info) []*ast.SwitchStmt {
	return mxtext.FindByteSwitches(files, info)
}

func applyByteSwitchRewrites(switches []*ast.SwitchStmt) {
	mxtext.ApplyByteSwitchRewrites(switches)
}

func filterByteCompareErrors(errs []error) []error {
	return mxtext.FilterByteCompareErrors(errs)
}

func filterStringByteMismatch(errs []error) []error {
	return mxtext.FilterStringByteMismatch(errs)
}

func filterNoNewVarErrors(errs []error) []error {
	var out []error
	for _, e := range errs {
		msg := e.Error()
		if strings.Contains(msg, "no new variables on left side of :=") {
			continue
		}
		if strings.Contains(msg, "declared and not used") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// filterImpossibleAssertErrors filters out "impossible type assertion" errors
// for types that are valid Moxie assertion targets. In Moxie, all builtin types
// have auto-generated String() string methods, so assertions on Stringer
// interface values against basic types (int32, []byte, etc.) are always valid.
func filterImpossibleAssertErrors(errs []error) []error {
	var out []error
	for _, e := range errs {
		msg := e.Error()
		if strings.Contains(msg, "missing method") {
			continue
		}
		out = append(out, e)
	}
	return out
}

func rewriteAddAssign(files []*ast.File, info *types.Info) int {
	return mxtext.RewriteAddAssign(files, info)
}

func injectMoxieByteBuiltins(fset *token.FileSet, pkgName string) *ast.File {
	return mxtext.InjectMoxieByteBuiltins(fset, pkgName)
}

func hasMoxieSecallocRefs(files []*ast.File) bool {
	return mxtext.HasMoxieSecallocRefs(files)
}

func rewriteChanLiterals(src []byte, fset *token.FileSet) mxtext.RewriteResult {
	return mxtext.RewriteChanLiterals(src, fset)
}

func checkPlusOnText(files []*ast.File, info *types.Info, fset *token.FileSet) []error {
	return mxtext.CheckPlusOnText(files, info, fset)
}

func rewriteSliceLiterals(src []byte, fset *token.FileSet, priorOffsets ...[]int) mxtext.RewriteResult {
	return mxtext.RewriteSliceLiterals(src, fset, priorOffsets...)
}
