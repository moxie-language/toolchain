// Syscall6 for Linux amd64.
//
// func Syscall6(num, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2, errno uintptr)
//
// LLVM calling convention ({i64,i64,i64} return in registers):
//   Args:    rdi=num  rsi=a1  rdx=a2  rcx=a3  r8=a4  r9=a5  [rsp+8]=a6
//   Returns: rax=r1   rdx=r2  rcx=errno
//
// Linux syscall convention:
//   rax=num  rdi=a1  rsi=a2  rdx=a3  r10=a4  r8=a5  r9=a6
//   Returns: rax  (clobbers rcx, r11)

.text
.globl "internal/runtime/syscall.Syscall6"
.type "internal/runtime/syscall.Syscall6", @function
"internal/runtime/syscall.Syscall6":
    movq    %rdi, %rax          // num -> rax
    movq    %rsi, %rdi          // a1 -> rdi
    movq    %rdx, %rsi          // a2 -> rsi
    movq    %rcx, %rdx          // a3 -> rdx
    movq    %r8, %r10           // a4 -> r10
    movq    %r9, %r8            // a5 -> r8
    movq    8(%rsp), %r9        // a6 -> r9
    syscall
    cmpq    $-4095, %rax
    jb      .Lok_amd64
    negq    %rax
    movq    %rax, %rcx          // errno (positive)
    movq    $-1, %rax           // r1 = -1
    xorq    %rdx, %rdx          // r2 = 0
    ret
.Lok_amd64:
    xorq    %rcx, %rcx          // errno = 0
    ret
.size "internal/runtime/syscall.Syscall6", . - "internal/runtime/syscall.Syscall6"

.section ".note.GNU-stack","",@progbits
