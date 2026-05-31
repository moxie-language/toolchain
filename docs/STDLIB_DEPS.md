# Moxie Stdlib Dependency Order

Generated 2026-05-30. 306 sub-packages across 47 top-level packages.

## Tier 0: Zero Dependencies (leaves)

  cmp
  container/list
  container/ring
  crypto/internal/boring/sig
  crypto/internal/boring/syso
  crypto/internal/fips140/aes/gcm/_asm/gcm
  crypto/internal/fips140deps
  crypto/md5/_asm
  crypto/tls/internal/fips140tls
  encoding
  errors
  image/color
  internal/binary
  internal/bytealg
  internal/byteorder
  internal/cfg
  internal/coverage
  internal/coverage/calloc
  internal/coverage/uleb128
  internal/goexperiment
  internal/goversion
  internal/itoa
  internal/nettrace
  internal/platform
  internal/profilerecord
  internal/runtime/exithook
  internal/singleflight
  internal/syslist
  internal/trace/tracev2
  internal/trace/traceviewer/format
  jsbridge/webgl
  log/internal
  runtime/cgo
  runtime/internal/sys
  runtime/metrics
  runtime/volatile
  structs
  unicode
  unicode/utf16
  unicode/utf8
  unsafe

## Tier 1: Shallow Dependencies (1-6 transitive, no cycles)

  builtin                                             d= 1 t= 1  <- cmp
  crypto/hpke                                         d= 1 t= 1  <- errors
  crypto/internal/boring/bcache                       d= 1 t= 1  <- unsafe
  crypto/internal/fips140/alias                       d= 1 t= 1  <- unsafe
  crypto/internal/fips140/check                       d= 1 t= 1  <- unsafe
  crypto/internal/fips140deps/byteorder               d= 1 t= 1  <- internal/byteorder
  internal/abi                                        d= 1 t= 1  <- unsafe
  internal/asan                                       d= 1 t= 1  <- unsafe
  internal/coverage/rtcov                             d= 1 t= 1  <- unsafe
  internal/cpu                                        d= 1 t= 1  <- unsafe
  internal/gclayout                                   d= 1 t= 1  <- unsafe
  internal/gover                                      d= 1 t= 1  <- cmp
  internal/msan                                       d= 1 t= 1  <- unsafe
  internal/oserror                                    d= 1 t= 1  <- errors
  internal/testlog                                    d= 1 t= 1  <- unsafe
  internal/unsafeheader                               d= 1 t= 1  <- unsafe
  io                                                  d= 1 t= 1  <- errors
  jsbridge/app                                        d= 1 t= 1  <- unsafe
  jsbridge/text                                       d= 1 t= 1  <- unsafe
  runtime/interrupt                                   d= 1 t= 1  <- unsafe
  sync/atomic                                         d= 1 t= 1  <- unsafe
  unique                                              d= 1 t= 1  <- unsafe
  hash                                                d= 1 t= 2  <- io
  internal/cm                                         d= 2 t= 2  <- structs, unsafe
  internal/race                                       d= 2 t= 2  <- internal/abi, unsafe
  internal/stringslite                                d= 2 t= 2  <- internal/bytealg, unsafe
  internal/synctest                                   d= 2 t= 2  <- internal/abi, unsafe
  internal/task                                       d= 2 t= 2  <- runtime/interrupt, unsafe
  runtime/pprof                                       d= 2 t= 2  <- errors, io
  internal/saferio                                    d= 2 t= 3  <- io, unsafe
  sync                                                d= 2 t= 3  <- internal/task, unsafe
  path                                                d= 3 t= 3  <- errors, internal/bytealg, unicode/utf8
  hash/adler32                                        d= 3 t= 4  <- errors, hash, internal/byteorder
  hash/crc64                                          d= 3 t= 4  <- errors, hash, internal/byteorder
  crypto/internal/fips140/ssh                         d= 2 t= 5  <- crypto/internal/fips140/check, hash
  hash/crc32                                          d= 5 t= 6  <- errors, hash, internal/byteorder, internal/cpu, unsafe

## Tier 2: Core Cycle (112 packages)

These packages form a single strongly-connected component.
Build order within the cycle requires the compiler to handle
circular imports (as Go/TinyGo/Moxie already do).

