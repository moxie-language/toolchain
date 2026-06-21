package com.moxie.lang

import com.intellij.psi.tree.IElementType
import com.intellij.psi.TokenType

class MoxieTokenType(debugName: String) : IElementType(debugName, MoxieLanguage.INSTANCE) {
    override fun toString(): String = "MoxieToken." + super.toString()
}

object MoxieTokenTypes {
    @JvmStatic val BAD_CHARACTER = TokenType.BAD_CHARACTER
    @JvmStatic val WHITE_SPACE = TokenType.WHITE_SPACE
    @JvmStatic val LINE_COMMENT = MoxieTokenType("LINE_COMMENT")
    @JvmStatic val BLOCK_COMMENT = MoxieTokenType("BLOCK_COMMENT")

    // Keywords
    @JvmStatic val BREAK = MoxieTokenType("BREAK")
    @JvmStatic val CASE = MoxieTokenType("CASE")
    @JvmStatic val CHAN = MoxieTokenType("CHAN")
    @JvmStatic val CONST = MoxieTokenType("CONST")
    @JvmStatic val CONTINUE = MoxieTokenType("CONTINUE")
    @JvmStatic val DEFAULT = MoxieTokenType("DEFAULT")
    @JvmStatic val DEFER = MoxieTokenType("DEFER")
    @JvmStatic val ELSE = MoxieTokenType("ELSE")
    @JvmStatic val FOR = MoxieTokenType("FOR")
    @JvmStatic val FUNC = MoxieTokenType("FUNC")
    @JvmStatic val GOTO = MoxieTokenType("GOTO")
    @JvmStatic val IF = MoxieTokenType("IF")
    @JvmStatic val IMPORT = MoxieTokenType("IMPORT")
    @JvmStatic val INTERFACE = MoxieTokenType("INTERFACE")
    @JvmStatic val MAP = MoxieTokenType("MAP")
    @JvmStatic val PACKAGE = MoxieTokenType("PACKAGE")
    @JvmStatic val RANGE = MoxieTokenType("RANGE")
    @JvmStatic val RETURN = MoxieTokenType("RETURN")
    @JvmStatic val SELECT = MoxieTokenType("SELECT")
    @JvmStatic val STRUCT = MoxieTokenType("STRUCT")
    @JvmStatic val SWITCH = MoxieTokenType("SWITCH")
    @JvmStatic val TYPE = MoxieTokenType("TYPE")
    @JvmStatic val VAR = MoxieTokenType("VAR")

    // Built-in functions
    @JvmStatic val PUSH = MoxieTokenType("PUSH")
    @JvmStatic val POP = MoxieTokenType("POP")
    @JvmStatic val RESIZE = MoxieTokenType("RESIZE")
    @JvmStatic val CAP = MoxieTokenType("CAP")
    @JvmStatic val CLEAR = MoxieTokenType("CLEAR")
    @JvmStatic val CLOSE = MoxieTokenType("CLOSE")
    @JvmStatic val COPY = MoxieTokenType("COPY")
    @JvmStatic val DELETE = MoxieTokenType("DELETE")
    @JvmStatic val LEN = MoxieTokenType("LEN")
    @JvmStatic val MAX = MoxieTokenType("MAX")
    @JvmStatic val MIN = MoxieTokenType("MIN")
    @JvmStatic val PANIC = MoxieTokenType("PANIC")
    @JvmStatic val PRINT = MoxieTokenType("PRINT")
    @JvmStatic val PRINTLN = MoxieTokenType("PRINTLN")
    @JvmStatic val RECOVER = MoxieTokenType("RECOVER")
    @JvmStatic val SPAWN = MoxieTokenType("SPAWN")

    // Built-in types
    @JvmStatic val BOOL = MoxieTokenType("BOOL")
    @JvmStatic val BYTE = MoxieTokenType("BYTE")
    @JvmStatic val ERROR = MoxieTokenType("ERROR")
    @JvmStatic val FLOAT32 = MoxieTokenType("FLOAT32")
    @JvmStatic val FLOAT64 = MoxieTokenType("FLOAT64")
    @JvmStatic val INT8 = MoxieTokenType("INT8")
    @JvmStatic val INT16 = MoxieTokenType("INT16")
    @JvmStatic val INT32 = MoxieTokenType("INT32")
    @JvmStatic val INT64 = MoxieTokenType("INT64")
    @JvmStatic val RUNE = MoxieTokenType("RUNE")
    @JvmStatic val STRING = MoxieTokenType("STRING")
    @JvmStatic val UINT8 = MoxieTokenType("UINT8")
    @JvmStatic val UINT16 = MoxieTokenType("UINT16")
    @JvmStatic val UINT32 = MoxieTokenType("UINT32")
    @JvmStatic val UINT64 = MoxieTokenType("UINT64")
    @JvmStatic val COMPARABLE = MoxieTokenType("COMPARABLE")

