package com.moxie.lang

import com.intellij.lexer.Lexer
import com.intellij.openapi.editor.DefaultLanguageHighlighterColors
import com.intellij.openapi.editor.HighlighterColors
import com.intellij.openapi.editor.colors.TextAttributesKey
import com.intellij.openapi.fileTypes.SyntaxHighlighterBase
import com.intellij.psi.tree.IElementType

class MoxieSyntaxHighlighter : SyntaxHighlighterBase() {

    companion object {
        val KEYWORD = TextAttributesKey.createTextAttributesKey(
            "MOXIE_KEYWORD",
            DefaultLanguageHighlighterColors.KEYWORD
        )

        val BUILTIN_FUNCTION = TextAttributesKey.createTextAttributesKey(
            "MOXIE_BUILTIN_FUNCTION",
            DefaultLanguageHighlighterColors.PREDEFINED_SYMBOL
        )

        val BUILTIN_TYPE = TextAttributesKey.createTextAttributesKey(
            "MOXIE_BUILTIN_TYPE",
            DefaultLanguageHighlighterColors.PREDEFINED_SYMBOL
        )

        val NUMBER = TextAttributesKey.createTextAttributesKey(
            "MOXIE_NUMBER",
            DefaultLanguageHighlighterColors.NUMBER
        )

        val STRING = TextAttributesKey.createTextAttributesKey(
            "MOXIE_STRING",
            DefaultLanguageHighlighterColors.STRING
        )

        val LINE_COMMENT = TextAttributesKey.createTextAttributesKey(
            "MOXIE_LINE_COMMENT",
            DefaultLanguageHighlighterColors.LINE_COMMENT
        )

        val BLOCK_COMMENT = TextAttributesKey.createTextAttributesKey(
            "MOXIE_BLOCK_COMMENT",
            DefaultLanguageHighlighterColors.BLOCK_COMMENT
        )

        val OPERATOR = TextAttributesKey.createTextAttributesKey(
            "MOXIE_OPERATOR",
            DefaultLanguageHighlighterColors.OPERATION_SIGN
        )

        val PARENTHESES = TextAttributesKey.createTextAttributesKey(
            "MOXIE_PARENTHESES",
            DefaultLanguageHighlighterColors.PARENTHESES
        )

        val BRACES = TextAttributesKey.createTextAttributesKey(
            "MOXIE_BRACES",
            DefaultLanguageHighlighterColors.BRACES
        )

        val BRACKETS = TextAttributesKey.createTextAttributesKey(
            "MOXIE_BRACKETS",
            DefaultLanguageHighlighterColors.BRACKETS
        )

        val COMMA = TextAttributesKey.createTextAttributesKey(
            "MOXIE_COMMA",
            DefaultLanguageHighlighterColors.COMMA
        )

        val DOT = TextAttributesKey.createTextAttributesKey(
            "MOXIE_DOT",
            DefaultLanguageHighlighterColors.DOT
        )

        val SEMICOLON = TextAttributesKey.createTextAttributesKey(
            "MOXIE_SEMICOLON",
            DefaultLanguageHighlighterColors.SEMICOLON
        )

        val IDENTIFIER = TextAttributesKey.createTextAttributesKey(
            "MOXIE_IDENTIFIER",
            DefaultLanguageHighlighterColors.IDENTIFIER
        )

        val BAD_CHARACTER = TextAttributesKey.createTextAttributesKey(
            "MOXIE_BAD_CHARACTER",
            HighlighterColors.BAD_CHARACTER
        )

        private val KEYWORD_KEYS = arrayOf(KEYWORD)
        private val BUILTIN_FUNCTION_KEYS = arrayOf(BUILTIN_FUNCTION)
        private val BUILTIN_TYPE_KEYS = arrayOf(BUILTIN_TYPE)
        private val NUMBER_KEYS = arrayOf(NUMBER)
        private val STRING_KEYS = arrayOf(STRING)
        private val LINE_COMMENT_KEYS = arrayOf(LINE_COMMENT)
        private val BLOCK_COMMENT_KEYS = arrayOf(BLOCK_COMMENT)
        private val OPERATOR_KEYS = arrayOf(OPERATOR)
        private val PARENTHESES_KEYS = arrayOf(PARENTHESES)
        private val BRACES_KEYS = arrayOf(BRACES)
        private val BRACKETS_KEYS = arrayOf(BRACKETS)
        private val COMMA_KEYS = arrayOf(COMMA)
        private val DOT_KEYS = arrayOf(DOT)
        private val SEMICOLON_KEYS = arrayOf(SEMICOLON)
        private val IDENTIFIER_KEYS = arrayOf(IDENTIFIER)
        private val BAD_CHARACTER_KEYS = arrayOf(BAD_CHARACTER)
        private val EMPTY_KEYS = emptyArray<TextAttributesKey>()
    }

    override fun getHighlightingLexer(): Lexer = MoxieLexer()

