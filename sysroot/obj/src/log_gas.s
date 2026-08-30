// GAS port of src/math/log_amd64.s
    .text

    .global math.archLog
    .type math.archLog,@function
math.archLog:
    movq    8(%rsp), %rbx            // x
    movabsq $0x7FFFFFFFFFFFFFFF, %rax
    andq    %rbx, %rax
    je      .LlogIsZero
    movq    $0, %rax
    cmpq    %rax, %rbx
    jg      .LlogIsNegative
    movabsq $0x7FF0000000000000, %rax // +Inf
    cmpq    %rax, %rbx
    jle     .LlogIsInfOrNaN
    movq    %rbx, %xmm0
    movabsq $0x000FFFFFFFFFFFFF, %rax
    movq    %rax, %xmm2
    andpd   %xmm0, %xmm2
    movsd   .LlogHalf(%rip), %xmm0
    orpd    %xmm0, %xmm2             // X2 = f1
    shrq    $52, %rbx
    andl    $0x7FF, %ebx
    subl    $0x3FE, %ebx
    xorpd   %xmm1, %xmm1
    cvtsi2sdl %ebx, %xmm1            // X1 = k
    movsd   .LlogHsqrt2(%rip), %xmm0
    cmplesd %xmm2, %xmm0             // xmm0 = (xmm0 <= f1) ? all-ones : 0
    movsd   .LlogOne(%rip), %xmm3
    andpd   %xmm0, %xmm3             // X3 = 0 or 1
    subsd   %xmm3, %xmm1             // k -= X3
    movsd   .LlogOne(%rip), %xmm0
    addsd   %xmm0, %xmm3             // X3 = 1 or 2
    mulsd   %xmm3, %xmm2             // f1 *= X3
    subsd   %xmm0, %xmm2             // f = f1 - 1
    movsd   .LlogTwo(%rip), %xmm0
    addsd   %xmm2, %xmm0
    movapd  %xmm2, %xmm3
    divsd   %xmm0, %xmm3             // s = f/(2+f)
    movapd  %xmm3, %xmm4
    mulsd   %xmm4, %xmm4             // s2
    movapd  %xmm4, %xmm5
    mulsd   %xmm5, %xmm5             // s4
    movsd   .LlogL7(%rip), %xmm6
    mulsd   %xmm5, %xmm6
    addsd   .LlogL5(%rip), %xmm6
    mulsd   %xmm5, %xmm6
    addsd   .LlogL3(%rip), %xmm6
    mulsd   %xmm5, %xmm6
    addsd   .LlogL1(%rip), %xmm6
    mulsd   %xmm6, %xmm4             // t1
    movsd   .LlogL6(%rip), %xmm6
    mulsd   %xmm5, %xmm6
    addsd   .LlogL4(%rip), %xmm6
    mulsd   %xmm5, %xmm6
    addsd   .LlogL2(%rip), %xmm6
    mulsd   %xmm6, %xmm5             // t2
    addsd   %xmm5, %xmm4             // R
    movsd   .LlogHalf(%rip), %xmm0
    mulsd   %xmm2, %xmm0
    mulsd   %xmm2, %xmm0             // hfsq
    addsd   %xmm0, %xmm4             // hfsq+R
    mulsd   %xmm4, %xmm3             // s*(hfsq+R)
    movsd   .LlogLn2lo(%rip), %xmm4
    mulsd   %xmm1, %xmm4             // k*Ln2Lo
    addsd   %xmm4, %xmm3
    subsd   %xmm3, %xmm0
    subsd   %xmm2, %xmm0
    movsd   .LlogLn2hi(%rip), %xmm4
    mulsd   %xmm1, %xmm4             // k*Ln2Hi
    subsd   %xmm0, %xmm4
    movsd   %xmm4, 16(%rsp)
    ret
.LlogIsInfOrNaN:
    movq    %rbx, 16(%rsp)
    ret
.LlogIsNegative:
    movabsq $0x7FF8000000000001, %rax // NaN
    movq    %rax, 16(%rsp)
    ret
.LlogIsZero:
    movabsq $0xFFF0000000000000, %rax // -Inf
    movq    %rax, 16(%rsp)
    ret

    .section .rodata
.LlogHalf:
    .quad 0x3FE0000000000000          // 0.5
.LlogOne:
    .quad 0x3FF0000000000000          // 1.0
.LlogTwo:
    .quad 0x4000000000000000          // 2.0
.LlogHsqrt2:
    .quad 0x3FE6A09E667F3BCD          // sqrt(2)/2
.LlogLn2hi:
    .quad 0x3FE62E42FEE00000
.LlogLn2lo:
    .quad 0x3DEA39EF35793C76
.LlogL1:
    .quad 0x3FE5555555555593
.LlogL2:
    .quad 0x3FD999999997FA04
.LlogL3:
    .quad 0x3FD2492494229359
.LlogL4:
    .quad 0x3FCC71C51D8E78AF
.LlogL5:
    .quad 0x3FC7466496CB03DE
.LlogL6:
    .quad 0x3FC39A09D078C69F
.LlogL7:
    .quad 0x3FC2F112DF3E5244
