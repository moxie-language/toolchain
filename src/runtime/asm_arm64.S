#ifdef __MACH__
.global _moxie_longjmp
_moxie_longjmp:
#else
.global moxie_longjmp
moxie_longjmp:
#endif
    // Note: the code we jump to assumes x0 is set to a non-zero value if we
    // jump from here (which is conveniently already the case).
    ldp x1, x2, [x0] // jumpSP, jumpPC
    mov sp, x1
    br  x2
