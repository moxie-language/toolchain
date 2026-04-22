package main

// PackageManifest is the top-level manifest for a single package's corpus output.
type PackageManifest struct {
	Package string         `json:"package"`
	Files   []FileManifest `json:"files"`
}

// FileManifest describes one source file and its segments.
type FileManifest struct {
	Path      string            `json:"path"`
	FileIndex int               `json:"file_index"`
	Segments  []SegmentManifest `json:"segments"`
}

// SegmentManifest describes one AST segment and its paired outputs.
type SegmentManifest struct {
	ID        string       `json:"id"`
	Kind      string       `json:"kind"`
	Name      string       `json:"name"`
	ASTFile   string       `json:"ast_file"`
	SizeBytes int          `json:"size_bytes"`
	SourcePos SourcePos    `json:"source_pos"`
	Comments  []string     `json:"comments,omitempty"`
	Generated bool         `json:"generated"`
	HasSpawn  bool         `json:"has_spawn"`
	IRFiles   OptFileMap   `json:"ir_files,omitempty"`
	ASMFiles  OptFileMap   `json:"asm_files,omitempty"`
	BinFiles  OptFileMap   `json:"bin_files,omitempty"`
	Lineinfo  *SizedFile   `json:"lineinfo_file,omitempty"`
	IRName    string       `json:"ir_name,omitempty"`
	IRDebug   *IRDebugInfo `json:"ir_debug,omitempty"`
}

type SourcePos struct {
	Line    int `json:"line"`
	EndLine int `json:"end_line"`
}

type SizedFile struct {
	File      string `json:"file"`
	SizeBytes int    `json:"size_bytes"`
}

// OptFileMap maps optimization level to file+size. Nil entries mean the
// function was eliminated at that opt level.
type OptFileMap map[string]*SizedFile

type IRDebugInfo struct {
	File string `json:"file"`
	Line int    `json:"line"`
}
