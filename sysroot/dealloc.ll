target datalayout = "e-m:e-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128"
target triple = "x86_64-unknown-linux-musleabihf"

define void @runtime.deallocPush(ptr %ptr, ptr %head, ptr %entry) {
  ret void
}

define void @runtime.deallocFree(ptr %head) {
  ret void
}

define void @runtime.deallocEscape(ptr %ptr, ptr %head) {
  ret void
}
