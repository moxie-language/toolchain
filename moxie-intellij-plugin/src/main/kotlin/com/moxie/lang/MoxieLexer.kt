package com.moxie.lang

import com.intellij.lexer.LexerBase
import com.intellij.psi.tree.IElementType

class MoxieLexer : LexerBase() {
    private var buffer: CharSequence = ""
    private var startOffset: Int = 0
    private var endOffset: Int = 0
    private var state: Int = 0
    private var tokenType: IElementType? = null
    private var tokenStart: Int = 0
    private var tokenEnd: Int = 0

    companion object {
        private val KEYWORDS = mapOf(
            "break" to MoxieTokenTypes.BREAK,
            "case" to MoxieTokenTypes.CASE,
            "chan" to MoxieTokenTypes.CHAN,
            "const" to MoxieTokenTypes.CONST,
            "continue" to MoxieTokenTypes.CONTINUE,
            "default" to MoxieTokenTypes.DEFAULT,
            "defer" to MoxieTokenTypes.DEFER,
            "else" to MoxieTokenTypes.ELSE,
            "for" to MoxieTokenTypes.FOR,
            "func" to MoxieTokenTypes.FUNC,
            "goto" to MoxieTokenTypes.GOTO,
            "if" to MoxieTokenTypes.IF,
            "import" to MoxieTokenTypes.IMPORT,
            "interface" to MoxieTokenTypes.INTERFACE,
            "map" to MoxieTokenTypes.MAP,
            "package" to MoxieTokenTypes.PACKAGE,
            "range" to MoxieTokenTypes.RANGE,
            "return" to MoxieTokenTypes.RETURN,
            "select" to MoxieTokenTypes.SELECT,
            "struct" to MoxieTokenTypes.STRUCT,
            "switch" to MoxieTokenTypes.SWITCH,
            "type" to MoxieTokenTypes.TYPE,
            "var" to MoxieTokenTypes.VAR,

            // Built-in functions
            "append" to MoxieTokenTypes.APPEND,
            "cap" to MoxieTokenTypes.CAP,
            "clear" to MoxieTokenTypes.CLEAR,
            "close" to MoxieTokenTypes.CLOSE,
            "copy" to MoxieTokenTypes.COPY,
            "delete" to MoxieTokenTypes.DELETE,
            "len" to MoxieTokenTypes.LEN,
            "make" to MoxieTokenTypes.MAKE,
            "panic" to MoxieTokenTypes.PANIC,
            "print" to MoxieTokenTypes.PRINT,
            "println" to MoxieTokenTypes.PRINTLN,
            "recover" to MoxieTokenTypes.RECOVER,
            "spawn" to MoxieTokenTypes.SPAWN,

            // Built-in types
            "bool" to MoxieTokenTypes.BOOL,
            "byte" to MoxieTokenTypes.BYTE,
            "error" to MoxieTokenTypes.ERROR,
            "float32" to MoxieTokenTypes.FLOAT32,
            "float64" to MoxieTokenTypes.FLOAT64,
            "int" to MoxieTokenTypes.INT,
            "int8" to MoxieTokenTypes.INT8,
            "int16" to MoxieTokenTypes.INT16,
            "int32" to MoxieTokenTypes.INT32,
            "int64" to MoxieTokenTypes.INT64,
            "rune" to MoxieTokenTypes.RUNE,
            "string" to MoxieTokenTypes.STRING,
            "uint" to MoxieTokenTypes.UINT,
            "uint8" to MoxieTokenTypes.UINT8,
            "uint16" to MoxieTokenTypes.UINT16,
            "uint32" to MoxieTokenTypes.UINT32,
            "uint64" to MoxieTokenTypes.UINT64,
            "any" to MoxieTokenTypes.ANY,
            "comparable" to MoxieTokenTypes.COMPARABLE,

            // Literals
            "true" to MoxieTokenTypes.TRUE,
            "false" to MoxieTokenTypes.FALSE,
            "nil" to MoxieTokenTypes.NIL,
            "iota" to MoxieTokenTypes.IOTA
        )
    }

    override fun start(buffer: CharSequence, startOffset: Int, endOffset: Int, initialState: Int) {
        this.buffer = buffer
        this.startOffset = startOffset
        this.endOffset = endOffset
        this.state = initialState
        this.tokenStart = startOffset
        this.tokenEnd = startOffset
        advance()
    }

