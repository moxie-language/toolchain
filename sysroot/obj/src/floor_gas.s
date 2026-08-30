// GAS port of src/math/floor_amd64.s for the moxie bootstrap.
    .text

    .global math.archFloor
    .type math.archFloor,@function
math.archFloor:
    movq    8(%rsp), %rax            // x
    movabsq $0x7FFFFFFFFFFFFFFF, %rdx
    andq    %rax, %rdx               // DX = |x|
    subq    $1, %rdx
    movabsq $0x432FFFFFFFFFFFFF, %rcx // Big - 1
    cmpq    %rcx, %rdx
    jae     .LisBig_floor
    movq    %rax, %xmm0
    cvttsd2siq %xmm0, %rax
    cvtsi2sdq %rax, %xmm1
    cmpltsd %xmm1, %xmm0            // xmm0 = 0xFFF... if x < float(int(x))
    movsd   .LCneg1(%rip), %xmm2
    andpd   %xmm2, %xmm0
    addsd   %xmm1, %xmm0
    movsd   %xmm0, 16(%rsp)
    ret
.LisBig_floor:
    movq    %rax, 16(%rsp)
    ret

    .global math.archCeil
    .type math.archCeil,@function
math.archCeil:
    movq    8(%rsp), %rax            // x
    movabsq $0x7FFFFFFFFFFFFFFF, %rdx
    movq    %rax, %rbx
    andq    %rdx, %rbx               // BX = |x|
    movabsq $0x4330000000000000, %rcx // Big
    cmpq    %rcx, %rbx
    jae     .LisBig_ceil
    movq    %rax, %xmm0
    movq    %rdx, %xmm2
    cvttsd2siq %xmm0, %rax
    andnpd  %xmm0, %xmm2             // X2 = sign
    cvtsi2sdq %rax, %xmm1
    cmplesd %xmm1, %xmm0             // xmm0 = 0xFFF... if float(int(x)) <= x
    orpd    %xmm2, %xmm1
    movsd   .LCone(%rip), %xmm3
    andnpd  %xmm3, %xmm0
    orpd    %xmm2, %xmm0
    addsd   %xmm1, %xmm0
    movsd   %xmm0, 16(%rsp)
    ret
.LisBig_ceil:
    movq    %rax, 16(%rsp)
    ret

    .global math.archTrunc
    .type math.archTrunc,@function
math.archTrunc:
    movq    8(%rsp), %rax            // x
    movabsq $0x7FFFFFFFFFFFFFFF, %rdx
    movq    %rax, %rbx
    andq    %rdx, %rbx               // BX = |x|
    movabsq $0x4330000000000000, %rcx // Big
    cmpq    %rcx, %rbx
    jae     .LisBig_trunc
    movq    %rax, %xmm0
    movq    %rdx, %xmm2
    cvttsd2siq %xmm0, %rax
    andnpd  %xmm0, %xmm2             // X2 = sign
    cvtsi2sdq %rax, %xmm0
    orpd    %xmm2, %xmm0
    movsd   %xmm0, 16(%rsp)
    ret
.LisBig_trunc:
    movq    %rax, 16(%rsp)
    ret

    .section .rodata
.LCneg1:
    .quad 0xBFF0000000000000          // -1.0
.LCone:
    .quad 0x3FF0000000000000          // 1.0
