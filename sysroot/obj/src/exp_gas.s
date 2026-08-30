// GAS port of src/math/exp_amd64.s (non-FMA path only).
    .text

    .global math.archExp
    .type math.archExp,@function
math.archExp:
    movq    8(%rsp), %rbx            // x
    movabsq $0x7FFFFFFFFFFFFFFF, %rax
    movq    %rbx, %rdx
    andq    %rax, %rdx
    movabsq $0x7FF0000000000000, %rax // +Inf
    cmpq    %rax, %rdx
    jle     .LexpNotFinite
    movq    %rbx, %xmm0
    movsd   .LexpOverflow(%rip), %xmm1
    comisd  %xmm1, %xmm0
    ja      .LexpOverflowOut
    movsd   .LexpLog2e(%rip), %xmm1
    mulsd   %xmm0, %xmm1
    cvttsd2si %xmm1, %ebx
    cvtsi2sdl %ebx, %xmm1
    movsd   .LexpLn2u(%rip), %xmm2
    mulsd   %xmm1, %xmm2
    subsd   %xmm2, %xmm0
    movsd   .LexpLn2l(%rip), %xmm2
    mulsd   %xmm1, %xmm2
    subsd   %xmm2, %xmm0
    mulsd   .Lexp0625(%rip), %xmm0
    movsd   .LexpData+64(%rip), %xmm1
    mulsd   %xmm0, %xmm1
    addsd   .LexpData+56(%rip), %xmm1
    mulsd   %xmm0, %xmm1
    addsd   .LexpData+48(%rip), %xmm1
    mulsd   %xmm0, %xmm1
    addsd   .LexpData+40(%rip), %xmm1
    mulsd   %xmm0, %xmm1
    addsd   .LexpData+32(%rip), %xmm1
    mulsd   %xmm0, %xmm1
    addsd   .LexpData+24(%rip), %xmm1
    mulsd   %xmm0, %xmm1
    addsd   .LexpData+0(%rip), %xmm1
    mulsd   %xmm0, %xmm1
    addsd   .LexpData+8(%rip), %xmm1
    mulsd   %xmm1, %xmm0
    movsd   .LexpData+16(%rip), %xmm1
    addsd   %xmm0, %xmm1
    mulsd   %xmm1, %xmm0
    movsd   .LexpData+16(%rip), %xmm1
    addsd   %xmm0, %xmm1
    mulsd   %xmm1, %xmm0
    movsd   .LexpData+16(%rip), %xmm1
    addsd   %xmm0, %xmm1
    mulsd   %xmm1, %xmm0
    movsd   .LexpData+16(%rip), %xmm1
    addsd   %xmm0, %xmm1
    mulsd   %xmm1, %xmm0
    addsd   .LexpData+8(%rip), %xmm0
.LexpLdexp:
    addl    $0x3FF, %ebx
    jle     .LexpDenormal
    cmpl    $0x7FF, %ebx
    jge     .LexpOverflowOut
.LexpLastStep:
    shlq    $52, %rbx
    movq    %rbx, %xmm1
    mulsd   %xmm1, %xmm0
    movsd   %xmm0, 16(%rsp)
    ret
.LexpNotFinite:
    movabsq $0xFFF0000000000000, %rax // -Inf
    cmpq    %rax, %rbx
    jne     .LexpNotNegInf
.LexpUnderflow:
    movq    $0, %rax
    movq    %rax, 16(%rsp)
    ret
.LexpOverflowOut:
    movabsq $0x7FF0000000000000, %rbx // +Inf
.LexpNotNegInf:
    movq    %rbx, 16(%rsp)
    ret
.LexpDenormal:
    cmpl    $-52, %ebx
    jl      .LexpUnderflow
    addl    $0x3FE, %ebx
    shlq    $52, %rbx
    movq    %rbx, %xmm1
    mulsd   %xmm1, %xmm0
    movl    $1, %ebx
    jmp     .LexpLastStep

    .section .rodata
.LexpData:
    .quad 0x3FE0000000000000          // 0.5
    .quad 0x3FF0000000000000          // 1.0
    .quad 0x4000000000000000          // 2.0
    .quad 0x3FC555555555553E          // 1/6
    .quad 0x3FA555555555554C          // 1/24
    .quad 0x3F8111111111110F          // 1/120
    .quad 0x3F56C16C16C16BE9          // 1/720
    .quad 0x3F2A01A01A019E56          // 1/5040
    .quad 0x3EFA01A01A01D423          // 1/40320
.LexpLn2u:
    .quad 0x3FE62E42FEE00000
.LexpLn2l:
    .quad 0x3DEA39EF35793C76
.LexpLog2e:
    .quad 0x3FF71547652B82FE
.LexpOverflow:
    .quad 0x40862E42FEFA39EF
.Lexp0625:
    .quad 0x3FB0000000000000          // 0.0625
