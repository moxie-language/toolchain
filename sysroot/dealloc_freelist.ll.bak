; Scope-based memory management runtime.
; alloc uses calloc with 16-byte magic header.
; free checks magic before freeing.
; Scope tracking via linked list with save points.
; sliceGrow does NOT free old buffers - scope system handles lifetime.

target datalayout = "e-m:e-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128"
target triple = "x86_64-unknown-linux-musleabihf"

@runtime.zeroSizedAlloc = external hidden global i8

declare ptr @calloc(i64, i64)
declare void @free(ptr)
declare ptr @realloc(ptr, i64)

declare hidden void @runtime.runtimePanic(ptr, i64, i64, ptr)
@.str.oom = hidden constant [13 x i8] c"out of memory"

define hidden noalias nonnull ptr @runtime.alloc(i64 %size, ptr %layout, ptr %context) #0 {
entry:
  %isz = icmp eq i64 %size, 0
  br i1 %isz, label %zero, label %nonzero
zero:
  ret ptr @runtime.zeroSizedAlloc
nonzero:
  %aligned = add i64 %size, 15
  %size16 = and i64 %aligned, -16
  %total = add i64 %size16, 16
  %base = call ptr @calloc(i64 1, i64 %total)
  %ok = icmp ne ptr %base, null
  br i1 %ok, label %done, label %oom
oom:
  call void @runtime.runtimePanic(ptr @.str.oom, i64 13, i64 13, ptr undef)
  unreachable
done:
  store i64 -6872988504188559820, ptr %base, align 8
  %user = getelementptr i8, ptr %base, i64 16
  ret ptr %user
}

define hidden void @runtime.free(ptr %ptr, ptr %context) {
entry:
  %isnil = icmp eq ptr %ptr, null
  br i1 %isnil, label %done, label %check
check:
  %base = getelementptr i8, ptr %ptr, i64 -16
  %magic = load i64, ptr %base, align 8
  %is_ours = icmp eq i64 %magic, -6872988504188559820
  br i1 %is_ours, label %dofree, label %done
dofree:
  store i64 0, ptr %base, align 8
  call void @free(ptr %base)
  br label %done
done:
  ret void
}

define hidden ptr @runtime.realloc(ptr %ptr, i64 %size) {
entry:
  %base = getelementptr i8, ptr %ptr, i64 -16
  %aligned = add i64 %size, 15
  %size16 = and i64 %aligned, -16
  %total = add i64 %size16, 16
  %p = call ptr @realloc(ptr %base, i64 %total)
  %user = getelementptr i8, ptr %p, i64 16
  ret ptr %user
}

define hidden void @runtime.hashmapDestroy(ptr %m, ptr %context) {
entry:
  %isnil = icmp eq ptr %m, null
  br i1 %isnil, label %done, label %destroy
destroy:
  %bucketsPtr = load ptr, ptr %m, align 8
  %hasBuckets = icmp ne ptr %bucketsPtr, null
  br i1 %hasBuckets, label %freeBuckets, label %freeMap
freeBuckets:
  %keySizePtr = getelementptr i8, ptr %m, i64 24
  %keySize = load i64, ptr %keySizePtr, align 8
  %valSizePtr = getelementptr i8, ptr %m, i64 32
  %valSize = load i64, ptr %valSizePtr, align 8
  %ks8 = mul i64 %keySize, 8
  %vs8 = mul i64 %valSize, 8
  %bs1 = add i64 16, %ks8
  %bucketSize = add i64 %bs1, %vs8
  %bitsPtr = getelementptr i8, ptr %m, i64 40
  %bits8 = load i8, ptr %bitsPtr, align 1
  %bits = zext i8 %bits8 to i64
  %numBuckets = shl i64 1, %bits
  br label %bucketLoop
bucketLoop:
  %i = phi i64 [ 0, %freeBuckets ], [ %inext, %bucketDone ]
  %idone = icmp uge i64 %i, %numBuckets
  br i1 %idone, label %freeBucketArray, label %processBucket
processBucket:
  %off = mul i64 %bucketSize, %i
  %bucket = getelementptr i8, ptr %bucketsPtr, i64 %off
  %overflowPtr = getelementptr i8, ptr %bucket, i64 8
  %overflow = load ptr, ptr %overflowPtr, align 8
  br label %overflowLoop
overflowLoop:
  %ov = phi ptr [ %overflow, %processBucket ], [ %ovNext, %freeOverflow ]
  %ovDone = icmp eq ptr %ov, null
  br i1 %ovDone, label %bucketDone, label %freeOverflow
freeOverflow:
  %ovNextPtr = getelementptr i8, ptr %ov, i64 8
  %ovNext = load ptr, ptr %ovNextPtr, align 8
  call void @runtime.free(ptr %ov, ptr undef)
  br label %overflowLoop
bucketDone:
  %inext = add i64 %i, 1
  br label %bucketLoop
freeBucketArray:
  call void @runtime.free(ptr %bucketsPtr, ptr undef)
  br label %freeMap
freeMap:
  call void @runtime.free(ptr %m, ptr undef)
  br label %done
done:
  ret void
}

