// GAS port of src/math/hypot_amd64.s
    .text

    .global math.archHypot
    .type math.archHypot,@function
math.archHypot:
    movq    8(%rsp), %rbx            // p
    movabsq $0x7FFFFFFFFFFFFFFF, %rax
    andq    %rax, %rbx               // |p|
    movq    16(%rsp), %rcx           // q
    andq    %rax, %rcx               // |q|
    movabsq $0x7FF0000000000000, %rax
    cmpq    %rax, %rbx
    jle     .LisInfOrNaN
    cmpq    %rax, %rcx
    jle     .LisInfOrNaN
    movq    %rbx, %xmm0
    movq    %rcx, %xmm1
    orq     %rcx, %rbx
    je      .LisZero
    movapd  %xmm0, %xmm2
    maxsd   %xmm1, %xmm0
    minsd   %xmm2, %xmm1
    divsd   %xmm0, %xmm1
    mulsd   %xmm1, %xmm1
    movsd   .LHCone(%rip), %xmm2
    addsd   %xmm2, %xmm1
    sqrtsd  %xmm1, %xmm1
    mulsd   %xmm1, %xmm0
    movsd   %xmm0, 24(%rsp)
    ret
.LisInfOrNaN:
    cmpq    %rax, %rbx
    je      .LisInf
    cmpq    %rax, %rcx
    je      .LisInf
    movabsq $0x7FF8000000000001, %rax // NaN
    movq    %rax, 24(%rsp)
    ret
.LisInf:
    movq    %rax, 24(%rsp)
    ret
.LisZero:
    movq    $0, %rax
    movq    %rax, 24(%rsp)
    ret

    .section .rodata
.LHCone:
    .quad 0x3FF0000000000000          // 1.0
