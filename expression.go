package lazyexp

import (
	"sync"
)

// A Node represents a blocking calculation.
//
// fetch() is private because it is only supposed to be called by this package, so use NewNode() to create a Node.
//
// While it might be convenient to also have a Fetch() function, that would encourage sequential fetching, so use the free Fetch() function instead.
type Node interface {
	fetch()
}

// NewNode returns a Node backed by the given function. The function must not be nil.
func NewNode(fetch func()) Node {
	return &node{
		fetcher: fetch,
	}
}

// Fetch will concurrently fetch the given Nodes. They must not be nil.
func Fetch(head Node, tail ...Node) {
	if len(tail) == 0 {
		head.fetch()
		return
	}
	var wg sync.WaitGroup
	wg.Add(len(tail))
	for i := range tail {
		go func(i int) {
			tail[i].fetch()
			wg.Done()
		}(i)
	}
	head.fetch()
	wg.Wait()
}

type node struct {
	fetcher func()
	once    sync.Once
}

var _ Node = (*node)(nil)

func (n *node) fetch() {
	n.once.Do(n.fetcher)
}
