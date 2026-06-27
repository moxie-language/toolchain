package com.moxie.lang

import com.intellij.openapi.editor.colors.TextAttributesKey
import com.intellij.openapi.fileTypes.SyntaxHighlighter
import com.intellij.openapi.options.colors.AttributesDescriptor
import com.intellij.openapi.options.colors.ColorDescriptor
import com.intellij.openapi.options.colors.ColorSettingsPage
import javax.swing.Icon

class MoxieColorSettingsPage : ColorSettingsPage {

    companion object {
        private val DESCRIPTORS = arrayOf(
            AttributesDescriptor("Keyword", MoxieSyntaxHighlighter.KEYWORD),
            AttributesDescriptor("Built-in Function", MoxieSyntaxHighlighter.BUILTIN_FUNCTION),
            AttributesDescriptor("Built-in Type", MoxieSyntaxHighlighter.BUILTIN_TYPE),
            AttributesDescriptor("Number", MoxieSyntaxHighlighter.NUMBER),
            AttributesDescriptor("String", MoxieSyntaxHighlighter.STRING),
            AttributesDescriptor("Line Comment", MoxieSyntaxHighlighter.LINE_COMMENT),
            AttributesDescriptor("Block Comment", MoxieSyntaxHighlighter.BLOCK_COMMENT),
            AttributesDescriptor("Operator", MoxieSyntaxHighlighter.OPERATOR),
            AttributesDescriptor("Parentheses", MoxieSyntaxHighlighter.PARENTHESES),
            AttributesDescriptor("Braces", MoxieSyntaxHighlighter.BRACES),
            AttributesDescriptor("Brackets", MoxieSyntaxHighlighter.BRACKETS),
            AttributesDescriptor("Comma", MoxieSyntaxHighlighter.COMMA),
            AttributesDescriptor("Dot", MoxieSyntaxHighlighter.DOT),
            AttributesDescriptor("Semicolon", MoxieSyntaxHighlighter.SEMICOLON),
            AttributesDescriptor("Identifier", MoxieSyntaxHighlighter.IDENTIFIER),
        )

        private val DEMO_TEXT = """
package main

import "moxie"

// Moxie: string and []byte are the same type.
// `int` and `uint` are always 32-bit. No goroutines, no new(),
// no complex numbers, no uintptr. Use literal syntax for slices,
// channels, and maps. Concat text with | (pipe), not +.

const MaxWorkers = 4

type Job struct {
    id   int32
    name string
}

func worker(j moxie.Int32, results chan moxie.Int32) {
    results <- moxie.Int32(int32(j) * int32(j))
}

func main() {
    // Slice, channel, and map literal syntax.
    buf := []byte{:1024}        // len 1024
    queue := []int32{:0:100}    // len 0, cap 100
    ch := chan moxie.Int32{}    // unbuffered
    config := map[string]int32{
        "timeout":    30,
        "maxRetries": 3,
    }

    // Text concatenation: | not +.
    greeting := "hello " | "moxie" | "!"

    // Fan out via spawn (new domain per worker).
    results := chan moxie.Int32{}
    for i := int32(0); i < MaxWorkers; i++ {
        spawn(worker, moxie.Int32(i), results)
    }

    // Event loop with select.
    for n := 0; n < MaxWorkers; n++ {
        select {
        case r := <-results:
            println(int32(r))
        case <-ch:
            return
        }
    }

    /*
       Block comment.
       Range over text yields bytes, not runes.
    */
    for i, b := range greeting {
        if b == ' ' {
            clear(buf[i:])
            break
        }
    }

    push(queue, 1, 2, 3)
    last := pop(queue)
    resize(queue, 0)
    _ = config
    _ = last
}
""".trimIndent()
    }

    override fun getIcon(): Icon? = MoxieIcons.FILE

    override fun getHighlighter(): SyntaxHighlighter = MoxieSyntaxHighlighter()

    override fun getDemoText(): String = DEMO_TEXT

    override fun getAdditionalHighlightingTagToDescriptorMap(): Map<String, TextAttributesKey>? = null

    override fun getAttributeDescriptors(): Array<AttributesDescriptor> = DESCRIPTORS

    override fun getColorDescriptors(): Array<ColorDescriptor> = ColorDescriptor.EMPTY_ARRAY

    override fun getDisplayName(): String = "Moxie"
}
