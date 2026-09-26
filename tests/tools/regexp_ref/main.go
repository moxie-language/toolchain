// Command regexp_ref prints the same case table tests/data/regexp/main.mx
// prints, using Go's regexp. Legacy's -print-allocs matches with Go's regexp,
// so this is the reference for the stage4 matcher in pkg/mxregexp: the two
// outputs must be byte identical.
package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i += 2
				continue
			case 't':
				b.WriteByte('\t')
				i += 2
				continue
			case 'r':
				b.WriteByte('\r')
				i += 2
				continue
			case '\\':
				b.WriteByte('\\')
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: regexp_ref <cases-file>")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			fmt.Printf("case %d: missing tab\n", i)
			os.Exit(1)
		}
		pattern, text := line[:tab], unescape(line[tab+1:])
		re, cerr := regexp.Compile(pattern)
		if cerr != nil {
			fmt.Printf("%d error\n", i)
			continue
		}
		fmt.Printf("%d %t\n", i, re.MatchString(text))
	}
}
