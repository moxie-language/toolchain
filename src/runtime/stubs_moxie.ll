; Linker stubs for functions not available in the Moxie runtime.
; These allow linking to succeed. Functions that are actually called
; at runtime will trap; dead-code paths are harmless.

target datalayout = "e-m:e-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128"
target triple = "x86_64-unknown-linux-musleabihf"

; --- time ---

define void @time.Sleep(i64 %ns, ptr %ctx) {
  ; nanosleep syscall: SYS_nanosleep = 35
  %ts = alloca [16 x i8], align 8
  %sec_ptr = bitcast ptr %ts to ptr
  %nsec_ptr = getelementptr i8, ptr %ts, i64 8
  %sec = udiv i64 %ns, 1000000000
  %nsec = urem i64 %ns, 1000000000
  store i64 %sec, ptr %sec_ptr, align 8
  store i64 %nsec, ptr %nsec_ptr, align 8
  call i64 asm sideeffect "syscall", "={rax},{rax},{rdi},{rsi},~{rcx},~{r11},~{memory}"(i64 35, ptr %ts, ptr null)
  ret void
}

define ptr @time.newTimer(i64 %when, i64 %period, {ptr, ptr} %f, {ptr, ptr} %arg, ptr %c, ptr %ctx) {
  ret ptr null
}

define i1 @time.stopTimer(ptr %t, ptr %ctx) {
  ret i1 false
}

define i1 @time.resetTimer(ptr %t, i64 %when, i64 %period, ptr %ctx) {
  ret i1 false
}

define i1 @time.runtimeIsBubbled(ptr %ctx) {
  ret i1 false
}

; --- internal/poll ---

define void @"internal/poll.runtime_pollServerInit"(ptr %ctx) {
  ret void
}

define {i64, i32} @"internal/poll.runtime_pollOpen"(i64 %fd, ptr %ctx) {
  %r = insertvalue {i64, i32} undef, i64 %fd, 0
  %r2 = insertvalue {i64, i32} %r, i32 0, 1
  ret {i64, i32} %r2
}

define void @"internal/poll.runtime_pollClose"(i64 %ctx_fd, ptr %ctx) {
  ret void
}

define i32 @"internal/poll.runtime_pollWait"(i64 %ctx_fd, i32 %mode, ptr %ctx) {
  ret i32 0
}

define void @"internal/poll.runtime_pollWaitCanceled"(i64 %ctx_fd, i32 %mode, ptr %ctx) {
  ret void
}

define i32 @"internal/poll.runtime_pollReset"(i64 %ctx_fd, i32 %mode, ptr %ctx) {
  ret i32 0
}

define void @"internal/poll.runtime_pollSetDeadline"(i64 %ctx_fd, i64 %d, i32 %mode, ptr %ctx) {
  ret void
}

define void @"internal/poll.runtime_pollUnblock"(i64 %ctx_fd, ptr %ctx) {
  ret void
}

define void @"internal/poll.runtime_Semacquire"(ptr %addr, ptr %ctx) {
  ret void
}

define void @"internal/poll.runtime_Semrelease"(ptr %addr, ptr %ctx) {
  ret void
}

; --- context ---

define ptr @context._closedchan(ptr %ctx) {
  ret ptr null
}

define ptr @context.cancelCtx.Done(ptr %self, ptr %ctx) {
  ret ptr null
}

define ptr @context.emptyCtx.Done(ptr %self, ptr %ctx) {
  ret ptr null
}

define ptr @context.withoutCancelCtx.Done(ptr %self, ptr %ctx) {
  ret ptr null
}

define ptr @context.parentCancelCtx(ptr %parent, ptr %ctx) {
  ret ptr null
}

; --- net ---

define ptr @net.pipeDeadline.wait(ptr %self, ptr %ctx) {
  ret ptr null
}

; --- crypto/elliptic nistCurve generic methods ---