declare hidden void @runtime.memmove(ptr, ptr, i64, ptr)
declare hidden void @runtime.negativeShiftPanic(ptr)
declare hidden i32 @"math/bits.Len"(i32, ptr)
declare hidden ptr @"(internal/gclayout.Layout).AsPtr"(i64, ptr)

define hidden { ptr, i64, i64 } @runtime.sliceGrow(ptr %oldBuf, i64 %oldLen, i64 %oldCap, i64 %newCap, i64 %elemSize, ptr %context) #1 {
entry:
  %fits = icmp uge i64 %oldCap, %newCap
  br i1 %fits, label %no_grow, label %grow
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
  br i1 %hasData, label %copy, label %ret
copy:
  %copySz = mul i64 %oldLen, %elemSize
  call void @runtime.memmove(ptr %buf, ptr %oldBuf, i64 %copySz, ptr undef)
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

define void @runtime._panic(ptr %msg) {
  call void @runtime.runtimePanic(ptr %msg, i64 0, i64 0, ptr null)
  unreachable
}

define void @runtime._panicstr({ ptr, i64, i64 } %s) {
  %p = extractvalue { ptr, i64, i64 } %s, 0
  %l = extractvalue { ptr, i64, i64 } %s, 1
  %c = extractvalue { ptr, i64, i64 } %s, 2
  call void @runtime.runtimePanic(ptr %p, i64 %l, i64 %c, ptr null)
  unreachable
}

define void @syscall.runtime_entersyscall() { ret void }
define void @syscall.runtime_exitsyscall() { ret void }

@environ = external global ptr
declare i64 @strlen(ptr)
declare i32 @setenv(ptr, ptr, i32)

define hidden { ptr, i64, i64 } @syscall.runtime_envs(ptr %context) {
entry:
  %ep = load ptr, ptr @environ, align 8
  %isnil = icmp eq ptr %ep, null
  br i1 %isnil, label %empty, label %count
count:
  br label %loop
loop:
  %p = phi ptr [ %ep, %count ], [ %nextp, %next ]
  %n = phi i64 [ 0, %count ], [ %n1, %next ]
  %cur = load ptr, ptr %p, align 8
  %done = icmp eq ptr %cur, null
  br i1 %done, label %build, label %next
next:
  %n1 = add i64 %n, 1
  %nextp = getelementptr ptr, ptr %p, i64 1
  br label %loop
build:
  %sz = mul i64 %n, 24
  %buf = call ptr @runtime.alloc(i64 %sz, ptr null, ptr null)
  br label %fill
fill:
  %i = phi i64 [ 0, %build ], [ %i1, %fillnext ]
  %idone = icmp uge i64 %i, %n
  br i1 %idone, label %retlbl, label %fillbody
fillbody:
  %envp = getelementptr ptr, ptr %ep, i64 %i
  %str = load ptr, ptr %envp, align 8
  %slen = call i64 @strlen(ptr %str)
  %slot = getelementptr { ptr, i64, i64 }, ptr %buf, i64 %i
  store ptr %str, ptr %slot, align 8
  %lenp = getelementptr i8, ptr %slot, i64 8
  store i64 %slen, ptr %lenp, align 8
  %capp = getelementptr i8, ptr %slot, i64 16
  store i64 %slen, ptr %capp, align 8
  br label %fillnext
fillnext:
  %i1 = add i64 %i, 1
  br label %fill
retlbl:
  %r0 = insertvalue { ptr, i64, i64 } zeroinitializer, ptr %buf, 0
  %r1 = insertvalue { ptr, i64, i64 } %r0, i64 %n, 1
  %r2 = insertvalue { ptr, i64, i64 } %r1, i64 %n, 2
  ret { ptr, i64, i64 } %r2
empty:
  ret { ptr, i64, i64 } zeroinitializer
}

