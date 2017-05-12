package lazyexp

import (
	"sync"
)

// A Node represents a blocking calculation. Create one using NewNode().
//
// You'll probably want to embed a Node in your struct that will contain the result.
type Node interface {
	Fetch()
	noUserImplementations()
}

// Dependencies are Nodes that must be Fetched before the given one can be.
type Dependencies []Node

// NewNode returns a Node backed by the given function.
//
// The fetch function must not be nil, unless you never intend to Fetch() this Node.
//
// The dependencies are Nodes that must be fetched before this one can be fetched. They will be fetched in parallel before fetch() is called and must not contain nil.
func NewNode(dependencies []Node, fetch func()) Node {
	// TODO: fetch func(context.Context) error - think about recoverable errors in particular
	return &node{
		fetcher:      fetch,
		dependencies: dependencies,
	}
}

type node struct {
	fetcher      func()
	dependencies []Node
	once         sync.Once
}

var _ Node = (*node)(nil)

func (n *node) Fetch() {
	n.once.Do(func() {
		// fetch dependencies in parallel
		if l := len(n.dependencies); l > 0 {
			if l == 1 {
				// no need to fetch single dependency in parallel
				n.dependencies[0].Fetch()
			} else {
				// we can save one goroutine by fetching that dependency on the current one
				var wg sync.WaitGroup
				wg.Add(l - 1)
				for i := 1; i < l; i++ {
					go func(i int) {
						n.dependencies[i].Fetch()
						wg.Done()
					}(i)
				}
				n.dependencies[0].Fetch()
				wg.Wait()
			}
		}
		n.fetcher()
	})
}

func (n *node) noUserImplementations() {}
