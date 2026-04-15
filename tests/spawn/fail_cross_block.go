package main

// Regression test for spawn move-semantics cross-block tracking.
// Prior to the fix in compiler/restrict.go, the check only walked the
// same basic block for post-spawn uses of moved values, so uses in
// reachable successor blocks silently passed validation. The BFS
// reachability walk now flags them.

func echo(x int32) {
	println(x)
}

func compute() int32 {
	return 42
}

func main() {
	x := compute()
	flag := x > 0
	if flag {
		spawn(echo, x)
	}
	println(x) // ERROR: variable used after spawn in reachable successor block
}
