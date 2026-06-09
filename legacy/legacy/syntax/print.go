package syntax

import "fmt"

func String(n Node) string {
	return fmt.Sprintf("%T", n)
}

func StartPos(n Node) Pos {
	return n.Pos()
}
