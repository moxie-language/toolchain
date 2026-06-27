package com.moxie.lang

import com.intellij.lang.BracePair
import com.intellij.lang.PairedBraceMatcher
import com.intellij.psi.PsiFile
import com.intellij.psi.tree.IElementType

class MoxieBraceMatcher : PairedBraceMatcher {
    companion object {
        private val PAIRS = arrayOf(
            BracePair(MoxieTokenTypes.LPAREN, MoxieTokenTypes.RPAREN, false),
            BracePair(MoxieTokenTypes.LBRACE, MoxieTokenTypes.RBRACE, true),
            BracePair(MoxieTokenTypes.LBRACKET, MoxieTokenTypes.RBRACKET, false),
        )
    }

    override fun getPairs(): Array<BracePair> = PAIRS

    override fun isPairedBracesAllowedBeforeType(
        lbraceType: IElementType,
        contextType: IElementType?
    ): Boolean = true

    override fun getCodeConstructStart(file: PsiFile?, openingBraceOffset: Int): Int = openingBraceOffset
}
