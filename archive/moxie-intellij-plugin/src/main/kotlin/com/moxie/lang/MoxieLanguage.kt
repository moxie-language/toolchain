package com.moxie.lang

import com.intellij.lang.Language

class MoxieLanguage private constructor() : Language("Moxie") {
    companion object {
        @JvmStatic
        val INSTANCE = MoxieLanguage()
    }

    override fun isCaseSensitive(): Boolean = true
    override fun getDisplayName(): String = "Moxie"
}