define ptr @"crypto/elliptic.nistCurve.Params"(ptr %self, ptr %ctx) {
  ret ptr null
}

define {ptr, ptr} @"crypto/elliptic.nistCurve.Add"(ptr %self, ptr %x1, ptr %y1, ptr %x2, ptr %y2, ptr %ctx) {
  %r = insertvalue {ptr, ptr} undef, ptr null, 0
  %r2 = insertvalue {ptr, ptr} %r, ptr null, 1
  ret {ptr, ptr} %r2
}

define i1 @"crypto/elliptic.nistCurve.IsOnCurve"(ptr %self, ptr %x, ptr %y, ptr %ctx) {
  ret i1 false
}

define {ptr, ptr} @"crypto/elliptic.nistCurve.ScalarBaseMult"(ptr %self, ptr %k_ptr, i64 %k_len, i64 %k_cap, ptr %ctx) {
  %r = insertvalue {ptr, ptr} undef, ptr null, 0
  %r2 = insertvalue {ptr, ptr} %r, ptr null, 1
  ret {ptr, ptr} %r2
}

define {ptr, ptr} @"crypto/elliptic.nistCurve.ScalarMult"(ptr %self, ptr %bx, ptr %by, ptr %k_ptr, i64 %k_len, i64 %k_cap, ptr %ctx) {
  %r = insertvalue {ptr, ptr} undef, ptr null, 0
  %r2 = insertvalue {ptr, ptr} %r, ptr null, 1
  ret {ptr, ptr} %r2
}

; --- crypto/ecdh nistCurve generic methods ---

define {ptr, {ptr, ptr}} @"crypto/ecdh.nistCurve.GenerateKey"(ptr %self, ptr %rand, ptr %rand2, ptr %ctx) {
  %r = insertvalue {ptr, {ptr, ptr}} undef, ptr null, 0
  %r2 = insertvalue {ptr, {ptr, ptr}} %r, ptr null, 1, 0
  %r3 = insertvalue {ptr, {ptr, ptr}} %r2, ptr null, 1, 1
  ret {ptr, {ptr, ptr}} %r3
}

define {ptr, {ptr, ptr}} @"crypto/ecdh.nistCurve.NewPrivateKey"(ptr %self, ptr %key_ptr, i64 %key_len, i64 %key_cap, ptr %ctx) {
  %r = insertvalue {ptr, {ptr, ptr}} undef, ptr null, 0
  %r2 = insertvalue {ptr, {ptr, ptr}} %r, ptr null, 1, 0
  %r3 = insertvalue {ptr, {ptr, ptr}} %r2, ptr null, 1, 1
  ret {ptr, {ptr, ptr}} %r3
}

define {ptr, {ptr, ptr}} @"crypto/ecdh.nistCurve.NewPublicKey"(ptr %self, ptr %key_ptr, i64 %key_len, i64 %key_cap, ptr %ctx) {
  %r = insertvalue {ptr, {ptr, ptr}} undef, ptr null, 0
  %r2 = insertvalue {ptr, {ptr, ptr}} %r, ptr null, 1, 0
  %r3 = insertvalue {ptr, {ptr, ptr}} %r2, ptr null, 1, 1
  ret {ptr, {ptr, ptr}} %r3
}

define {ptr, i64, i64} @"crypto/ecdh.nistCurve.String"(ptr %self, ptr %ctx) {
  %r = insertvalue {ptr, i64, i64} undef, ptr null, 0
  %r2 = insertvalue {ptr, i64, i64} %r, i64 0, 1
  %r3 = insertvalue {ptr, i64, i64} %r2, i64 0, 2
  ret {ptr, i64, i64} %r3
}

; --- os.runtime_args ---

@runtime.main_argc = external global i32, align 4
@runtime.main_argv = external global ptr, align 8
declare noalias nonnull ptr @runtime.alloc(i64, ptr, ptr)

