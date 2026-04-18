package com.moxie.lang

import com.intellij.lang.ASTNode
import com.intellij.lang.folding.FoldingBuilder
import com.intellij.lang.folding.FoldingDescriptor
import com.intellij.openapi.editor.Document
import com.intellij.openapi.util.TextRange
import com.intellij.psi.PsiElement
import com.intellij.psi.util.PsiTreeUtil

class MoxieFoldingBuilder : FoldingBuilder {

    override fun buildFoldRegions(node: ASTNode, document: Document): Array<FoldingDescriptor> {
        val descriptors = mutableListOf<FoldingDescriptor>()
        collectFoldingDescriptors(node, descriptors)
        return descriptors.toTypedArray()
    }

    private fun collectFoldingDescriptors(node: ASTNode, descriptors: MutableList<FoldingDescriptor>) {
        val type = node.elementType

        if (type == MoxieTokenTypes.BLOCK_COMMENT) {
            val range = node.textRange
            if (range.length > 2) {
                descriptors.add(FoldingDescriptor(node, range))
            }
        } else if (type == MoxieTokenTypes.LBRACE) {
            // Find matching closing brace
            var current: ASTNode? = node.treeNext
            var braceCount = 1
            while (current != null && braceCount > 0) {
                when (current.elementType) {
                    MoxieTokenTypes.LBRACE -> braceCount++
                    MoxieTokenTypes.RBRACE -> braceCount--
                }
                if (braceCount == 0) {
                    val range = TextRange(node.startOffset, current.startOffset + current.textLength)
                    if (range.length > 2) {
                        descriptors.add(FoldingDescriptor(node, range))
                    }
                    break
                }
                current = current.treeNext
            }
        }

        // Recurse to children
        var child = node.firstChildNode
        while (child != null) {
            collectFoldingDescriptors(child, descriptors)
            child = child.treeNext
        }
    }

    override fun getPlaceholderText(node: ASTNode): String? {
        return when (node.elementType) {
            MoxieTokenTypes.BLOCK_COMMENT -> "/*...*/"
            MoxieTokenTypes.LBRACE -> "{...}"
            else -> "..."
        }
    }

    override fun isCollapsedByDefault(node: ASTNode): Boolean = false
}
