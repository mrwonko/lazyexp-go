package lazyexp

import (
	"sync"
)

// A Node represents a blocking calculation.
type Node interface {
	Fetch()
}

// Fetch will concurrently fetch the given Nodes.
func Fetch(head Node, tail ...Node) {
	if len(tail) == 0 {
		head.Fetch()
	}
	var wg sync.WaitGroup
	wg.Add(len(tail))
	for i := range tail {
		go func(i int) {
			tail[i].Fetch()
			wg.Done()
		}(i)
	}
	head.Fetch()
	wg.Wait()
}
