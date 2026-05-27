package transform

import (
	"tinygo.org/x/go-llvm"
)

// Return a list of values (actually, instructions) where this value is used as
// an operand.
func getUses(value llvm.Value) []llvm.Value {
	if value.IsNil() {
		return nil
	}
	var uses []llvm.Value
	use := value.FirstUse()
	for !use.IsNil() {
		uses = append(uses, use.User())
		use = use.NextUse()
	}
	return uses
}

// hasUses returns whether the given value has any uses. It is equivalent to
// getUses(value) != nil but faster.
func hasUses(value llvm.Value) bool {
	if value.IsNil() {
		return false
	}
	return !value.FirstUse().IsNil()
}

// getGlobalBytes returns the slice contained in the array of the provided
// global. The builder parameter is only used for constant operations.
func getGlobalBytes(global llvm.Value, builder llvm.Builder) []byte {
	value := global.Initializer()
	buf := make([]byte, value.Type().ArrayLength())
	for i := range buf {
		buf[i] = byte(builder.CreateExtractValue(value, i, "").ZExtValue())
	}
	return buf
}

// stripPointerCasts strips instruction pointer casts (getelementptr and
// bitcast) and returns the original value without the casts.
func stripPointerCasts(value llvm.Value) llvm.Value {
	if !value.IsAConstantExpr().IsNil() {
		switch value.Opcode() {
		case llvm.GetElementPtr, llvm.BitCast:
			return stripPointerCasts(value.Operand(0))
		}
	}
	if !value.IsAInstruction().IsNil() {
		switch value.InstructionOpcode() {
		case llvm.GetElementPtr, llvm.BitCast:
			return stripPointerCasts(value.Operand(0))
		}
	}
	return value
}
