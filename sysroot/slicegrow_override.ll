; Diagnostic alloc override - traps huge allocation requests with backtrace.
; Also overrides sliceGrow to call free.

target datalayout = "e-m:e-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128"
target triple = "x86_64-unknown-linux-musleabihf"

@runtime.zeroSizedAlloc = external hidden global i8
declare ptr @calloc(i64, i64)
declare void @free(ptr)
declare i64 @write(i32, ptr, i64)
declare void @exit(i32) noreturn
declare i32 @snprintf(ptr, i64, ptr, ...)
declare hidden void @runtime.memmove(ptr, ptr, i64, ptr)
declare hidden i64 @runtime.uintptrLen(i64, ptr)
declare hidden void @runtime.runtimePanic(ptr, i64, i64, ptr)

@diag.msg = internal constant [128 x i8] c"\n\n!!! ALLOC TRAP: huge size requested. size=0x%lx (%lu) caller_ra=%p\nlast_szlt=0x%lx slt=0x%lx\nNN\0"
@diag.buf = internal global [256 x i8] zeroinitializer

define hidden noalias nonnull ptr @runtime.alloc(i64 %size, ptr %layout, ptr %context) #0 {
entry:
  %zero = icmp eq i64 %size, 0
  br i1 %zero, label %ret_zero, label %check_huge

ret_zero:
  ret ptr @runtime.zeroSizedAlloc

check_huge:
  ; Sizes > 256MB are almost certainly bugs in compiler
  %threshold = icmp ugt i64 %size, 268435456
  br i1 %threshold, label %trap_huge, label %normal

trap_huge:
  %ra = call ptr @llvm.returnaddress(i32 0)
  %nbytes = call i32 (ptr, i64, ptr, ...) @snprintf(
    ptr getelementptr ([256 x i8], ptr @diag.buf, i64 0, i64 0),
    i64 256,
    ptr getelementptr ([128 x i8], ptr @diag.msg, i64 0, i64 0),
    i64 %size,
    i64 %size,
    ptr %ra,
    i64 0,
    i64 0)
  %nb64 = sext i32 %nbytes to i64
  call i64 @write(i32 2,
    ptr getelementptr ([256 x i8], ptr @diag.buf, i64 0, i64 0),
    i64 %nb64)
  call void @exit(i32 134) noreturn
  unreachable

normal:
  %aligned = add i64 %size, 15
  %size16 = and i64 %aligned, -16
  %total = add i64 %size16, 16
  %base = call ptr @calloc(i64 1, i64 %total)
  %ok = icmp ne ptr %base, null
  br i1 %ok, label %done, label %oom

oom:
  call void @exit(i32 134) noreturn
  unreachable

done:
  store i64 -6872988504188559820, ptr %base, align 8
  %user = getelementptr i8, ptr %base, i64 16
  ret ptr %user
}

declare ptr @llvm.returnaddress(i32)
declare hidden ptr @runtime.alloc(i64, ptr, ptr)
declare hidden void @runtime.memmove(ptr, ptr, i64, ptr)
declare hidden void @runtime.negativeShiftPanic(ptr)
declare hidden i32 @"math/bits.Len"(i32, ptr)
declare hidden ptr @"(internal/gclayout.Layout).AsPtr"(i64, ptr)

define hidden { ptr, i64, i64 } @runtime.sliceGrow(ptr %oldBuf, i64 %oldLen, i64 %oldCap, i64 %newCap, i64 %elemSize, ptr %context) #0 {
entry:
  %0 = icmp uge i64 %oldCap, %newCap
  br i1 %0, label %no_grow, label %grow

no_grow:
  %r0 = insertvalue { ptr, i64, i64 } zeroinitializer, ptr %oldBuf, 0
  %r1 = insertvalue { ptr, i64, i64 } %r0, i64 %oldLen, 1
  %r2 = insertvalue { ptr, i64, i64 } %r1, i64 %oldCap, 2
  ret { ptr, i64, i64 } %r2

grow:
  %tr = trunc i64 %newCap to i32
  %bits = call i32 @"math/bits.Len"(i32 %tr, ptr undef)
  %neg = icmp slt i32 %bits, 0
  br i1 %neg, label %shift_throw, label %shift_ok

shift_ok:
  %overflow = icmp uge i32 %bits, 64
  %ext = zext i32 %bits to i64
  %shifted = shl i64 1, %ext
  %roundCap = select i1 %overflow, i64 0, i64 %shifted
  %small = icmp ult i64 %elemSize, 8
  br i1 %small, label %set_layout, label %do_alloc

set_layout:
  %lay = call ptr @"(internal/gclayout.Layout).AsPtr"(i64 3, ptr undef)
  br label %do_alloc

do_alloc:
  %layout = phi ptr [ null, %shift_ok ], [ %lay, %set_layout ]
  %totalSz = mul i64 %roundCap, %elemSize
  %buf = call ptr @runtime.alloc(i64 %totalSz, ptr %layout, ptr undef)
  %hasData = icmp ugt i64 %oldLen, 0
  br i1 %hasData, label %copy, label %free_old

copy:
  %copySz = mul i64 %oldLen, %elemSize
  call void @runtime.memmove(ptr %buf, ptr %oldBuf, i64 %copySz, ptr undef)
  br label %free_old

free_old:
  %oldNotNull = icmp ne ptr %oldBuf, null
  br i1 %oldNotNull, label %do_free, label %ret

do_free:
  call void @free(ptr %oldBuf)
  br label %ret

ret:
  %s0 = insertvalue { ptr, i64, i64 } zeroinitializer, ptr %buf, 0
  %s1 = insertvalue { ptr, i64, i64 } %s0, i64 %oldLen, 1
  %s2 = insertvalue { ptr, i64, i64 } %s1, i64 %roundCap, 2
  ret { ptr, i64, i64 } %s2

shift_throw:
  call void @runtime.negativeShiftPanic(ptr undef)
  unreachable
}

attributes #0 = { noinline nounwind }
