// Restriction tests ensure the compiler rejects specific patterns with
// a clean error message rather than crashing.
//
// Format: each .mx file starts with "// expected: <substring>"
// The runner compiles the file, asserts exit != 0, and checks that
// stderr contains the expected substring. It also asserts that the
// compiler does NOT crash (no "panic:", "SIGSEGV", "nil pointer").
