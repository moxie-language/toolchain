; Diagnostic override for runtime.alloc - tracks allocation/free/scope stats.
; Link with --override= to replace dealloc_freelist.bc's runtime.alloc/free/scopePush/scopeFreeTo.
; Prints summary to stderr on exit via a destructor.

target datalayout = "e-m:e-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128"
target triple = "x86_64-unknown-linux-musleabihf"

@runtime.zeroSizedAlloc = external hidden global i8

declare ptr @calloc(i64, i64)
declare void @free(ptr)
declare i64 @write(i32, ptr, i64)
declare void @exit(i32) noreturn
declare i32 @snprintf(ptr, i64, ptr, ...)
declare ptr @llvm.returnaddress(i32)

; counters
@diag.alloc_count = internal global i64 0, align 8
@diag.alloc_bytes = internal global i64 0, align 8
@diag.free_count = internal global i64 0, align 8
@diag.free_bytes = internal global i64 0, align 8
@diag.peak_bytes = internal global i64 0, align 8
@diag.live_bytes = internal global i64 0, align 8
@diag.scope_push = internal global i64 0, align 8
@diag.scope_free = internal global i64 0, align 8
@diag.huge_count = internal global i64 0, align 8
@diag.huge_bytes = internal global i64 0, align 8

@diag.buf = internal global [512 x i8] zeroinitializer
@diag.fmt = internal constant [201 x i8] c"\0A=== ALLOC DIAG ===\0Aallocs:     %lu (%lu bytes)\0Afrees:      %lu (%lu bytes)\0Alive:       %lu bytes\0Apeak:       %lu bytes\0Ascope_push: %lu\0Ascope_free: %lu\0Ahuge(>1MB): %lu (%lu bytes)\0A===================\0A\00"
@diag.hugefmt = internal constant [33 x i8] c"[HUGE ALLOC] size=%lu caller=%p\0A\00"

define hidden noalias nonnull ptr @runtime.alloc(i64 %size, ptr %layout, ptr %context) #0 {
entry:
  %zero = icmp eq i64 %size, 0
  br i1 %zero, label %ret_zero, label %track

ret_zero:
  ret ptr @runtime.zeroSizedAlloc

track:
  ; update counters
  %ac = load i64, ptr @diag.alloc_count, align 8
  %ac1 = add i64 %ac, 1
  store i64 %ac1, ptr @diag.alloc_count, align 8

  %aligned = add i64 %size, 15
  %size16 = and i64 %aligned, -16

  %ab = load i64, ptr @diag.alloc_bytes, align 8
  %ab1 = add i64 %ab, %size16
  store i64 %ab1, ptr @diag.alloc_bytes, align 8

  %lb = load i64, ptr @diag.live_bytes, align 8
  %lb1 = add i64 %lb, %size16
  store i64 %lb1, ptr @diag.live_bytes, align 8

  %pk = load i64, ptr @diag.peak_bytes, align 8
  %higher = icmp ugt i64 %lb1, %pk
  br i1 %higher, label %update_peak, label %check_huge

update_peak:
  store i64 %lb1, ptr @diag.peak_bytes, align 8
  br label %check_huge

check_huge:
  %is_huge = icmp ugt i64 %size16, 1048576
  br i1 %is_huge, label %log_huge, label %do_alloc

log_huge:
  %hc = load i64, ptr @diag.huge_count, align 8
  store i64 0, ptr @diag.huge_count, align 8
  %hc1 = add i64 %hc, 1
  store i64 %hc1, ptr @diag.huge_count, align 8
  %hb = load i64, ptr @diag.huge_bytes, align 8
  %hb1 = add i64 %hb, %size16
  store i64 %hb1, ptr @diag.huge_bytes, align 8
  ; print to stderr
  %ra = call ptr @llvm.returnaddress(i32 0)
  %n = call i32 (ptr, i64, ptr, ...) @snprintf(
    ptr getelementptr ([512 x i8], ptr @diag.buf, i64 0, i64 0),
    i64 512,
    ptr getelementptr ([33 x i8], ptr @diag.hugefmt, i64 0, i64 0),
    i64 %size16, ptr %ra)
  %n64 = sext i32 %n to i64
  call i64 @write(i32 2, ptr getelementptr ([512 x i8], ptr @diag.buf, i64 0, i64 0), i64 %n64)
  br label %do_alloc

