package compiler

// AllocSiteInfo describes a heap allocation site matched by -print-allocs.
type AllocSiteInfo struct {
	ID   int32  `json:"id"`
	Name string `json:"name"` // "pkg/path.FuncName@file:line"
}

var AllocSites []AllocSiteInfo

func ResetAllocSites() {
	AllocSites = AllocSites[:0]
}

func nextAllocSite(name string) int32 {
	id := int32(len(AllocSites))
	AllocSites = append(AllocSites, AllocSiteInfo{ID: id, Name: name})
	return id
}