define void @syscall.runtimeSetenv({ ptr, i64, i64 } %k, { ptr, i64, i64 } %v, ptr %context) {
  %kptr = extractvalue { ptr, i64, i64 } %k, 0
  %vptr = extractvalue { ptr, i64, i64 } %v, 0
  %1 = call i32 @setenv(ptr %kptr, ptr %vptr, i32 1)
  ret void
}

define { i64, i64, i64 } @"internal/runtime/syscall.Syscall6"(i64 %num, i64 %a1, i64 %a2, i64 %a3, i64 %a4, i64 %a5, i64 %a6) {
  %1 = call { i64, i64 } asm sideeffect "syscall", "={rax},={rdx},{rax},{rdi},{rsi},{rdx},{r10},{r8},{r9},~{rcx},~{r11},~{memory}"(i64 %num, i64 %a1, i64 %a2, i64 %a3, i64 %a4, i64 %a5, i64 %a6)
  %r1 = extractvalue { i64, i64 } %1, 0
  %r2 = extractvalue { i64, i64 } %1, 1
  %ret1 = insertvalue { i64, i64, i64 } undef, i64 %r1, 0
  %ret2 = insertvalue { i64, i64, i64 } %ret1, i64 %r2, 1
  %ret3 = insertvalue { i64, i64, i64 } %ret2, i64 0, 2
  ret { i64, i64, i64 } %ret3
}

; scopePush: push allocation onto scope list.
; Node layout: {ptr payload, ptr next} (16 bytes)
define hidden void @runtime.scopePush(ptr %payload, ptr %headSlot) {
entry:
  %isnil = icmp eq ptr %payload, null
  br i1 %isnil, label %done, label %push
push:
  %node = call ptr @calloc(i64 1, i64 16)
  %ok = icmp ne ptr %node, null
  br i1 %ok, label %fill, label %done
fill:
  store ptr %payload, ptr %node, align 8
  %nextSlot = getelementptr i8, ptr %node, i64 8
  %oldHead = load ptr, ptr %headSlot, align 8
  store ptr %oldHead, ptr %nextSlot, align 8
  store ptr %node, ptr %headSlot, align 8
  br label %done
done:
  ret void
}

; scopeFreeTo: walk from head, free each payload+node, stop at sentinel.
define hidden ptr @runtime.scopeFreeTo(ptr %head, ptr %sentinel) {
entry:
  br label %loop
loop:
  %cur = phi ptr [ %head, %entry ], [ %next, %freeself ]
  %atEnd = icmp eq ptr %cur, %sentinel
  br i1 %atEnd, label %ret, label %checknull
checknull:
  %isnull = icmp eq ptr %cur, null
  br i1 %isnull, label %ret, label %freenode
freenode:
  %payload = load ptr, ptr %cur, align 8
  %nextSlot = getelementptr i8, ptr %cur, i64 8
  %next0 = load ptr, ptr %nextSlot, align 8
  %paynil = icmp eq ptr %payload, null
  br i1 %paynil, label %freeself, label %freepay
freepay:
  call void @runtime.free(ptr %payload, ptr undef)
  br label %freeself
freeself:
  %next = phi ptr [ %next0, %freenode ], [ %next0, %freepay ]
  call void @free(ptr %cur)
  br label %loop
ret:
  ret ptr %sentinel
}