do_alloc:
  %total = add i64 %size16, 16
  %base = call ptr @calloc(i64 1, i64 %total)
  %ok = icmp ne ptr %base, null
  br i1 %ok, label %done, label %oom

oom:
  call void @exit(i32 134) noreturn
  unreachable

done:
  ; store magic + size in header for free tracking
  store i64 -6872988504188559820, ptr %base, align 8
  %szslot = getelementptr i8, ptr %base, i64 8
  store i64 %size16, ptr %szslot, align 8
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
  ; read size from header
  %szslot = getelementptr i8, ptr %ptr, i64 -8
  %sz = load i64, ptr %szslot, align 8
  ; update counters
  %fc = load i64, ptr @diag.free_count, align 8
  store i64 0, ptr @diag.free_count, align 8
  %fc1 = add i64 %fc, 1
  store i64 %fc1, ptr @diag.free_count, align 8
  %fb = load i64, ptr @diag.free_bytes, align 8
  %fb1 = add i64 %fb, %sz
  store i64 %fb1, ptr @diag.free_bytes, align 8
  %lb = load i64, ptr @diag.live_bytes, align 8
  %lb1 = sub i64 %lb, %sz
  store i64 %lb1, ptr @diag.live_bytes, align 8
  ; clear magic and free
  store i64 0, ptr %base, align 8
  call void @free(ptr %base)
  br label %done
done:
  ret void
}

define hidden void @runtime.scopePush(ptr %payload, ptr %headSlot) {
entry:
  %isnil = icmp eq ptr %payload, null
  br i1 %isnil, label %done, label %push
push:
  %sp = load i64, ptr @diag.scope_push, align 8
  %sp1 = add i64 %sp, 1
  store i64 %sp1, ptr @diag.scope_push, align 8
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

define hidden ptr @runtime.scopeFreeTo(ptr %head, ptr %sentinel) {
entry:
  br label %loop
loop:
  %cur = phi ptr [ %head, %entry ], [ %next, %freeself ]
  %done = icmp eq ptr %cur, %sentinel
  br i1 %done, label %ret, label %checknull
checknull:
  %isnull = icmp eq ptr %cur, null
  br i1 %isnull, label %ret, label %freenode
freenode:
  %sf = load i64, ptr @diag.scope_free, align 8
  %sf1 = add i64 %sf, 1
  store i64 %sf1, ptr @diag.scope_free, align 8
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

; print summary at exit
@llvm.global_ctors = appending global [0 x { i32, ptr, ptr }] zeroinitializer
@llvm.global_dtors = appending global [1 x { i32, ptr, ptr }] [{ i32, ptr, ptr } { i32 65535, ptr @diag.report, ptr null }]

define internal void @diag.report() {
  %ac = load i64, ptr @diag.alloc_count, align 8
  %ab = load i64, ptr @diag.alloc_bytes, align 8
  %fc = load i64, ptr @diag.free_count, align 8
  %fb = load i64, ptr @diag.free_bytes, align 8
  %lb = load i64, ptr @diag.live_bytes, align 8
  %pk = load i64, ptr @diag.peak_bytes, align 8
  %sp = load i64, ptr @diag.scope_push, align 8
  %sf = load i64, ptr @diag.scope_free, align 8
  %hc = load i64, ptr @diag.huge_count, align 8
  %hb = load i64, ptr @diag.huge_bytes, align 8
  %n = call i32 (ptr, i64, ptr, ...) @snprintf(
    ptr getelementptr ([512 x i8], ptr @diag.buf, i64 0, i64 0),
    i64 512,
    ptr getelementptr ([201 x i8], ptr @diag.fmt, i64 0, i64 0),
    i64 %ac, i64 %ab,
    i64 %fc, i64 %fb,
    i64 %lb,
    i64 %pk,
    i64 %sp,
    i64 %sf,
    i64 %hc, i64 %hb)
  %n64 = sext i32 %n to i64
  call i64 @write(i32 2, ptr getelementptr ([512 x i8], ptr @diag.buf, i64 0, i64 0), i64 %n64)
  ret void
}

attributes #0 = { noinline nounwind }
attributes #1 = { nounwind }
