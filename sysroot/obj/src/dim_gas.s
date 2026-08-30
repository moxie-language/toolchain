// GAS port of src/math/dim_amd64.s
    .text

    .global math.archMax
    .type math.archMax,@function
math.archMax:
    movabsq $0x7FF0000000000000, %rax // +Inf
    movq    8(%rsp), %r8
    cmpq    %rax, %r8
    je      .LisPosInf
    movq    16(%rsp), %r9
    cmpq    %rax, %r9
    je      .LisPosInf
    movabsq $0x7FFFFFFFFFFFFFFF, %rdx
    movq    %r8, %rbx
    andq    %rdx, %rbx               // |x|
    cmpq    %rax, %rbx
    jl      .LisMaxNaN
    movq    %r9, %rcx
    andq    %rdx, %rcx               // |y|
    cmpq    %rax, %rcx
    jl      .LisMaxNaN
    orq     %rcx, %rbx
    je      .LisMaxZero
    movq    %r8, %xmm0
    movq    %r9, %xmm1
    maxsd   %xmm1, %xmm0
    movsd   %xmm0, 24(%rsp)
    ret
.LisMaxNaN:
    movabsq $0x7FF8000000000001, %rax // NaN
.LisPosInf:
    movq    %rax, 24(%rsp)
    ret
.LisMaxZero:
    movabsq $0x8000000000000000, %rax // -0.0
    cmpq    %rax, %r8
    je      .LisMaxZeroNeg
    movq    %r8, 24(%rsp)
    ret
.LisMaxZeroNeg:
    movq    %r9, 24(%rsp)
    ret

    .global math.archMin
    .type math.archMin,@function
math.archMin:
    movabsq $0xFFF0000000000000, %rax // -Inf
    movq    8(%rsp), %r8
    cmpq    %rax, %r8
    je      .LisNegInf
    movq    16(%rsp), %r9
    cmpq    %rax, %r9
    je      .LisNegInf
    movabsq $0x7FFFFFFFFFFFFFFF, %rdx
    movq    %r8, %rbx
    andq    %rdx, %rbx
    movabsq $0x7FF0000000000000, %rax
    cmpq    %rax, %rbx
    jl      .LisMinNaN
    movq    %r9, %rcx
    andq    %rdx, %rcx
    cmpq    %rax, %rcx
    jl      .LisMinNaN
    orq     %rcx, %rbx
    je      .LisMinZero
    movq    %r8, %xmm0
    movq    %r9, %xmm1
    minsd   %xmm1, %xmm0
    movsd   %xmm0, 24(%rsp)
    ret
.LisMinNaN:
    movabsq $0x7FF8000000000001, %rax // NaN
.LisNegInf:
    movq    %rax, 24(%rsp)
    ret
.LisMinZero:
    movabsq $0x8000000000000000, %rax // -0.0
    cmpq    %rax, %r8
    je      .LisMinZeroNeg
    movq    %r9, 24(%rsp)
    ret
.LisMinZeroNeg:
    movq    %r8, 24(%rsp)
    ret