; scopeRelocate: move allocation from inner scope to outer scope.
define hidden void @runtime.scopeRelocate(ptr %payload, ptr %headSlot, ptr %saveSlot) {
entry:
  %isnil = icmp eq ptr %payload, null
  br i1 %isnil, label %done, label %start
start:
  %saved = load ptr, ptr %saveSlot, align 8
  %head = load ptr, ptr %headSlot, align 8
  %headIsSave = icmp eq ptr %head, %saved
  br i1 %headIsSave, label %done, label %headnullchk
headnullchk:
  %headNull = icmp eq ptr %head, null
  br i1 %headNull, label %done, label %checkhead
checkhead:
  %headPay = load ptr, ptr %head, align 8
  %headMatch = icmp eq ptr %headPay, %payload
  br i1 %headMatch, label %unlinkhead, label %search
unlinkhead:
  %headNext = getelementptr i8, ptr %head, i64 8
  %hn = load ptr, ptr %headNext, align 8
  store ptr %hn, ptr %headSlot, align 8
  %savIsNull = icmp eq ptr %saved, null
  br i1 %savIsNull, label %insertnull, label %insertafter
insertnull:
  store ptr null, ptr %headNext, align 8
  store ptr %head, ptr %saveSlot, align 8
  br label %done
insertafter:
  %savNext = getelementptr i8, ptr %saved, i64 8
  %sn = load ptr, ptr %savNext, align 8
  store ptr %sn, ptr %headNext, align 8
  store ptr %head, ptr %savNext, align 8
  br label %done
search:
  br label %sloop
sloop:
  %prev = phi ptr [ %head, %search ], [ %scur, %snext ]
  %prevNextSlot = getelementptr i8, ptr %prev, i64 8
  %scur = load ptr, ptr %prevNextSlot, align 8
  %curDone = icmp eq ptr %scur, %saved
  br i1 %curDone, label %done, label %schecknull
schecknull:
  %curNull = icmp eq ptr %scur, null
  br i1 %curNull, label %done, label %smatch
smatch:
  %curPay = load ptr, ptr %scur, align 8
  %curMatch = icmp eq ptr %curPay, %payload
  br i1 %curMatch, label %unlinkmid, label %snext
snext:
  br label %sloop
unlinkmid:
  %curNextSlot = getelementptr i8, ptr %scur, i64 8
  %cn = load ptr, ptr %curNextSlot, align 8
  store ptr %cn, ptr %prevNextSlot, align 8
  %savIsNull2 = icmp eq ptr %saved, null
  br i1 %savIsNull2, label %insertnull2, label %insertafter2
insertnull2:
  store ptr null, ptr %curNextSlot, align 8
  store ptr %scur, ptr %saveSlot, align 8
  br label %done
insertafter2:
  %savNext2 = getelementptr i8, ptr %saved, i64 8
  %sn2 = load ptr, ptr %savNext2, align 8
  store ptr %sn2, ptr %curNextSlot, align 8
  store ptr %scur, ptr %savNext2, align 8
  br label %done
done:
  ret void
}

; scopeEscapeReturn: null out payload so scopeFreeTo won't free it.
define hidden void @runtime.scopeEscapeReturn(ptr %payload, ptr %headSlot) {
entry:
  %isnil = icmp eq ptr %payload, null
  br i1 %isnil, label %done, label %start
start:
  %head = load ptr, ptr %headSlot, align 8
  br label %loop
loop:
  %cur = phi ptr [ %head, %start ], [ %next, %nomatch ]
  %curnil = icmp eq ptr %cur, null
  br i1 %curnil, label %done, label %check
check:
  %curPay = load ptr, ptr %cur, align 8
  %match = icmp eq ptr %curPay, %payload
  br i1 %match, label %nullify, label %nomatch
nullify:
  store ptr null, ptr %cur, align 8
  br label %done
nomatch:
  %nextSlot = getelementptr i8, ptr %cur, i64 8
  %next = load ptr, ptr %nextSlot, align 8
  br label %loop
done:
  ret void
}

attributes #0 = { noinline nounwind allockind("alloc,zeroed") allocsize(0) uwtable(sync) "alloc-family"="runtime.alloc" "target-cpu"="x86-64" "target-features"="+cmov,+cx8,+fxsr,+mmx,+sse,+sse2,+x87" }
attributes #1 = { noinline nounwind uwtable(sync) "target-cpu"="x86-64" "target-features"="+cmov,+cx8,+fxsr,+mmx,+sse,+sse2,+x87" }
