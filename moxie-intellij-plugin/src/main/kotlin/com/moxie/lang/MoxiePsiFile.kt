package com.moxie.lang

import com.intellij.extapi.psi.PsiFileBase
import com.intellij.openapi.fileTypes.FileType
import com.intellij.psi.FileViewProvider

class MoxiePsiFile(viewProvider: FileViewProvider) : PsiFileBase(viewProvider, MoxieLanguage.INSTANCE) {
    override fun getFileType(): FileType = MoxieFileType.INSTANCE
    override fun toString(): String = "Moxie File"
}
