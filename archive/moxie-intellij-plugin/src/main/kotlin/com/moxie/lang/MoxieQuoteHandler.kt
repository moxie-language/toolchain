package com.moxie.lang

import com.intellij.codeInsight.editorActions.SimpleTokenSetQuoteHandler

class MoxieQuoteHandler : SimpleTokenSetQuoteHandler(
    MoxieTokenTypes.STRING_LITERAL,
    MoxieTokenTypes.RAW_STRING_LITERAL,
    MoxieTokenTypes.CHAR_LITERAL
)