define {ptr, i64, i64} @"os.runtime_args"() {
entry:
  %argc32 = load i32, ptr @runtime.main_argc, align 4
  %argc = sext i32 %argc32 to i64
  %cmp = icmp sle i64 %argc, 0
  br i1 %cmp, label %empty, label %alloc

empty:
  ret {ptr, i64, i64} zeroinitializer

alloc:
  ; each string is {ptr, i64, i64} = 24 bytes
  %sz = mul i64 %argc, 24
  %arr = call ptr @runtime.alloc(i64 %sz, ptr inttoptr (i64 3 to ptr), ptr undef)
  %argv = load ptr, ptr @runtime.main_argv, align 8
  br label %loop

loop:
  %i = phi i64 [0, %alloc], [%inext, %done_str]
  %done = icmp uge i64 %i, %argc
  br i1 %done, label %ret, label %body

body:
  ; argv[i] is a C string pointer
  %argvp = getelementptr ptr, ptr %argv, i64 %i
  %cstr = load ptr, ptr %argvp, align 8
  br label %strlen_loop

strlen_loop:
  %slen = phi i64 [0, %body], [%snext, %strlen_cont]
  %cp = getelementptr i8, ptr %cstr, i64 %slen
  %ch = load i8, ptr %cp, align 1
  %isz = icmp eq i8 %ch, 0
  br i1 %isz, label %done_str, label %strlen_cont

strlen_cont:
  %snext = add i64 %slen, 1
  br label %strlen_loop

done_str:
  ; store string {ptr, len, cap} at arr[i]
  %slot = getelementptr [24 x i8], ptr %arr, i64 %i
  %pp = getelementptr {ptr, i64, i64}, ptr %slot, i64 0, i32 0
  store ptr %cstr, ptr %pp, align 8
  %lp = getelementptr {ptr, i64, i64}, ptr %slot, i64 0, i32 1
  store i64 %slen, ptr %lp, align 8
  %cp2 = getelementptr {ptr, i64, i64}, ptr %slot, i64 0, i32 2
  store i64 %slen, ptr %cp2, align 8
  %inext = add i64 %i, 1
  br label %loop

ret:
  %r1 = insertvalue {ptr, i64, i64} undef, ptr %arr, 0
  %r2 = insertvalue {ptr, i64, i64} %r1, i64 %argc, 1
  %r3 = insertvalue {ptr, i64, i64} %r2, i64 %argc, 2
  ret {ptr, i64, i64} %r3
}

; --- slices (generic functions not monomorphized by compiler) ---

define void @slices.Clone(ptr %ctx) {
  ret void
}

define void @slices.SortFunc(ptr %ctx) {
  ret void
}

define void @slices.SortStableFunc(ptr %ctx) {
  ret void
}

; --- unsafe (compiler intrinsics that should be resolved at compile time) ---

define i32 @unsafe.Sizeof({ptr, ptr} %iface, ptr %ctx) {
  ret i32 0
}

define i32 @unsafe.Offsetof({ptr, ptr} %iface, ptr %ctx) {
  ret i32 0
}

; --- syscall ---

define i32 @syscall.Getpagesize() {
  ret i32 4096
}

; --- net ---

define i64 @net.runtime_rand(ptr %ctx) {
  %buf = alloca i64, align 8
  call i64 asm sideeffect "syscall", "={rax},{rax},{rdi},{rsi},{rdx},~{rcx},~{r11},~{memory}"(i64 318, ptr %buf, i64 8, i64 0)
  %val = load i64, ptr %buf, align 8
  ret i64 %val
}

; --- runtime ---

define i32 @runtime.LastSpawnedParentFd(ptr %ctx) {
  ret i32 -1
}

; --- crypto/internal/fips140/ecdh ---

define void @"crypto/internal/fips140/ecdh.ECDH"(ptr %ctx) {
  ret void
}

define void @"crypto/internal/fips140/ecdh.GenerateKey"(ptr %ctx) {
  ret void
}

