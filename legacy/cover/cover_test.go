package cover

import (
	"os"
	"path/filepath"
	"testing"
)

// The fixture is shared with stage4: cmd/mxcover over these same two files must
// print the same text (see the cover-report check in tests/phase6). Keeping the
// expected report literal here is what pins the format across the two
// compilers' harnesses.
const coverSitesJSON = `[{"id":0,"name":"git.smesh.lol/smesh/pkg/demo.F@git.smesh.lol/smesh/pkg/demo/demo.mx:10"},` +
	`{"id":1,"name":"git.smesh.lol/smesh/pkg/demo.G@git.smesh.lol/smesh/pkg/demo/demo.mx:20"},` +
	`{"id":2,"name":"git.smesh.lol/smesh/other.H@git.smesh.lol/smesh/other/other.mx:5"}]`

const coverCountsText = "coversite 0 count=3\ncoversite 2 count=1\n"

const coverWantAll = `sites: 3 covered: 2 (66.6%)
100.0%  1/1  git.smesh.lol/smesh/other/other.mx
50.0%  1/2  git.smesh.lol/smesh/pkg/demo/demo.mx

uncovered sites: 1
  git.smesh.lol/smesh/pkg/demo/demo.mx:20  git.smesh.lol/smesh/pkg/demo.G
`

const coverWantPkg = `sites: 2 covered: 1 (50.0%)
50.0%  1/2  git.smesh.lol/smesh/pkg/demo/demo.mx

uncovered sites: 1
  git.smesh.lol/smesh/pkg/demo/demo.mx:20  git.smesh.lol/smesh/pkg/demo.G
`

func writeCoverFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	sites := filepath.Join(dir, "sites.json")
	counts := filepath.Join(dir, "counts")
	if err := os.WriteFile(sites, []byte(coverSitesJSON), 0666); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(counts, []byte(coverCountsText), 0666); err != nil {
		t.Fatal(err)
	}
	return sites, counts
}

func TestCoverReport(t *testing.T) {
	sites, counts := writeCoverFixture(t)
	for _, tc := range []struct {
		name   string
		prefix string
		want   string
	}{
		{"all", "git.smesh.lol/smesh/", coverWantAll},
		{"package", "git.smesh.lol/smesh/pkg/", coverWantPkg},
	} {
		got, err := Report(sites, counts, tc.prefix)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s report mismatch:\n got:\n%s\nwant:\n%s", tc.name, got, tc.want)
		}
	}
}

func TestCoverReportErrors(t *testing.T) {
	sites, counts := writeCoverFixture(t)
	if _, err := Report(sites+".missing", counts, ""); err == nil {
		t.Fatal("missing site table must be an error")
	}
	if _, err := Report(sites, counts+".missing", ""); err == nil {
		t.Fatal("missing counter dump must be an error")
	}
	if _, err := Report(sites, counts, "git.smesh.lol/nothing/"); err == nil {
		t.Fatal("a prefix with no sites must be an error")
	}
}