    // Boolean literals
    @JvmStatic val TRUE = MoxieTokenType("TRUE")
    @JvmStatic val FALSE = MoxieTokenType("FALSE")
    @JvmStatic val NIL = MoxieTokenType("NIL")
    @JvmStatic val IOTA = MoxieTokenType("IOTA")

    // Literals
    @JvmStatic val IDENTIFIER = MoxieTokenType("IDENTIFIER")
    @JvmStatic val INT_LITERAL = MoxieTokenType("INT_LITERAL")
    @JvmStatic val FLOAT_LITERAL = MoxieTokenType("FLOAT_LITERAL")
    @JvmStatic val IMAGINARY_LITERAL = MoxieTokenType("IMAGINARY_LITERAL")
    @JvmStatic val CHAR_LITERAL = MoxieTokenType("CHAR_LITERAL")
    @JvmStatic val STRING_LITERAL = MoxieTokenType("STRING_LITERAL")
    @JvmStatic val RAW_STRING_LITERAL = MoxieTokenType("RAW_STRING_LITERAL")

    // Operators and Delimiters
    @JvmStatic val PLUS = MoxieTokenType("PLUS")
    @JvmStatic val MINUS = MoxieTokenType("MINUS")
    @JvmStatic val STAR = MoxieTokenType("STAR")
    @JvmStatic val DIV = MoxieTokenType("DIV")
    @JvmStatic val MOD = MoxieTokenType("MOD")
    @JvmStatic val AND = MoxieTokenType("AND")
    @JvmStatic val OR = MoxieTokenType("OR")
    @JvmStatic val XOR = MoxieTokenType("XOR")
    @JvmStatic val SHL = MoxieTokenType("SHL")
    @JvmStatic val SHR = MoxieTokenType("SHR")
    @JvmStatic val AND_NOT = MoxieTokenType("AND_NOT")

    @JvmStatic val PLUS_ASSIGN = MoxieTokenType("PLUS_ASSIGN")
    @JvmStatic val MINUS_ASSIGN = MoxieTokenType("MINUS_ASSIGN")
    @JvmStatic val MUL_ASSIGN = MoxieTokenType("MUL_ASSIGN")
    @JvmStatic val DIV_ASSIGN = MoxieTokenType("DIV_ASSIGN")
    @JvmStatic val MOD_ASSIGN = MoxieTokenType("MOD_ASSIGN")

    @JvmStatic val AND_ASSIGN = MoxieTokenType("AND_ASSIGN")
    @JvmStatic val OR_ASSIGN = MoxieTokenType("OR_ASSIGN")
    @JvmStatic val XOR_ASSIGN = MoxieTokenType("XOR_ASSIGN")
    @JvmStatic val SHL_ASSIGN = MoxieTokenType("SHL_ASSIGN")
    @JvmStatic val SHR_ASSIGN = MoxieTokenType("SHR_ASSIGN")
    @JvmStatic val AND_NOT_ASSIGN = MoxieTokenType("AND_NOT_ASSIGN")

    @JvmStatic val LAND = MoxieTokenType("LAND")
    @JvmStatic val LOR = MoxieTokenType("LOR")
    @JvmStatic val ARROW = MoxieTokenType("ARROW")
    @JvmStatic val INC = MoxieTokenType("INC")
    @JvmStatic val DEC = MoxieTokenType("DEC")

    @JvmStatic val EQ = MoxieTokenType("EQ")
    @JvmStatic val LT = MoxieTokenType("LT")
    @JvmStatic val GT = MoxieTokenType("GT")
    @JvmStatic val ASSIGN = MoxieTokenType("ASSIGN")
    @JvmStatic val NOT = MoxieTokenType("NOT")

    @JvmStatic val NE = MoxieTokenType("NE")
    @JvmStatic val LE = MoxieTokenType("LE")
    @JvmStatic val GE = MoxieTokenType("GE")
    @JvmStatic val DEFINE = MoxieTokenType("DEFINE")
    @JvmStatic val ELLIPSIS = MoxieTokenType("ELLIPSIS")

    @JvmStatic val LPAREN = MoxieTokenType("LPAREN")
    @JvmStatic val LBRACKET = MoxieTokenType("LBRACKET")
    @JvmStatic val LBRACE = MoxieTokenType("LBRACE")
    @JvmStatic val COMMA = MoxieTokenType("COMMA")
    @JvmStatic val DOT = MoxieTokenType("DOT")

    @JvmStatic val RPAREN = MoxieTokenType("RPAREN")
    @JvmStatic val RBRACKET = MoxieTokenType("RBRACKET")
    @JvmStatic val RBRACE = MoxieTokenType("RBRACE")
    @JvmStatic val SEMICOLON = MoxieTokenType("SEMICOLON")
    @JvmStatic val COLON = MoxieTokenType("COLON")
}
