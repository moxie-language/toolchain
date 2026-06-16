#ifdef __ELF__
.section .text.moxie_longjmp
.global moxie_longjmp
moxie_longjmp:
#else // Darwin
.global _moxie_longjmp
_moxie_longjmp:
#endif
    // Note: the code we jump to assumes rax is set to a non-zero value if we
    // jump from here, so we use rax as the temporary value for jumpPC.
    movq 0(%rdi), %rsp // jumpSP
    movq 8(%rdi), %rax // jumpPC
    jmpq *%rax


#ifdef __MACH__ // Darwin
// allow these symbols to stripped as dead code
.subsections_via_symbols
#endif