Sorted by direct dependency count (least to most):

  crypto/internal/entropy                                  d= 1  <- crypto/internal/sysrand
  os/exec                                                  d= 1  <- os
  context                                                  d= 2  <- errors, time
  crypto/internal/fips140cache                             d= 2  <- runtime, weak
  crypto/internal/fips140deps/cpu                          d= 2  <- internal/cpu, internal/goarch
  crypto/internal/randutil                                 d= 2  <- io, math/rand/v2
  crypto/mlkem                                             d= 2  <- crypto, crypto/internal/fips140/mlkem
  internal/runtime/sys                                     d= 2  <- internal/cpu, internal/goarch
  internal/runtime/syscall                                 d= 2  <- internal/goarch, unsafe
  maps                                                     d= 2  <- iter, unsafe
  net/http/internal/ascii                                  d= 2  <- bytes, unicode
  crypto/internal/boring/bbig                              d= 3  <- crypto/internal/boring, math/big, unsafe
  crypto/internal/fips140/hkdf                             d= 3  <- crypto/internal/fips140, crypto/internal/fips140/hmac, hash
  crypto/internal/fips140/tls13                            d= 3  <- crypto/internal/fips140/hkdf, crypto/internal/fips140deps/byteorder, hash
  crypto/sha256                                            d= 3  <- crypto/internal/boring, crypto/internal/fips140/sha256, hash
  crypto/sha3                                              d= 3  <- crypto/internal/fips140/sha3, hash, unsafe
  crypto/sha512                                            d= 3  <- crypto/internal/boring, crypto/internal/fips140/sha512, hash
  crypto/subtle                                            d= 3  <- crypto/internal/fips140/subtle, internal/runtime/sys, runtime
  internal/syscall/unix                                    d= 3  <- runtime, syscall, unsafe
  iter                                                     d= 3  <- internal/race, runtime, unsafe
  math                                                     d= 3  <- internal/cpu, math/bits, unsafe
  math/rand                                                d= 3  <- fmt, math, unsafe
  syscall/js                                               d= 3  <- internal/synctest, runtime, unsafe
  text/tabwriter                                           d= 3  <- fmt, io, unicode/utf8
  time/tzdata                                              d= 3  <- errors, syscall, unsafe
  weak                                                     d= 3  <- internal/abi, runtime, unsafe
  bufio                                                    d= 4  <- bytes, errors, io, unicode/utf8
  crypto/aes                                               d= 4  <- crypto/cipher, crypto/internal/boring, crypto/internal/fips140/aes, strconv
  crypto/elliptic                                          d= 4  <- crypto/internal/fips140/nistec, errors, io, math/big
  crypto/internal/fips140                                  d= 4  <- bytes, errors, runtime, unsafe
  crypto/internal/fips140hash                              d= 4  <- crypto/internal/fips140/sha3, crypto/sha3, hash, unsafe
  crypto/rand                                              d= 4  <- errors, io, math/big, syscall
  crypto/rc4                                               d= 4  <- crypto/internal/fips140/alias, crypto/internal/fips140only, errors, strconv
  encoding/base64                                          d= 4  <- internal/byteorder, io, slices, strconv
  encoding/binary                                          d= 4  <- errors, io, math, slices
  internal/chacha8rand                                     d= 4  <- internal/byteorder, internal/cpu, internal/goarch, unsafe
  internal/goarch                                          d= 4  <- bytes, fmt, log, os
  mime/quotedprintable                                     d= 4  <- bufio, bytes, fmt, io
  slices                                                   d= 4  <- cmp, iter, math/bits, unsafe
  crypto/dsa                                               d= 5  <- crypto/internal/fips140only, crypto/internal/randutil, errors, io, math/big
  crypto/hkdf                                              d= 5  <- crypto/internal/fips140/hkdf, crypto/internal/fips140hash, crypto/internal/fips140only, errors, hash
  crypto/internal/fips140/edwards25519/field               d= 5  <- crypto/internal/fips140/check, crypto/internal/fips140/subtle, crypto/internal/fips140deps/byteorder, errors, math/bits
  crypto/internal/fips140/subtle                           d= 5  <- crypto/internal/fips140/alias, crypto/internal/fips140deps/byteorder, math/bits, runtime, unsafe
  crypto/internal/fips140/tls12                            d= 5  <- crypto/internal/fips140, crypto/internal/fips140/hmac, crypto/internal/fips140/sha256, crypto/internal/fips140/sha512, hash
  crypto/x509/pkix                                         d= 5  <- encoding/asn1, encoding/hex, fmt, math/big, time
  encoding/hex                                             d= 5  <- bytes, errors, fmt, io, slices
  encoding/pem                                             d= 5  <- bytes, encoding/base64, errors, io, slices
  go/build/constraint                                      d= 5  <- bytes, errors, strconv, unicode, unicode/utf8
  go/format                                                d= 5  <- bytes, fmt, go/printer, go/token, io
  internal/filepathlite                                    d= 5  <- errors, internal/bytealg, internal/stringslite, io/fs, slices
  net/http/internal                                        d= 5  <- bufio, bytes, errors, fmt, io
  crypto/des                                               d= 6  <- crypto/cipher, crypto/internal/fips140/alias, crypto/internal/fips140only, errors, internal/byteorder, strconv
  crypto/internal/fips140/bigmod                           d= 6  <- crypto/internal/fips140/check, crypto/internal/fips140deps/byteorder, crypto/internal/fips140deps/cpu, errors, math/bits, unsafe
  crypto/internal/fips140/edwards25519                     d= 6  <- crypto/internal/fips140/check, crypto/internal/fips140/edwards25519/field, crypto/internal/fips140/subtle, crypto/internal/fips140deps/byteorder, errors, math/bits
  crypto/internal/fips140/hmac                             d= 6  <- crypto/internal/fips140, crypto/internal/fips140/sha256, crypto/internal/fips140/sha3, crypto/internal/fips140/sha512, errors, hash
  crypto/internal/fips140/sha256                           d= 6  <- crypto/internal/fips140, crypto/internal/fips140deps/byteorder, crypto/internal/fips140deps/cpu, errors, hash, math/bits
  crypto/internal/fips140/sha512                           d= 6  <- crypto/internal/fips140, crypto/internal/fips140deps/byteorder, crypto/internal/fips140deps/cpu, errors, hash, math/bits
  crypto/internal/fips140only                              d= 6  <- crypto/internal/fips140/drbg, crypto/internal/fips140/sha256, crypto/internal/fips140/sha3, crypto/internal/fips140/sha512, hash, io
  crypto/sha1                                              d= 6  <- crypto/internal/fips140only, errors, hash, internal/byteorder, internal/cpu, math/bits
  math/bits                                                d= 6  <- bytes, fmt, go/format, io, log, os
  math/rand/v2                                             d= 6  <- errors, internal/byteorder, internal/chacha8rand, math, math/bits, unsafe
  net/http/httptrace                                       d= 6  <- context, crypto/tls, internal/nettrace, net, net/textproto, time
  crypto/hmac                                              d= 7  <- crypto/internal/boring, crypto/internal/fips140/hmac, crypto/internal/fips140hash, crypto/internal/fips140only, crypto/sha256, crypto/subtle, hash
  crypto/internal/sysrand                                  d= 7  <- errors, internal/syscall/unix, math, os, syscall, time, unsafe
  encoding/csv                                             d= 7  <- bufio, bytes, errors, fmt, io, unicode, unicode/utf8
  fmt                                                      d= 7  <- errors, io, math, os, slices, strconv, unicode/utf8
  go/token                                                 d= 7  <- cmp, fmt, iter, slices, strconv, unicode, unicode/utf8
  log                                                      d= 7  <- fmt, io, log/internal, os, runtime, time, unsafe
  runtime                                                  d= 7  <- internal/binary, internal/gclayout, internal/task, math/bits, runtime/interrupt, runtime/volatile, unsafe
  sort                                                     d= 7  <- bytes, fmt, go/format, log, math/bits, os, slices
  bytes                                                    d= 8  <- errors, internal/bytealg, io, iter, math/bits, unicode, unicode/utf8, unsafe
  compress/flate                                           d= 8  <- bufio, errors, fmt, io, math, math/bits, sort, strconv
  compress/gzip                                            d= 8  <- bufio, compress/flate, encoding/binary, errors, fmt, hash/crc32, io, time
  crypto                                                   d= 8  <- crypto/internal/fips140/sha256, crypto/internal/fips140/sha3, crypto/internal/fips140/sha512, crypto/md5, crypto/sha1, hash, io, strconv
  crypto/internal/fips140/ed25519                          d= 8  <- bytes, crypto/internal/fips140, crypto/internal/fips140/check, crypto/internal/fips140/drbg, crypto/internal/fips140/edwards25519, crypto/internal/fips140/sha512, errors, strconv
  crypto/internal/fips140/sha3                             d= 8  <- bytes, crypto/internal/fips140, crypto/internal/fips140/subtle, crypto/internal/fips140deps/byteorder, crypto/internal/fips140deps/cpu, errors, math/bits, unsafe
  internal/poll                                            d= 8  <- errors, internal/itoa, internal/syscall/unix, io, runtime, syscall, time, unsafe
  io/fs                                                    d= 8  <- errors, internal/bytealg, internal/oserror, io, path, slices, time, unicode/utf8
  crypto/cipher                                            d= 9  <- bytes, crypto/internal/fips140/aes, crypto/internal/fips140/aes/gcm, crypto/internal/fips140/alias, crypto/internal/fips140only, crypto/subtle, errors, internal/byteorder, io
  crypto/ed25519                                           d= 9  <- crypto, crypto/internal/fips140/ed25519, crypto/internal/fips140cache, crypto/internal/fips140only, crypto/rand, crypto/subtle, errors, io, strconv
  crypto/internal/fips140/aes/gcm                          d= 9  <- crypto/internal/fips140, crypto/internal/fips140/aes, crypto/internal/fips140/alias, crypto/internal/fips140/drbg, crypto/internal/fips140/subtle, crypto/internal/fips140deps/byteorder, crypto/internal/fips140deps/cpu, errors, math
  crypto/internal/fips140/drbg                             d= 9  <- crypto/internal/entropy, crypto/internal/fips140, crypto/internal/fips140/aes, crypto/internal/fips140/subtle, crypto/internal/fips140deps/byteorder, crypto/internal/randutil, crypto/internal/sysrand, io, math/bits
  crypto/internal/fips140/ecdh                             d= 9  <- bytes, crypto/internal/fips140, crypto/internal/fips140/check, crypto/internal/fips140/drbg, crypto/internal/fips140/nistec, crypto/internal/fips140deps/byteorder, errors, io, math/bits
  crypto/md5                                               d= 9  <- bytes, crypto/internal/fips140only, errors, go/format, hash, internal/byteorder, log, math/bits, os
  net/http/internal/httpcommon                             d= 9  <- bytes, context, errors, fmt, net/http/httptrace, net/textproto, net/url, sort, strconv
  net/netip                                                d= 9  <- cmp, errors, internal/bytealg, internal/byteorder, internal/itoa, math, math/bits, strconv, unique
  net/textproto                                            d= 9  <- bufio, bytes, errors, fmt, io, math, net, strconv, unsafe
  net/url                                                  d= 9  <- bytes, errors, fmt, maps, net/netip, path, slices, strconv, unsafe
  os                                                       d= 9  <- errors, internal/itoa, internal/testlog, io, io/fs, runtime, syscall, time, unsafe
  path/filepath                                            d= 9  <- bytes, errors, internal/bytealg, internal/filepathlite, io/fs, os, slices, syscall, unicode/utf8
  crypto/ecdh                                              d=10  <- bytes, crypto, crypto/internal/boring, crypto/internal/fips140/ecdh, crypto/internal/fips140/edwards25519/field, crypto/internal/fips140only, crypto/internal/randutil, crypto/subtle, errors, io
  crypto/internal/fips140/aes                              d=10  <- bytes, crypto/internal/fips140, crypto/internal/fips140/alias, crypto/internal/fips140/subtle, crypto/internal/fips140deps/byteorder, crypto/internal/fips140deps/cpu, fmt, math/bits, os, strconv
  crypto/internal/fips140/nistec/fiat                      d=10  <- bytes, crypto/internal/fips140/check, crypto/internal/fips140/subtle, errors, go/format, io, log, math/bits, os, os/exec
  crypto/internal/fips140/ecdsa                            d=11  <- bytes, crypto/internal/fips140, crypto/internal/fips140/bigmod, crypto/internal/fips140/check, crypto/internal/fips140/drbg, crypto/internal/fips140/hmac, crypto/internal/fips140/nistec, crypto/internal/fips140/sha512, errors, hash, io
  crypto/internal/fips140/mlkem                            d=11  <- bytes, crypto/internal/fips140, crypto/internal/fips140/drbg, crypto/internal/fips140/sha3, crypto/internal/fips140/subtle, crypto/internal/fips140deps/byteorder, errors, go/format, go/token, log, os
  mime                                                     d=11  <- bufio, bytes, encoding/base64, errors, fmt, io, maps, os, slices, unicode, unicode/utf8
  crypto/internal/fips140/rsa                              d=12  <- bytes, crypto/internal/fips140, crypto/internal/fips140/bigmod, crypto/internal/fips140/check, crypto/internal/fips140/drbg, crypto/internal/fips140/sha256, crypto/internal/fips140/sha3, crypto/internal/fips140/sha512, crypto/internal/fips140/subtle, errors, hash, io
  encoding/asn1                                            d=12  <- bytes, errors, fmt, internal/saferio, math, math/big, slices, strconv, time, unicode/utf16, unicode/utf8, unsafe
  go/printer                                               d=12  <- bytes, fmt, go/build/constraint, go/token, io, math, os, slices, strconv, text/tabwriter, unicode, unicode/utf8
  strconv                                                  d=13  <- bytes, errors, fmt, go/format, internal/bytealg, internal/stringslite, log, math, math/bits, os, slices, unicode, unicode/utf8
  crypto/internal/boring                                   d=15  <- bytes, crypto, crypto/cipher, crypto/internal/boring/sig, crypto/internal/boring/syso, crypto/internal/fips140, crypto/subtle, errors, hash, internal/byteorder, internal/stringslite, math/bits, runtime, strconv, unsafe
  math/big                                                 d=15  <- bytes, encoding/csv, errors, fmt, internal/byteorder, internal/cpu, io, log, math, math/bits, math/rand, os, slices, strconv, unsafe
  mime/multipart                                           d=15  <- bufio, bytes, crypto/rand, errors, fmt, io, maps, math, mime, mime/quotedprintable, net/textproto, os, path/filepath, slices, unsafe
  crypto/rsa                                               d=17  <- crypto, crypto/internal/boring, crypto/internal/boring/bbig, crypto/internal/boring/bcache, crypto/internal/fips140/bigmod, crypto/internal/fips140/rsa, crypto/internal/fips140hash, crypto/internal/fips140only, crypto/internal/randutil, crypto/rand, crypto/subtle, errors, fmt, hash, io, math, math/big
  time                                                     d=17  <- bytes, errors, go/format, internal/bytealg, internal/itoa, internal/stringslite, io, log, math/bits, net/http, os, runtime, slices, syscall, time/tzdata, unicode/utf8, unsafe
  crypto/ecdsa                                             d=18  <- crypto, crypto/ecdh, crypto/elliptic, crypto/internal/boring, crypto/internal/boring/bbig, crypto/internal/boring/bcache, crypto/internal/fips140/ecdsa, crypto/internal/fips140/nistec, crypto/internal/fips140cache, crypto/internal/fips140hash, crypto/internal/fips140only, crypto/internal/randutil, crypto/sha512, crypto/subtle, errors, io, math/big, math/rand/v2
  crypto/internal/fips140/nistec                           d=18  <- bytes, crypto/elliptic, crypto/internal/fips140/check, crypto/internal/fips140/nistec/fiat, crypto/internal/fips140/subtle, crypto/internal/fips140deps/byteorder, crypto/internal/fips140deps/cpu, errors, fmt, go/format, io, log, math/big, math/bits, os, os/exec, runtime, unsafe
  syscall                                                  d=19  <- bytes, errors, fmt, go/format, internal/asan, internal/bytealg, internal/byteorder, internal/goarch, internal/itoa, internal/msan, internal/oserror, internal/race, internal/runtime/syscall, io, log, os, runtime, slices, unsafe
  net                                                      d=20  <- cmp, context, errors, fmt, internal/bytealg, internal/itoa, internal/nettrace, internal/poll, internal/singleflight, internal/stringslite, internal/syscall/unix, io, io/fs, net/netip, os, runtime, slices, syscall, time, unsafe
  crypto/x509                                              d=38  <- bytes, crypto, crypto/aes, crypto/cipher, crypto/des, crypto/dsa, crypto/ecdh, crypto/ecdsa, crypto/ed25519, crypto/elliptic, crypto/md5, crypto/rand, crypto/rsa, crypto/sha256, crypto/x509/pkix, encoding/asn1, encoding/hex, encoding/pem, errors, fmt, io, io/fs, iter, maps, math, math/big, math/bits, net, net/netip, net/url, os, path/filepath, strconv, time, unicode, unicode/utf16, unicode/utf8, unsafe
  net/http                                                 d=39  <- bufio, bytes, compress/gzip, container/list, context, crypto/rand, crypto/tls, encoding/base64, encoding/binary, errors, fmt, io, io/fs, log, maps, math, math/rand, mime, mime/multipart, net, net/http/httptrace, net/http/internal, net/http/internal/ascii, net/http/internal/httpcommon, net/textproto, net/url, os, path, path/filepath, runtime, slices, sort, strconv, syscall, syscall/js, time, unicode, unicode/utf8, unsafe
  crypto/tls                                               d=44  <- bytes, container/list, context, crypto, crypto/aes, crypto/cipher, crypto/des, crypto/ecdh, crypto/ecdsa, crypto/ed25519, crypto/elliptic, crypto/hkdf, crypto/hmac, crypto/hpke, crypto/internal/boring, crypto/internal/fips140/aes, crypto/internal/fips140/aes/gcm, crypto/internal/fips140/tls12, crypto/internal/fips140/tls13, crypto/md5, crypto/mlkem, crypto/rand, crypto/rc4, crypto/rsa, crypto/sha1, crypto/sha256, crypto/sha512, crypto/subtle, crypto/tls/internal/fips140tls, crypto/x509, encoding/pem, errors, fmt, hash, internal/byteorder, internal/cpu, io, net, os, runtime, slices, strconv, time, unsafe

