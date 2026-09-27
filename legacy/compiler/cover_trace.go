package compiler

// CoverSiteInfo describes a basic block recorded by -cover.
type CoverSiteInfo struct {
	ID   int32  `json:"id"`
	Name string `json:"name"` // "pkg/path.FuncName@file:line"
}

var CoverSites []CoverSiteInfo

func ResetCoverSites() {
	CoverSites = CoverSites[:0]
}

func nextCoverSite(name string) int32 {
	id := int32(len(CoverSites))
	CoverSites = append(CoverSites, CoverSiteInfo{ID: id, Name: name})
	return id
}
