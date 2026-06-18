package compiler

// Scope dealloc infrastructure removed: arena-per-function model.
// No individual free(). Arena bulk-frees at munmap.
