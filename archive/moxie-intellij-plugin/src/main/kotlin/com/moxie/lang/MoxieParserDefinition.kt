package com.moxie.lang

import com.intellij.lang.ASTNode
import com.intellij.lang.ParserDefinition
import com.intellij.lang.PsiParser
import com.intellij.lexer.Lexer
import com.intellij.openapi.project.Project
import com.intellij.psi.FileViewProvider
import com.intellij.psi.PsiElement
import com.intellij.psi.PsiFile
import com.intellij.psi.tree.IFileElementType
import com.intellij.psi.tree.TokenSet

class MoxieParserDefinition : ParserDefinition {

    companion object {
        val FILE = IFileElementType(MoxieLanguage.INSTANCE)

        val COMMENTS = TokenSet.create(
            MoxieTokenTypes.LINE_COMMENT,
            MoxieTokenTypes.BLOCK_COMMENT
        )

        val STRINGS = TokenSet.create(
            MoxieTokenTypes.STRING_LITERAL,
            MoxieTokenTypes.RAW_STRING_LITERAL,
            MoxieTokenTypes.CHAR_LITERAL
        )

        val WHITESPACES = TokenSet.create(
            MoxieTokenTypes.WHITE_SPACE
        )
    }

    override fun createLexer(project: Project?): Lexer = MoxieLexer()

    override fun createParser(project: Project?): PsiParser = MoxieParser()

    override fun getFileNodeType(): IFileElementType = FILE

    override fun getCommentTokens(): TokenSet = COMMENTS

    override fun getStringLiteralElements(): TokenSet = STRINGS

    override fun createElement(node: ASTNode?): PsiElement = MoxiePsiElement(node!!)

    override fun createFile(viewProvider: FileViewProvider): PsiFile = MoxiePsiFile(viewProvider)

    override fun getWhitespaceTokens(): TokenSet = WHITESPACES
}