define void @"crypto/internal/fips140/ecdh.NewPublicKey"(ptr %ctx) {
  ret void
}

; --- builtin String() methods for basic types ---

declare {ptr, i64, i64} @strconv.Itoa(i32, ptr)
declare {ptr, i64, i64} @strconv.FormatInt(i64, i32, ptr)
declare {ptr, i64, i64} @strconv.FormatUint(i64, i32, ptr)
declare {ptr, i64, i64} @strconv.FormatBool(i1, ptr)
declare {ptr, i64, i64} @strconv.FormatFloat(double, i8, i32, i32, ptr)

define {ptr, i64, i64} @"__builtin.bytes.String"({ptr, i64, i64} %self, ptr %ctx) {
  ret {ptr, i64, i64} %self
}

define {ptr, i64, i64} @"__builtin.bool.String"(i1 %self, ptr %ctx) {
  %r = call {ptr, i64, i64} @strconv.FormatBool(i1 %self, ptr null)
  ret {ptr, i64, i64} %r
}

define {ptr, i64, i64} @"__builtin.int8.String"(i8 %self, ptr %ctx) {
  %ext = sext i8 %self to i32
  %r = call {ptr, i64, i64} @strconv.Itoa(i32 %ext, ptr null)
  ret {ptr, i64, i64} %r
}

define {ptr, i64, i64} @"__builtin.int16.String"(i16 %self, ptr %ctx) {
  %ext = sext i16 %self to i32
  %r = call {ptr, i64, i64} @strconv.Itoa(i32 %ext, ptr null)
  ret {ptr, i64, i64} %r
}

define {ptr, i64, i64} @"__builtin.int32.String"(i32 %self, ptr %ctx) {
  %r = call {ptr, i64, i64} @strconv.Itoa(i32 %self, ptr null)
  ret {ptr, i64, i64} %r
}

define {ptr, i64, i64} @"__builtin.int64.String"(i64 %self, ptr %ctx) {
  %r = call {ptr, i64, i64} @strconv.FormatInt(i64 %self, i32 10, ptr null)
  ret {ptr, i64, i64} %r
}

define {ptr, i64, i64} @"__builtin.uint8.String"(i8 %self, ptr %ctx) {
  %ext = zext i8 %self to i64
  %r = call {ptr, i64, i64} @strconv.FormatUint(i64 %ext, i32 10, ptr null)
  ret {ptr, i64, i64} %r
}

define {ptr, i64, i64} @"__builtin.uint16.String"(i16 %self, ptr %ctx) {
  %ext = zext i16 %self to i64
  %r = call {ptr, i64, i64} @strconv.FormatUint(i64 %ext, i32 10, ptr null)
  ret {ptr, i64, i64} %r
}

define {ptr, i64, i64} @"__builtin.uint32.String"(i32 %self, ptr %ctx) {
  %ext = zext i32 %self to i64
  %r = call {ptr, i64, i64} @strconv.FormatUint(i64 %ext, i32 10, ptr null)
  ret {ptr, i64, i64} %r
}

define {ptr, i64, i64} @"__builtin.uint64.String"(i64 %self, ptr %ctx) {
  %r = call {ptr, i64, i64} @strconv.FormatUint(i64 %self, i32 10, ptr null)
  ret {ptr, i64, i64} %r
}

define {ptr, i64, i64} @"__builtin.float32.String"(float %self, ptr %ctx) {
  %ext = fpext float %self to double
  %r = call {ptr, i64, i64} @strconv.FormatFloat(double %ext, i8 103, i32 -1, i32 32, ptr null)
  ret {ptr, i64, i64} %r
}

define {ptr, i64, i64} @"__builtin.float64.String"(double %self, ptr %ctx) {
  %r = call {ptr, i64, i64} @strconv.FormatFloat(double %self, i8 103, i32 -1, i32 64, ptr null)
  ret {ptr, i64, i64} %r
}
