package cover

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// coverSiteEntry mirrors one entry of the <output>.cover_sites.json table the
// compiler writes for a -cover build.
type coverSiteEntry struct {
	ID   int32  `json:"id"`
	Name string `json:"name"`
}

type coverRec struct {
	fn   string
	file string
	line int32
	hits uint64
}

// Report joins the compiler's site table with the counters the
// instrumented program dumped, producing the same text stage4's pkg/mxcover
// prints: totals, per-file table, then every site that never ran. The test
// harness prints it for the package it just ran, so -cover needs no external
// tool on either compiler.
func Report(sitesPath, countsPath, prefix string) (string, error) {
	sitesData, err := os.ReadFile(sitesPath)
	if err != nil {
		return "", fmt.Errorf("cannot read %s", sitesPath)
	}
	countsData, err := os.ReadFile(countsPath)
	if err != nil {
		return "", fmt.Errorf("cannot read %s", countsPath)
	}
	var sites []coverSiteEntry
	if err := json.Unmarshal(sitesData, &sites); err != nil {
		return "", fmt.Errorf("cannot parse %s", sitesPath)
	}
	hits := parseCounts(string(countsData))
	var recs []coverRec
	for _, s := range sites {
		at := strings.LastIndexByte(s.Name, '@')
		if at < 0 {
			continue
		}
		fn := s.Name[:at]
		loc := s.Name[at+1:]
		colon := strings.LastIndexByte(loc, ':')
		if colon < 0 {
			continue
		}
		file := loc[:colon]
		if file == "" {
			// A synthetic block (a package init) has no source file.
			continue
		}
		if prefix != "" && !strings.HasPrefix(file, prefix) {
			continue
		}
		line, _ := strconv.Atoi(loc[colon+1:])
		recs = append(recs, coverRec{fn: fn, file: file, line: int32(line), hits: hits[strconv.Itoa(int(s.ID))]})
	}
	if len(recs) == 0 {
		note := ""
		if prefix != "" {
			note = " under " + prefix
		}
		return "", fmt.Errorf("no sites%s", note)
	}
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].file != recs[j].file {
			return recs[i].file < recs[j].file
		}
		if recs[i].line != recs[j].line {
			return recs[i].line < recs[j].line
		}
		return recs[i].fn < recs[j].fn
	})

	var b strings.Builder
	covered := int32(0)
	for _, r := range recs {
		if r.hits > 0 {
			covered++
		}
	}
	fmt.Fprintf(&b, "sites: %d covered: %d (%s)\n", len(recs), covered, Pct(covered, int32(len(recs))))

	// Per-file table, in path order (the sort above ordered by file, then line).
	var curFile string
	var fileTotal, fileCov int32
	flush := func() {
		if curFile != "" {
			fmt.Fprintf(&b, "%s  %d/%d  %s\n", Pct(fileCov, fileTotal), fileCov, fileTotal, curFile)
		}
	}
	for _, r := range recs {
		if r.file != curFile {
			flush()
			curFile = r.file
			fileTotal = 0
			fileCov = 0
		}
		fileTotal++
		if r.hits > 0 {
			fileCov++
		}
	}
	flush()

	uncovered := int32(0)
	for _, r := range recs {
		if r.hits == 0 {
			uncovered++
		}
	}
	fmt.Fprintf(&b, "\nuncovered sites: %d\n", uncovered)
	for _, r := range recs {
		if r.hits == 0 {
			fmt.Fprintf(&b, "  %s:%d  %s\n", r.file, r.line, r.fn)
		}
	}
	return b.String(), nil
}

// Pct renders a percentage with one decimal, matching pkg/mxcover (a
// zero total keeps its two leading spaces so the columns stay aligned).
func Pct(n, total int32) string {
	if total == 0 {
		return "  0.0%"
	}
	tenths := (int64(n) * 1000) / int64(total)
	return strconv.FormatInt(tenths/10, 10) + "." + strconv.FormatInt(tenths%10, 10) + "%"
}

// parseCounts reads the runtime's dump: one "coversite <id> count=<n>" line
// per site that ran, keyed by the id as text.
func parseCounts(data string) map[string]uint64 {
	m := map[string]uint64{}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "coversite ") {
			continue
		}
		rest := line[len("coversite "):]
		sp := strings.IndexByte(rest, ' ')
		if sp <= 0 {
			continue
		}
		id := rest[:sp]
		rest = rest[sp+1:]
		if !strings.HasPrefix(rest, "count=") {
			continue
		}
		n, _ := strconv.ParseUint(strings.TrimSpace(rest[len("count="):]), 10, 64)
		m[id] = n
	}
	return m
}

// ModulePrefix returns the module path plus a trailing slash, which is the
// prefix that selects the module's own files out of the link set (the runtime
// and stdlib are compiled in too). It mirrors the compiler's own module-root
// search, with MOXIEROOT as the fallback for a target staged outside the tree.
func ModulePrefix(startDir string) string {
	dir := startDir
	if dir == "" {
		dir = "."
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	for {
		for _, name := range []string{"moxie.mod", "go.mod"} {
			p := filepath.Join(dir, name)
			if data, err := os.ReadFile(p); err == nil {
				if mp := parseModuleName(string(data)); mp != "" {
					return mp + "/"
				}
				return ""
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if root := os.Getenv("MOXIEROOT"); root != "" {
		for _, name := range []string{"moxie.mod", "go.mod"} {
			if data, err := os.ReadFile(filepath.Join(root, name)); err == nil {
				if mp := parseModuleName(string(data)); mp != "" {
					return mp + "/"
				}
			}
		}
	}
	return ""
}

func parseModuleName(data string) string {
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(line[len("module "):])
		}
	}
	return ""
}