    override fun getTokenHighlights(tokenType: IElementType?): Array<TextAttributesKey> {
        return when (tokenType) {
            // Keywords
            MoxieTokenTypes.BREAK, MoxieTokenTypes.CASE, MoxieTokenTypes.CHAN,
            MoxieTokenTypes.CONST, MoxieTokenTypes.CONTINUE, MoxieTokenTypes.DEFAULT,
            MoxieTokenTypes.DEFER, MoxieTokenTypes.ELSE, MoxieTokenTypes.FOR,
            MoxieTokenTypes.FUNC, MoxieTokenTypes.GOTO, MoxieTokenTypes.IF,
            MoxieTokenTypes.IMPORT, MoxieTokenTypes.INTERFACE, MoxieTokenTypes.MAP,
            MoxieTokenTypes.PACKAGE, MoxieTokenTypes.RANGE, MoxieTokenTypes.RETURN,
            MoxieTokenTypes.SELECT, MoxieTokenTypes.STRUCT, MoxieTokenTypes.SWITCH,
            MoxieTokenTypes.TYPE, MoxieTokenTypes.VAR -> KEYWORD_KEYS

            // Built-in functions
            MoxieTokenTypes.PUSH, MoxieTokenTypes.POP, MoxieTokenTypes.RESIZE,
            MoxieTokenTypes.CAP, MoxieTokenTypes.CLEAR,
            MoxieTokenTypes.CLOSE, MoxieTokenTypes.COPY, MoxieTokenTypes.DELETE,
            MoxieTokenTypes.LEN, MoxieTokenTypes.MAX, MoxieTokenTypes.MIN,
            MoxieTokenTypes.PANIC, MoxieTokenTypes.PRINT, MoxieTokenTypes.PRINTLN,
            MoxieTokenTypes.RECOVER, MoxieTokenTypes.SPAWN -> BUILTIN_FUNCTION_KEYS

            // Built-in types
            MoxieTokenTypes.BOOL, MoxieTokenTypes.BYTE, MoxieTokenTypes.ERROR,
            MoxieTokenTypes.FLOAT32, MoxieTokenTypes.FLOAT64,
            MoxieTokenTypes.INT8, MoxieTokenTypes.INT16, MoxieTokenTypes.INT32,
            MoxieTokenTypes.INT64, MoxieTokenTypes.RUNE, MoxieTokenTypes.STRING,
            MoxieTokenTypes.UINT8, MoxieTokenTypes.UINT16,
            MoxieTokenTypes.UINT32, MoxieTokenTypes.UINT64,
            MoxieTokenTypes.COMPARABLE, MoxieTokenTypes.TRUE, MoxieTokenTypes.FALSE,
            MoxieTokenTypes.NIL, MoxieTokenTypes.IOTA -> BUILTIN_TYPE_KEYS

            // Numbers
            MoxieTokenTypes.INT_LITERAL, MoxieTokenTypes.FLOAT_LITERAL,
            MoxieTokenTypes.IMAGINARY_LITERAL -> NUMBER_KEYS

            // Strings
            MoxieTokenTypes.STRING_LITERAL, MoxieTokenTypes.RAW_STRING_LITERAL,
            MoxieTokenTypes.CHAR_LITERAL -> STRING_KEYS

            // Comments
            MoxieTokenTypes.LINE_COMMENT -> LINE_COMMENT_KEYS
            MoxieTokenTypes.BLOCK_COMMENT -> BLOCK_COMMENT_KEYS

            // Operators
            MoxieTokenTypes.PLUS, MoxieTokenTypes.MINUS, MoxieTokenTypes.STAR,
            MoxieTokenTypes.DIV, MoxieTokenTypes.MOD, MoxieTokenTypes.AND,
            MoxieTokenTypes.OR, MoxieTokenTypes.XOR, MoxieTokenTypes.SHL,
            MoxieTokenTypes.SHR, MoxieTokenTypes.AND_NOT, MoxieTokenTypes.PLUS_ASSIGN,
            MoxieTokenTypes.MINUS_ASSIGN, MoxieTokenTypes.MUL_ASSIGN,
            MoxieTokenTypes.DIV_ASSIGN, MoxieTokenTypes.MOD_ASSIGN,
            MoxieTokenTypes.AND_ASSIGN, MoxieTokenTypes.OR_ASSIGN,
            MoxieTokenTypes.XOR_ASSIGN, MoxieTokenTypes.SHL_ASSIGN,
            MoxieTokenTypes.SHR_ASSIGN, MoxieTokenTypes.AND_NOT_ASSIGN,
            MoxieTokenTypes.LAND, MoxieTokenTypes.LOR, MoxieTokenTypes.ARROW,
            MoxieTokenTypes.INC, MoxieTokenTypes.DEC, MoxieTokenTypes.EQ,
            MoxieTokenTypes.LT, MoxieTokenTypes.GT, MoxieTokenTypes.ASSIGN,
            MoxieTokenTypes.NOT, MoxieTokenTypes.NE, MoxieTokenTypes.LE,
            MoxieTokenTypes.GE, MoxieTokenTypes.DEFINE, MoxieTokenTypes.ELLIPSIS -> OPERATOR_KEYS

            // Delimiters
            MoxieTokenTypes.LPAREN, MoxieTokenTypes.RPAREN -> PARENTHESES_KEYS
            MoxieTokenTypes.LBRACE, MoxieTokenTypes.RBRACE -> BRACES_KEYS
            MoxieTokenTypes.LBRACKET, MoxieTokenTypes.RBRACKET -> BRACKETS_KEYS
            MoxieTokenTypes.COMMA -> COMMA_KEYS
            MoxieTokenTypes.DOT -> DOT_KEYS
            MoxieTokenTypes.SEMICOLON, MoxieTokenTypes.COLON -> SEMICOLON_KEYS

            // Identifier
            MoxieTokenTypes.IDENTIFIER -> IDENTIFIER_KEYS

            // Bad character
            MoxieTokenTypes.BAD_CHARACTER -> BAD_CHARACTER_KEYS

            else -> EMPTY_KEYS
        }
    }
}
