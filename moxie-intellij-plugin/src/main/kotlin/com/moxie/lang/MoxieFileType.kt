package com.moxie.lang

import com.intellij.openapi.fileTypes.LanguageFileType
import javax.swing.Icon

class MoxieFileType private constructor() : LanguageFileType(MoxieLanguage.INSTANCE) {

    override fun getName(): String = "Moxie File"

    override fun getDescription(): String = "Moxie language file"

    override fun getDefaultExtension(): String = "mx"

    override fun getIcon(): Icon? = MoxieIcons.FILE

    companion object {
        @JvmStatic
        val INSTANCE = MoxieFileType()
    }
}
