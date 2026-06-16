// Syscall6 for Linux arm64.
//
// func Syscall6(num, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2, errno uintptr)
//
// LLVM calling convention ({i64,i64,i64} return in registers):
//   Args:    x0=num  x1=a1  x2=a2  x3=a3  x4=a4  x5=a5  x6=a6  x7=ctx
//   Returns: x0=r1   x1=r2  x2=errno
//
// Linux arm64 syscall convention:
//   x8=num  x0-x5=args
//   Returns: x0  (x1 for dual-return syscalls)

.text
.globl "internal/runtime/syscall.Syscall6"
.type "internal/runtime/syscall.Syscall6", %function
"internal/runtime/syscall.Syscall6":
    mov     x8, x0              // num -> x8
    mov     x0, x1              // a1 -> x0
    mov     x1, x2              // a2 -> x1
    mov     x2, x3              // a3 -> x2
    mov     x3, x4              // a4 -> x3
    mov     x4, x5              // a5 -> x4
    mov     x5, x6              // a6 -> x5
    svc     #0
    cmn     x0, #4095
    b.lo    .Lok_arm64
    neg     x2, x0              // errno (positive)
    mov     x0, #-1             // r1 = -1
    mov     x1, xzr             // r2 = 0
    ret
.Lok_arm64:
    mov     x2, xzr             // errno = 0
    ret
.size "internal/runtime/syscall.Syscall6", . - "internal/runtime/syscall.Syscall6"

.section ".note.GNU-stack","",@progbits