## Tier 3: Post-cycle packages (depend on the core cycle)

Sorted by transitive dependency count:

  container/heap                                           d= 1 t=153  <- sort
  crypto/boring                                            d= 1 t=153  <- crypto/internal/boring
  crypto/internal/fips140/aes/_asm/ctr                     d= 1 t=153  <- fmt
  crypto/internal/fips140/bigmod/_asm                      d= 1 t=153  <- strconv
  crypto/internal/fips140/edwards25519/field/_asm          d= 1 t=153  <- fmt
  crypto/internal/fips140/sha256/_asm                      d= 1 t=153  <- os
  crypto/internal/fips140/sha3/_asm                        d= 1 t=153  <- os
  crypto/internal/fips140/sha512/_asm                      d= 1 t=153  <- os
  crypto/internal/impl                                     d= 1 t=153  <- bytes
  crypto/sha1/_asm                                         d= 1 t=153  <- fmt
  internal/bisect                                          d= 1 t=153  <- runtime
  internal/runtime/math                                    d= 1 t=153  <- internal/goarch
  internal/syscall/execenv                                 d= 1 t=153  <- syscall
  net/http/internal/testcert                               d= 1 t=153  <- bytes
  crypto/ec/bech32                                         d= 2 t=153  <- bytes, fmt
  crypto/fips140                                           d= 2 t=153  <- crypto/internal/fips140, crypto/internal/fips140/check
  crypto/internal/fips140/aes/_asm/standard                d= 2 t=153  <- bytes, os
  crypto/internal/fips140/nistec/_asm                      d= 2 t=153  <- bytes, os
  crypto/internal/rand                                     d= 2 t=153  <- crypto/rand, io
  crypto/internal/sysrand/internal/seccomp                 d= 2 t=153  <- errors, fmt
  crypto/x509/internal/macos                               d= 2 t=153  <- errors, time
  encoding/ascii85                                         d= 2 t=153  <- io, strconv
  evloop                                                   d= 2 t=153  <- fmt, syscall
  html                                                     d= 2 t=153  <- bytes, unicode/utf8
  internal/coverage/slicewriter                            d= 2 t=153  <- fmt, io
  internal/lazyregexp                                      d= 2 t=153  <- bytes, os
  math/cmplx                                               d= 2 t=153  <- math, math/bits
  net/internal/socktest                                    d= 2 t=153  <- fmt, syscall
  testing                                                  d= 2 t=153  <- fmt, os
  encoding/base32                                          d= 3 t=153  <- io, slices, strconv
  encoding/base58                                          d= 3 t=153  <- crypto/sha256, errors, math/big
  internal/diff                                            d= 3 t=153  <- bytes, fmt, sort
  internal/lazytemplate                                    d= 3 t=153  <- bytes, io, os
  internal/obscuretestdata                                 d= 3 t=153  <- encoding/base64, io, os
  internal/runtime/atomic                                  d= 3 t=153  <- internal/cpu, internal/goarch, unsafe
  internal/testpty                                         d= 3 t=153  <- errors, fmt, os
  internal/txtar                                           d= 3 t=153  <- bytes, fmt, os
  runtime/trace                                            d= 3 t=153  <- context, errors, io
  compress/bzip2                                           d= 4 t=153  <- bufio, cmp, io, slices
  compress/lzw                                             d= 4 t=153  <- bufio, errors, fmt, io
  crypto/ec/chainhash                                      d= 4 t=153  <- crypto/sha256, encoding/hex, errors, fmt
  crypto/internal/fips140/pbkdf2                           d= 4 t=153  <- crypto/internal/fips140, crypto/internal/fips140/hmac, errors, hash
  hash/fnv                                                 d= 4 t=153  <- errors, hash, internal/byteorder, math/bits
  internal/coverage/slicereader                            d= 4 t=153  <- encoding/binary, fmt, io, unsafe
  internal/dag                                             d= 4 t=153  <- bytes, cmp, fmt, slices
  internal/goos                                            d= 4 t=153  <- bytes, fmt, log, os
  internal/goroot                                          d= 4 t=153  <- bytes, os, os/exec, path/filepath
  debug/gosym                                              d= 5 t=153  <- bytes, encoding/binary, fmt, sort, strconv
  encoding/json                                            d= 5 t=153  <- bytes, errors, math, strconv, unsafe
  internal/zstd                                            d= 5 t=153  <- encoding/binary, errors, fmt, io, math/bits
  io/ioutil                                                d= 5 t=153  <- bytes, io, io/fs, os, slices
  math/big/internal/asmgen                                 d= 5 t=153  <- bytes, cmp, fmt, math/bits, slices
  runtime/debug                                            d= 5 t=153  <- bytes, fmt, runtime, strconv, time
  embed                                                    d= 6 t=153  <- errors, internal/bytealg, internal/stringslite, io, io/fs, time
  text/scanner                                             d= 6 t=153  <- bytes, fmt, io, os, unicode, unicode/utf8
  log/syslog                                               d= 7 t=153  <- bytes, errors, fmt, log, net, os, time
  debug/dwarf                                              d= 8 t=153  <- bytes, encoding/binary, errors, fmt, io, path, sort, strconv
  go/constant                                              d= 8 t=153  <- bytes, fmt, go/token, math, math/big, math/bits, strconv, unicode/utf8
  internal/profile                                         d= 8 t=153  <- bytes, compress/gzip, errors, fmt, io, sort, strconv, time
  crypto/internal/hpke                                     d= 9 t=153  <- crypto, crypto/aes, crypto/cipher, crypto/ecdh, crypto/hkdf, crypto/rand, errors, internal/byteorder, math/bits
  go/scanner                                               d= 9 t=153  <- bytes, fmt, go/token, io, path/filepath, sort, strconv, unicode, unicode/utf8
  index/suffixarray                                        d= 9 t=153  <- bytes, encoding/binary, errors, io, log, math, os, slices, sort
  internal/runtime/gc                                      d= 9 t=153  <- bytes, fmt, go/format, internal/goarch, io, log, math, math/bits, os
  os/user                                                  d=10 t=153  <- bufio, bytes, errors, fmt, io, os, runtime, strconv, syscall, unsafe
  strings                                                  d=10 t=153  <- errors, internal/abi, internal/bytealg, internal/stringslite, io, iter, math/bits, unicode, unicode/utf8, unsafe
  net/http/cookiejar                                       d=11 t=153  <- bytes, cmp, errors, fmt, net, net/http, net/http/internal/ascii, net/url, slices, time, unicode/utf8
  archive/zip                                              d=15 t=153  <- bufio, bytes, compress/flate, encoding/binary, errors, fmt, hash, hash/crc32, io, io/fs, os, path, slices, time, unicode/utf8
  internal/runtime/strconv                                 d= 1 t=154  <- internal/runtime/math
  crypto/siphash                                           d= 2 t=154  <- math/bits, testing
  go/version                                               d= 2 t=154  <- bytes, internal/gover
  internal/coverage/cmerge                                 d= 3 t=154  <- fmt, internal/coverage, math
  internal/trace/version                                   d= 3 t=154  <- fmt, internal/trace/tracev2, io
  crypto/secp256k1                                         d= 4 t=154  <- bytes, crypto/sha256, errors, testing
  crypto/pbkdf2                                            d= 5 t=154  <- crypto/internal/fips140/pbkdf2, crypto/internal/fips140hash, crypto/internal/fips140only, errors, hash
  internal/testhash                                        d= 6 t=154  <- bytes, hash, io, math/rand, testing, time
  image/color/palette                                      d= 7 t=154  <- bytes, fmt, go/format, image/color, io, log, os
  compress/zlib                                            d= 8 t=154  <- bufio, compress/flate, encoding/binary, errors, fmt, hash, hash/adler32, io
  internal/coverage/pods                                   d= 8 t=154  <- bytes, cmp, fmt, internal/coverage, os, path/filepath, slices, strconv
  internal/runtime/maps                                    d= 8 t=154  <- internal/abi, internal/asan, internal/goarch, internal/msan, internal/race, internal/runtime/math, internal/runtime/sys, unsafe
  internal/xcoff                                           d= 9 t=154  <- bytes, debug/dwarf, encoding/binary, errors, fmt, internal/saferio, io, os, strconv
  crypto/ec/secp256k1                                      d=12 t=154  <- bytes, crypto/ecdsa, crypto/elliptic, crypto/rand, crypto/sha256, embed, encoding/hex, fmt, hash, io, math/big, math/bits
  internal/pkgbits                                         d=12 t=154  <- bytes, crypto/sha256, encoding/binary, errors, fmt, go/constant, go/token, io, math/big, os, runtime, strconv
  archive/tar                                              d=13 t=154  <- bytes, errors, fmt, io, io/fs, maps, math, os/user, path, slices, strconv, syscall, time
  crypto/ec                                                d= 2 t=155  <- crypto/ec/secp256k1, fmt
  crypto/ec/ecdsa                                          d= 2 t=155  <- crypto/ec/secp256k1, fmt
  internal/coverage/stringtab                              d= 4 t=155  <- fmt, internal/coverage/slicereader, internal/coverage/uleb128, io
  internal/runtime/cgroup                                  d= 4 t=155  <- internal/bytealg, internal/runtime/strconv, internal/runtime/syscall, unsafe
  crypto/bip32                                             d= 5 t=155  <- bytes, crypto/hmac, crypto/secp256k1, crypto/sha512, testing
  internal/trace/raw                                       d= 9 t=155  <- bufio, bytes, encoding/binary, fmt, internal/trace/tracev2, internal/trace/version, io, strconv, unicode
  internal/coverage/cformat                                d=10 t=155  <- bytes, cmp, fmt, internal/coverage, internal/coverage/cmerge, io, maps, slices, sort, text/tabwriter
  internal/trace/internal/tracev1                          d=10 t=155  <- bytes, cmp, encoding/binary, errors, fmt, internal/trace/version, io, math, slices, sort
  crypto/bip39                                             d= 5 t=156  <- bytes, crypto/pbkdf2, crypto/sha256, crypto/sha512, testing
  internal/cgrouptest                                      d= 9 t=157  <- bytes, fmt, internal/runtime/cgroup, os, path/filepath, slices, strconv, syscall, testing
  internal/coverage/decodecounter                          d= 9 t=157  <- encoding/binary, fmt, internal/coverage, internal/coverage/slicereader, internal/coverage/stringtab, io, os, strconv, unsafe
  debug/elf                                                d=12 t=157  <- bytes, compress/zlib, debug/dwarf, encoding/binary, errors, fmt, internal/saferio, internal/zstd, io, os, strconv, unsafe
  internal/trace                                           d=18 t=157  <- bufio, bytes, cmp, container/heap, encoding/binary, errors, fmt, internal/trace/internal/tracev1, internal/trace/tracev2, internal/trace/version, io, iter, math, slices, sort, strconv, time, unsafe
  crypto/ec/schnorr                                        d= 6 t=158  <- crypto/ec, crypto/ec/chainhash, crypto/ec/secp256k1, crypto/sha256, fmt, testing
  image/internal/imageutil                                 d= 6 t=158  <- bytes, fmt, go/format, image, log, os
  internal/coverage/decodemeta                             d= 9 t=158  <- bufio, encoding/binary, fmt, hash/fnv, internal/coverage, internal/coverage/slicereader, internal/coverage/stringtab, io, os
  internal/coverage/encodecounter                          d=11 t=158  <- bufio, encoding/binary, fmt, internal/coverage, internal/coverage/slicewriter, internal/coverage/stringtab, internal/coverage/uleb128, io, maps, os, slices
  internal/coverage/encodemeta                             d=12 t=158  <- bufio, bytes, encoding/binary, fmt, hash, hash/fnv, internal/coverage, internal/coverage/stringtab, internal/coverage/uleb128, io, os, unsafe
  go/build                                                 d=23 t=158  <- bufio, bytes, errors, fmt, go/build/constraint, go/scanner, go/token, internal/goroot, internal/goversion, internal/platform, internal/syslist, io, io/fs, os, os/exec, path, path/filepath, runtime, slices, strconv, unicode, unicode/utf8, unsafe
  image/draw                                               d= 3 t=159  <- image, image/color, image/internal/imageutil
  image/jpeg                                               d= 6 t=159  <- bufio, errors, image, image/color, image/internal/imageutil, io
  go/internal/srcimporter                                  d= 9 t=159  <- bytes, fmt, go/build, go/token, io, os, os/exec, path/filepath, unsafe
  debug/buildinfo                                          d=11 t=159  <- bytes, debug/elf, encoding/binary, errors, fmt, internal/saferio, io, io/fs, os, runtime/debug, unsafe
  internal/exportdata                                      d=11 t=159  <- bufio, bytes, errors, fmt, go/build, internal/saferio, io, os, os/exec, path/filepath, strconv
  internal/trace/testtrace                                 d=11 t=160  <- bufio, bytes, errors, fmt, internal/trace, internal/trace/raw, internal/trace/version, internal/txtar, io, slices, strconv
  go/internal/gccgoimporter                                d=15 t=161  <- bufio, bytes, debug/elf, errors, fmt, go/constant, go/token, internal/xcoff, io, os, os/exec, path/filepath, strconv, text/scanner, unicode/utf8
  go/internal/gcimporter                                   d= 9 t=162  <- bufio, bytes, fmt, go/token, internal/exportdata, internal/pkgbits, io, os, slices
  image/gif                                                d=11 t=162  <- bufio, bytes, compress/lzw, errors, fmt, image, image/color, image/color/palette, image/draw, internal/byteorder, io
  internal/trace/traceviewer                               d=18 t=162  <- bufio, bytes, embed, encoding/json, fmt, internal/profile, internal/trace, internal/trace/traceviewer/format, io, log, math, net/http, os, os/exec, path/filepath, runtime, strconv, time
  internal/coverage/cfile                                  d=22 t=170  <- bytes, encoding/json, fmt, hash/fnv, internal/coverage, internal/coverage/calloc, internal/coverage/cformat, internal/coverage/cmerge, internal/coverage/decodecounter, internal/coverage/decodemeta, internal/coverage/encodecounter, internal/coverage/encodemeta, internal/coverage/pods, internal/coverage/rtcov, internal/runtime/exithook, io, os, path/filepath, runtime, strconv, time, unsafe
  go/importer                                              d= 7 t=172  <- go/build, go/internal/gccgoimporter, go/internal/gcimporter, go/internal/srcimporter, go/token, io, runtime
  internal/types/errors                                    d= 9 t=173  <- bytes, fmt, go/importer, go/token, log, os, path, strconv, unsafe

