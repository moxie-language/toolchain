package compiler

// This file contains some utility functions related to error handling.

import (
	"fmt"
	"go/token"
	"path/filepath"

	"tinygo.org/x/go-llvm"
)

// Error is a compiler diagnostic with file/position info.
// Mirror of go/types.Error fields, kept local so compiler/ does not need to
// import go/types just for the error type. Diagnostics package handles both.
type Error struct {
	Fset *token.FileSet
	Pos  token.Pos
	Msg  string
}

func (e Error) Error() string {
	if e.Fset != nil {
		return fmt.Sprintf("%s: %s", e.Fset.Position(e.Pos), e.Msg)
	}
	return e.Msg
}

// makeError makes it easy to create an error from a token.Pos with a message.
func (c *compilerContext) makeError(pos token.Pos, msg string) Error {
	return Error{
		Fset: c.mxProgram.Fset,
		Pos:  pos,
		Msg:  msg,
	}
}

// addError adds a new compiler diagnostic with the given location and message.
func (c *compilerContext) addError(pos token.Pos, msg string) {
	c.diagnostics = append(c.diagnostics, c.makeError(pos, msg))
}

// getPosition returns the position information for the given value, as far as
// it is available.
func getPosition(val llvm.Value) token.Position {
	if !val.IsAInstruction().IsNil() {
		loc := val.InstructionDebugLoc()
		if loc.IsNil() {
			return token.Position{}
		}
		file := loc.LocationScope().ScopeFile()
		return token.Position{
			Filename: filepath.Join(file.FileDirectory(), file.FileFilename()),
			Line:     int(loc.LocationLine()),
			Column:   int(loc.LocationColumn()),
		}
	} else if !val.IsAFunction().IsNil() {
		loc := val.Subprogram()
		if loc.IsNil() {
			return token.Position{}
		}
		file := loc.ScopeFile()
		return token.Position{
			Filename: filepath.Join(file.FileDirectory(), file.FileFilename()),
			Line:     int(loc.SubprogramLine()),
		}
	} else {
		return token.Position{}
	}
}