    override fun getState(): Int = state

    override fun getTokenType(): IElementType? = tokenType

    override fun getTokenStart(): Int = tokenStart

    override fun getTokenEnd(): Int = tokenEnd

    override fun advance() {
        tokenStart = tokenEnd
        if (tokenEnd >= endOffset) {
            tokenType = null
            return
        }

        val c = buffer[tokenEnd]

        when {
            // Whitespace
            c.isWhitespace() -> {
                tokenEnd++
                while (tokenEnd < endOffset && buffer[tokenEnd].isWhitespace()) {
                    tokenEnd++
                }
                tokenType = MoxieTokenTypes.WHITE_SPACE
            }

            // Line comment
            c == '/' && tokenEnd + 1 < endOffset && buffer[tokenEnd + 1] == '/' -> {
                tokenEnd += 2
                while (tokenEnd < endOffset && buffer[tokenEnd] != '\n') {
                    tokenEnd++
                }
                tokenType = MoxieTokenTypes.LINE_COMMENT
            }

            // Block comment
            c == '/' && tokenEnd + 1 < endOffset && buffer[tokenEnd + 1] == '*' -> {
                tokenEnd += 2
                while (tokenEnd + 1 < endOffset) {
                    if (buffer[tokenEnd] == '*' && buffer[tokenEnd + 1] == '/') {
                        tokenEnd += 2
                        break
                    }
                    tokenEnd++
                }
                tokenType = MoxieTokenTypes.BLOCK_COMMENT
            }

            // String literals
            c == '"' -> {
                tokenEnd++
                while (tokenEnd < endOffset) {
                    val ch = buffer[tokenEnd]
                    if (ch == '"') {
                        tokenEnd++
                        break
                    }
                    if (ch == '\\' && tokenEnd + 1 < endOffset) {
                        tokenEnd += 2
                    } else {
                        tokenEnd++
                    }
                }
                tokenType = MoxieTokenTypes.STRING_LITERAL
            }

            // Raw string literals
            c == '`' -> {
                tokenEnd++
                while (tokenEnd < endOffset && buffer[tokenEnd] != '`') {
                    tokenEnd++
                }
                if (tokenEnd < endOffset) tokenEnd++
                tokenType = MoxieTokenTypes.RAW_STRING_LITERAL
            }

            // Char literals
            c == '\'' -> {
                tokenEnd++
                while (tokenEnd < endOffset) {
                    val ch = buffer[tokenEnd]
                    if (ch == '\'') {
                        tokenEnd++
                        break
                    }
                    if (ch == '\\' && tokenEnd + 1 < endOffset) {
                        tokenEnd += 2
                    } else {
                        tokenEnd++
                    }
                }
                tokenType = MoxieTokenTypes.CHAR_LITERAL
            }

            // Numbers
            c.isDigit() -> {
                tokenEnd++
                // Hex, octal, binary
                if (c == '0' && tokenEnd < endOffset) {
                    val next = buffer[tokenEnd]
                    if (next == 'x' || next == 'X' || next == 'o' || next == 'O' || next == 'b' || next == 'B') {
                        tokenEnd++
                    }
                }
                while (tokenEnd < endOffset) {
                    val ch = buffer[tokenEnd]
                    if (!ch.isLetterOrDigit() && ch != '.' && ch != '_') break
                    tokenEnd++
                }
                // Check for imaginary
                if (tokenEnd < endOffset && buffer[tokenEnd] == 'i') {
                    tokenEnd++
                    tokenType = MoxieTokenTypes.IMAGINARY_LITERAL
                } else if (buffer.substring(tokenStart, tokenEnd).contains('.')) {
                    tokenType = MoxieTokenTypes.FLOAT_LITERAL
                } else {
                    tokenType = MoxieTokenTypes.INT_LITERAL
                }
            }

            // Identifiers and keywords
            c.isLetter() || c == '_' -> {
                tokenEnd++
                while (tokenEnd < endOffset) {
                    val ch = buffer[tokenEnd]
                    if (!ch.isLetterOrDigit() && ch != '_') break
                    tokenEnd++
                }
                val text = buffer.substring(tokenStart, tokenEnd)
                tokenType = KEYWORDS[text] ?: MoxieTokenTypes.IDENTIFIER
            }

            // Operators and punctuation
            else -> {
                tokenType = when {
                    matchSequence("...") -> MoxieTokenTypes.ELLIPSIS
                    matchSequence(":=") -> MoxieTokenTypes.DEFINE
                    matchSequence("<<") -> MoxieTokenTypes.SHL
                    matchSequence(">>") -> MoxieTokenTypes.SHR
                    matchSequence("&&") -> MoxieTokenTypes.LAND
                    matchSequence("||") -> MoxieTokenTypes.LOR
                    matchSequence("<-") -> MoxieTokenTypes.ARROW
                    matchSequence("++") -> MoxieTokenTypes.INC
                    matchSequence("--") -> MoxieTokenTypes.DEC
                    matchSequence("==") -> MoxieTokenTypes.EQ
                    matchSequence("!=") -> MoxieTokenTypes.NE
                    matchSequence("<=") -> MoxieTokenTypes.LE
                    matchSequence(">=") -> MoxieTokenTypes.GE
                    matchSequence("+=") -> MoxieTokenTypes.PLUS_ASSIGN
                    matchSequence("-=") -> MoxieTokenTypes.MINUS_ASSIGN
                    matchSequence("*=") -> MoxieTokenTypes.MUL_ASSIGN
                    matchSequence("/=") -> MoxieTokenTypes.DIV_ASSIGN
                    matchSequence("%=") -> MoxieTokenTypes.MOD_ASSIGN
                    matchSequence("&=") -> MoxieTokenTypes.AND_ASSIGN
                    matchSequence("|=") -> MoxieTokenTypes.OR_ASSIGN
                    matchSequence("^=") -> MoxieTokenTypes.XOR_ASSIGN
                    matchSequence("<<=") -> MoxieTokenTypes.SHL_ASSIGN
                    matchSequence(">>=") -> MoxieTokenTypes.SHR_ASSIGN
                    matchSequence("&^=") -> MoxieTokenTypes.AND_NOT_ASSIGN
                    matchSequence("&^") -> MoxieTokenTypes.AND_NOT
                    c == '+' -> { tokenEnd++; MoxieTokenTypes.PLUS }
                    c == '-' -> { tokenEnd++; MoxieTokenTypes.MINUS }
                    c == '*' -> { tokenEnd++; MoxieTokenTypes.STAR }
                    c == '/' -> { tokenEnd++; MoxieTokenTypes.DIV }
                    c == '%' -> { tokenEnd++; MoxieTokenTypes.MOD }
                    c == '&' -> { tokenEnd++; MoxieTokenTypes.AND }
                    c == '|' -> { tokenEnd++; MoxieTokenTypes.OR }
                    c == '^' -> { tokenEnd++; MoxieTokenTypes.XOR }
                    c == '<' -> { tokenEnd++; MoxieTokenTypes.LT }
                    c == '>' -> { tokenEnd++; MoxieTokenTypes.GT }
                    c == '=' -> { tokenEnd++; MoxieTokenTypes.ASSIGN }
                    c == '!' -> { tokenEnd++; MoxieTokenTypes.NOT }
                    c == '(' -> { tokenEnd++; MoxieTokenTypes.LPAREN }
                    c == ')' -> { tokenEnd++; MoxieTokenTypes.RPAREN }
                    c == '[' -> { tokenEnd++; MoxieTokenTypes.LBRACKET }
                    c == ']' -> { tokenEnd++; MoxieTokenTypes.RBRACKET }
                    c == '{' -> { tokenEnd++; MoxieTokenTypes.LBRACE }
                    c == '}' -> { tokenEnd++; MoxieTokenTypes.RBRACE }
                    c == ',' -> { tokenEnd++; MoxieTokenTypes.COMMA }
                    c == '.' -> { tokenEnd++; MoxieTokenTypes.DOT }
                    c == ';' -> { tokenEnd++; MoxieTokenTypes.SEMICOLON }
                    c == ':' -> { tokenEnd++; MoxieTokenTypes.COLON }
                    else -> { tokenEnd++; MoxieTokenTypes.BAD_CHARACTER }
                }
            }
        }
    }

    private fun matchSequence(seq: String): Boolean {
        if (tokenEnd + seq.length > endOffset) return false
        for (i in seq.indices) {
            if (buffer[tokenEnd + i] != seq[i]) return false
        }
        tokenEnd += seq.length
        return true
    }

    override fun getBufferSequence(): CharSequence = buffer

    override fun getBufferEnd(): Int = endOffset
}
