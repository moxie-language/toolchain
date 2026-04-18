package com.moxie.lang

import com.intellij.lang.ASTNode
import com.intellij.lang.PsiBuilder
import com.intellij.lang.PsiParser
import com.intellij.psi.tree.IElementType

class MoxieParser : PsiParser {
    override fun parse(root: IElementType, builder: PsiBuilder): ASTNode {
        val marker = builder.mark()

        while (!builder.eof()) {
            builder.advanceLexer()
        }

        marker.done(root)
        return builder.treeBuilt
    }
}
