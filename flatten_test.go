package lazyexp

import (
	"testing"
)

func TestGetID(t *testing.T) {
	// equal nodes should still get distinct IDs
	n1 := node{}
	n2 := n1
	nf := nodeFlattener{
		visited: map[Node]ID{},
	}
	id, visited := nf.getID(&n1)
	if visited {
		t.Error("n1 should not be visited")
	} else if id != 0 {
		t.Errorf("%d != 0", id)
	}
	id, visited = nf.getID(&n2)
	if visited {
		t.Error("n2 should not be visited")
	} else if id != 1 {
		t.Errorf("%d != 1", id)
	}
}

// TODO test Flatten
