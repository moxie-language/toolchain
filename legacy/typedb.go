package main

// The type-resolution engine, legacy side (wood law).
//
// The schema is docs/type-db.md: nominal fact keys, structural hashes as a
// secondary index only, and six indexes - type->methods (forward, sorted; no
// method->types index), package->interface contributions, (package,name),
// hash, package->types, and alias/underlying->named types. Every index has a
// cheap delete because release is delete.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	edgeDeclares = iota
	edgeImplements
	edgeFieldOf
	edgeAlias
	edgeEmbed
	edgeKindCount
)

type typeFact struct {
	key, pkg, name, kind, under, sig, hash, version string
}

type typeDB struct {
	facts    map[string]*typeFact
	fwd, rev [edgeKindCount]map[string][]string
	cnt      [edgeKindCount]map[string]int32
	versions map[string]string
	pkgFacts map[string][]string
	byHash   map[string][]string
	pkgIface map[string][]string
}

func newTypeDB() *typeDB {
	db := &typeDB{
		facts:    map[string]*typeFact{},
		versions: map[string]string{},
		pkgFacts: map[string][]string{},
		byHash:   map[string][]string{},
		pkgIface: map[string][]string{},
	}
	for i := 0; i < edgeKindCount; i++ {
		db.fwd[i] = map[string][]string{}
		db.rev[i] = map[string][]string{}
		db.cnt[i] = map[string]int32{}
	}
	return db
}

func addUnique(xs []string, v string) []string {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}

func removeStr(xs []string, v string) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func factKey(pkg, name string) string { return pkg + "|" + name }

func descKey(text string) string { return "desc:" + text }

func hashFact(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

func recvName(r string) string {
	r = strings.TrimSpace(r)
	if len(r) >= 2 && r[0] == '(' && r[len(r)-1] == ')' {
		r = r[1 : len(r)-1]
	}
	return strings.TrimPrefix(r, "*")
}

func typeRef(selfPkg, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return descKey(text)
	}
	base := strings.TrimLeft(text, "*[]()")
	if i := strings.IndexByte(base, '.'); i > 0 {
		return factKey(base[:i], base[i+1:])
	}
	if base != text {
		return descKey(text)
	}
	return factKey(selfPkg, base)
}

func (db *typeDB) indexFact(f *typeFact) {
	db.pkgFacts[f.pkg] = addUnique(db.pkgFacts[f.pkg], f.key)
	if f.hash != "" {
		db.byHash[f.hash] = addUnique(db.byHash[f.hash], f.key)
	}
}

func (db *typeDB) edgeAdd(kind int, from, to string) {
	if from == "" || to == "" {
		return
	}
	db.fwd[kind][from] = addUnique(db.fwd[kind][from], to)
	if kind != edgeDeclares {
		db.rev[kind][to] = addUnique(db.rev[kind][to], from)
	}
	db.cnt[kind][from+"|"+to]++
}

func (db *typeDB) edgeRemove(kind int, from, to string) {
	db.fwd[kind][from] = removeStr(db.fwd[kind][from], to)
	if kind != edgeDeclares {
		db.rev[kind][to] = removeStr(db.rev[kind][to], from)
	}
	k := from + "|" + to
	if c := db.cnt[kind][k]; c > 1 {
		db.cnt[kind][k] = c - 1
	} else {
		delete(db.cnt[kind], k)
	}
}