## Minor Cycle: image, image/png

  image: bufio, errors, image/color, image/png, io, math/bits, strconv
  image/png: bufio, compress/zlib, encoding/binary, fmt, hash, hash/crc32, image, image/color, io, strconv

## Top-Level Summary (47 packages, by direct dep count)

  cmp               d= 0  <- (none)
  errors            d= 0  <- (none)
  moxie             d= 0  <- (none)
  structs           d= 0  <- (none)
  unicode           d= 0  <- (none)
  unsafe            d= 0  <- (none)
  builtin           d= 1  <- cmp
  container         d= 1  <- sort
  jsbridge          d= 1  <- unsafe
  unique            d= 1  <- unsafe
  context           d= 2  <- errors, time
  evloop            d= 2  <- fmt, syscall
  html              d= 2  <- bytes, unicode
  maps              d= 2  <- iter, unsafe
  sync              d= 2  <- internal, unsafe
  testing           d= 2  <- fmt, os
  iter              d= 3  <- internal, runtime, unsafe
  weak              d= 3  <- internal, runtime, unsafe
  bufio             d= 4  <- bytes, errors, io, unicode
  embed             d= 4  <- errors, internal, io, time
  slices            d= 4  <- cmp, iter, math, unsafe
  hash              d= 5  <- errors, internal, io, math, unsafe
  text              d= 5  <- bytes, fmt, io, os, unicode
  bytes             d= 7  <- errors, internal, io, iter, math, unicode, unsafe
  fmt               d= 7  <- errors, io, math, os, slices, strconv, unicode
  sort              d= 7  <- bytes, fmt, go, log, math, os, slices
  strings           d= 7  <- errors, internal, io, iter, math, unicode, unsafe
  io                d= 8  <- bytes, errors, internal, os, path, slices, time, unicode
  path              d= 8  <- bytes, errors, internal, io, os, slices, syscall, unicode
  index             d= 9  <- bytes, encoding, errors, io, log, math, os, slices, sort
  log               d= 9  <- bytes, errors, fmt, io, net, os, runtime, time, unsafe
  runtime           d=10  <- bytes, context, errors, fmt, internal, io, math, strconv, time, unsafe
  strconv           d=10  <- bytes, errors, fmt, go, internal, log, math, os, slices, unicode
  os                d=11  <- bufio, bytes, errors, fmt, internal, io, runtime, strconv, syscall, time, unsafe
  syscall           d=11  <- bytes, errors, fmt, go, internal, io, log, os, runtime, slices, unsafe
  compress          d=12  <- bufio, cmp, encoding, errors, fmt, hash, io, math, slices, sort, strconv, time
  debug             d=13  <- bytes, compress, encoding, errors, fmt, internal, io, os, path, runtime, sort, strconv, unsafe
  encoding          d=13  <- bufio, bytes, crypto, errors, fmt, internal, io, math, slices, strconv, time, unicode, unsafe
  math              d=13  <- bytes, cmp, encoding, errors, fmt, go, internal, io, log, os, slices, strconv, unsafe
  image             d=14  <- bufio, bytes, compress, encoding, errors, fmt, go, hash, internal, io, log, math, os, strconv
  time              d=14  <- bytes, errors, go, internal, io, log, math, net, os, runtime, slices, syscall, unicode, unsafe
  mime              d=15  <- bufio, bytes, crypto, encoding, errors, fmt, io, maps, math, net, os, path, slices, unicode, unsafe
  archive           d=17  <- bufio, bytes, compress, encoding, errors, fmt, hash, io, maps, math, os, path, slices, strconv, syscall, time, unicode
  go                d=19  <- bufio, bytes, cmp, debug, errors, fmt, internal, io, iter, math, os, path, runtime, slices, sort, strconv, text, unicode, unsafe
  crypto            d=27  <- bytes, container, context, embed, encoding, errors, fmt, go, hash, internal, io, iter, log, maps, math, net, os, path, runtime, slices, strconv, syscall, testing, time, unicode, unsafe, weak
  net               d=27  <- bufio, bytes, cmp, compress, container, context, crypto, encoding, errors, fmt, internal, io, log, maps, math, mime, os, path, runtime, slices, sort, strconv, syscall, time, unicode, unique, unsafe
  internal          d=32  <- bufio, bytes, cmp, compress, container, crypto, debug, embed, encoding, errors, fmt, go, hash, io, iter, log, maps, math, net, os, path, runtime, slices, sort, strconv, structs, syscall, testing, text, time, unicode, unsafe
