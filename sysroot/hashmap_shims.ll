; ModuleID = '/home/mleku/s/moxie/sysroot/hashmap_shims.bc'
source_filename = "hashmap_shims.ll"
target datalayout = "e-m:e-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128"
target triple = "x86_64-pc-linux-gnu"

@io.EOF = local_unnamed_addr global { ptr, ptr } zeroinitializer
@io.ErrNoProgress = local_unnamed_addr global { ptr, ptr } zeroinitializer
@.str.eof = hidden constant [3 x i8] c"EOF"
@.str.enoprogress = hidden constant [43 x i8] c"multiple Read calls return no data or error"
@"reflect/types.type:pointer:named:bytes.Buffer" = local_unnamed_addr global i8 0

declare void @runtime.hashmapSet(ptr, ptr, ptr, i32, ptr) local_unnamed_addr

declare i1 @runtime.hashmapGet(ptr, ptr, ptr, i64, i32, ptr) local_unnamed_addr

; Function Attrs: mustprogress nofree norecurse nosync nounwind willreturn memory(argmem: read)
define i64 @runtime.hashmapLen(ptr readonly captures(address_is_null) %m) local_unnamed_addr #0 {
entry:
  %isnil = icmp eq ptr %m, null
  br i1 %isnil, label %common.ret, label %load

common.ret:                                       ; preds = %entry, %load
  %common.ret.op = phi i64 [ %count, %load ], [ 0, %entry ]
  ret i64 %common.ret.op

load:                                             ; preds = %entry
  %countptr = getelementptr i8, ptr %m, i64 16
  %count = load i64, ptr %countptr, align 4
  br label %common.ret
}

define void @runtime.hashmapBinarySet(ptr %m, ptr %key, ptr %value) local_unnamed_addr {
entry:
  %seedptr = getelementptr i8, ptr %m, i64 8
  %seed = load i64, ptr %seedptr, align 4
  %keysizeptr = getelementptr i8, ptr %m, i64 24
  %keysize = load i64, ptr %keysizeptr, align 4
  %hashfnptr = getelementptr i8, ptr %m, i64 72
  %hashfn = load ptr, ptr %hashfnptr, align 8
  %hash = tail call i32 %hashfn(ptr %key, i64 %keysize, i64 %seed)
  tail call void @runtime.hashmapSet(ptr %m, ptr %key, ptr %value, i32 %hash, ptr null)
  ret void
}

define i1 @runtime.hashmapBinaryGet(ptr %m, ptr %key, ptr %value, i64 %valueSize) local_unnamed_addr {
entry:
  %isnil = icmp eq ptr %m, null
  br i1 %isnil, label %common.ret, label %doit

common.ret:                                       ; preds = %entry, %doit
  %common.ret.op = phi i1 [ %ok, %doit ], [ false, %entry ]
  ret i1 %common.ret.op

doit:                                             ; preds = %entry
  %seedptr = getelementptr i8, ptr %m, i64 8
  %seed = load i64, ptr %seedptr, align 4
  %keysizeptr = getelementptr i8, ptr %m, i64 24
  %keysize = load i64, ptr %keysizeptr, align 4
  %hashfnptr = getelementptr i8, ptr %m, i64 72
  %hashfn = load ptr, ptr %hashfnptr, align 8
  %hash = tail call i32 %hashfn(ptr %key, i64 %keysize, i64 %seed)
  %ok = tail call i1 @runtime.hashmapGet(ptr nonnull %m, ptr %key, ptr %value, i64 %valueSize, i32 %hash, ptr null)
  br label %common.ret
}

declare void @runtime.hashmapDelete(ptr, ptr, i32, ptr) local_unnamed_addr

define void @runtime.hashmapBinaryDelete(ptr %m, ptr %key) local_unnamed_addr {
entry:
  %isnil = icmp eq ptr %m, null
  br i1 %isnil, label %done, label %doit

doit:
  %seedptr = getelementptr i8, ptr %m, i64 8
  %seed = load i64, ptr %seedptr, align 4
  %keysizeptr = getelementptr i8, ptr %m, i64 24
  %keysize = load i64, ptr %keysizeptr, align 4
  %hashfnptr = getelementptr i8, ptr %m, i64 72
  %hashfn = load ptr, ptr %hashfnptr, align 8
  %hash = tail call i32 %hashfn(ptr %key, i64 %keysize, i64 %seed)
  tail call void @runtime.hashmapDelete(ptr nonnull %m, ptr %key, i32 %hash, ptr null)
  br label %done

done:
  ret void
}

; Function Attrs: mustprogress nofree norecurse nounwind willreturn memory(argmem: read)
define i1 @runtime.stringLess(ptr readonly captures(none) %a.data, i64 %a.len, i64 %a.cap, ptr readonly captures(none) %b.data, i64 %b.len, i64 %b.cap) local_unnamed_addr #1 {
entry:
  %minlen = tail call i64 @llvm.umin.i64(i64 %a.len, i64 %b.len)
  %cmp = tail call i32 @memcmp(ptr %a.data, ptr %b.data, i64 %minlen)
  %islt = icmp slt i32 %cmp, 0
  br i1 %islt, label %common.ret, label %check

check:                                            ; preds = %entry
  %minlen.a = icmp ult i64 %a.len, %b.len
  %iseq = icmp eq i32 %cmp, 0
  %spec.select = select i1 %iseq, i1 %minlen.a, i1 false
  br label %common.ret

common.ret:                                       ; preds = %check, %entry
  %common.ret.op = phi i1 [ true, %entry ], [ %spec.select, %check ]
  ret i1 %common.ret.op
}

declare void @runtime.runtimePanicAt(ptr, ptr, i64, i64, ptr) local_unnamed_addr

; Function Attrs: noreturn
define void @runtime._panicstr(ptr %msg.data, i64 %msg.len, i64 %msg.cap) local_unnamed_addr #2 {
entry:
  tail call void @runtime.runtimePanicAt(ptr null, ptr %msg.data, i64 %msg.len, i64 %msg.cap, ptr null)
  unreachable
}

; Function Attrs: mustprogress nocallback nofree nounwind willreturn memory(argmem: read)
declare i32 @memcmp(ptr captures(none), ptr captures(none), i64) local_unnamed_addr #3

; Function Attrs: nocallback nofree nosync nounwind speculatable willreturn memory(none)
declare i64 @llvm.umin.i64(i64, i64) #4

attributes #0 = { mustprogress nofree norecurse nosync nounwind willreturn memory(argmem: read) }
attributes #1 = { mustprogress nofree norecurse nounwind willreturn memory(argmem: read) }
attributes #2 = { noreturn }
attributes #3 = { mustprogress nocallback nofree nounwind willreturn memory(argmem: read) }
attributes #4 = { nocallback nofree nosync nounwind speculatable willreturn memory(none) }