func (db *typeDB) loadMxh(data []byte) {
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "package ") {
		return
	}
	fields := strings.Fields(lines[0])
	if len(fields) < 2 {
		return
	}
	pkg := fields[1]
	ver := hashFact(string(data))
	db.versions[pkg] = ver
	for _, line := range lines[1:] {
		switch {
		case strings.HasPrefix(line, "type "):
			rest := strings.TrimPrefix(line, "type ")
			i := strings.IndexByte(rest, ' ')
			if i < 0 {
				continue
			}
			name := rest[:i]
			f := &typeFact{key: factKey(pkg, name), pkg: pkg, name: name, kind: "type", under: strings.TrimSpace(rest[i+1:]), version: ver}
			f.hash = hashFact(f.kind + "|" + f.under)
			db.facts[f.key] = f
			db.indexFact(f)
		case strings.HasPrefix(line, "alias "):
			rest := strings.TrimPrefix(line, "alias ")
			i := strings.IndexByte(rest, ' ')
			if i < 0 {
				continue
			}
			name := rest[:i]
			f := &typeFact{key: factKey(pkg, name), pkg: pkg, name: name, kind: "alias", under: strings.TrimSpace(rest[i+1:]), version: ver}
			f.hash = hashFact(f.kind + "|" + f.under)
			db.facts[f.key] = f
			db.indexFact(f)
			db.edgeAdd(edgeAlias, f.key, typeRef(pkg, f.under))
		case strings.HasPrefix(line, "iface "):
			rest := strings.TrimPrefix(line, "iface ")
			i := strings.IndexByte(rest, ' ')
			if i < 0 {
				continue
			}
			name := rest[:i]
			f := &typeFact{key: factKey(pkg, name), pkg: pkg, name: name, kind: "iface", sig: strings.TrimSpace(rest[i+1:]), version: ver}
			f.hash = hashFact(f.kind + "|" + f.sig)
			db.facts[f.key] = f
			db.indexFact(f)
		case strings.HasPrefix(line, "method "):
			fs := strings.Fields(strings.TrimPrefix(line, "method "))
			if len(fs) < 2 {
				continue
			}
			recv := recvName(fs[0])
			mname := fs[1]
			tkey := factKey(pkg, recv)
			db.edgeAdd(edgeDeclares, tkey, mname)
			if db.facts[tkey] == nil {
				f := &typeFact{key: tkey, pkg: pkg, name: recv, kind: "methodrecv", version: ver}
				f.hash = hashFact(f.kind + "|" + recv)
				db.facts[tkey] = f
				db.indexFact(f)
			}
		}
	}
}

// addConversion records one actual site (traffic), not potential.
func (db *typeDB) addConversion(iface, typeKey, sitePkg string) {
	db.edgeAdd(edgeImplements, iface, typeKey)
	if sitePkg != "" {
		db.pkgIface[sitePkg] = addUnique(db.pkgIface[sitePkg], iface+"|"+typeKey)
	}
}

func (db *typeDB) deletePkg(pkg string) {
	keys := append([]string(nil), db.pkgFacts[pkg]...)
	for _, key := range keys {
		if f := db.facts[key]; f != nil && f.hash != "" {
			db.byHash[f.hash] = removeStr(db.byHash[f.hash], key)
		}
		delete(db.facts, key)
		for kind := 0; kind < edgeKindCount; kind++ {
			for _, to := range append([]string(nil), db.fwd[kind][key]...) {
				db.edgeRemove(kind, key, to)
			}
			for _, from := range append([]string(nil), db.rev[kind][key]...) {
				db.edgeRemove(kind, from, key)
			}
		}
	}
	delete(db.pkgFacts, pkg)
	delete(db.versions, pkg)
	delete(db.pkgIface, pkg)
}

func (db *typeDB) methodCount() int {
	seen := map[string]bool{}
	for _, ms := range db.fwd[edgeDeclares] {
		for _, m := range ms {
			seen[m] = true
		}
	}
	return len(seen)
}

func (db *typeDB) factCount() int { return len(db.facts) }

func (db *typeDB) sharedHashes() int {
	type owner struct{ key string }
	seen := map[string]string{}
	shared := 0
	for k, f := range db.facts {
		if prev, ok := seen[f.hash]; ok && prev != k {
			shared++
		} else {
			seen[f.hash] = k
		}
	}
	return shared
}

// typedbCmd is the diagnostic: mxc typedb file.mxh...
func typedbCmd(files []string) {
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: mxc typedb <file.mxh>...")
		return
	}
	db := newTypeDB()
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "typedb: cannot read", path)
			continue
		}
		db.loadMxh(data)
	}
	fmt.Printf("typedb: facts=%d methods=%d versions=%d shared=%d\n", db.factCount(), db.methodCount(), len(db.versions), db.sharedHashes())
	delPkg := ""
	for _, f := range db.facts {
		delPkg = f.pkg
		break
	}
	if delPkg != "" {
		db.deletePkg(delPkg)
		fmt.Printf("typedb: after delete %s: facts=%d methods=%d\n", delPkg, db.factCount(), db.methodCount())
	}
}

// keep sort imported: the type->method index is sorted for subset tests.
var _ = sort.Strings
