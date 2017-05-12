package lazyexp

import (
	"sync"
)

// A Fetcher represents a blocking calculation executed by a Node to "fill" it.
//
// fetch() is private because it is only supposed to be called by this package, use FuncFetcher() to create one.
//
// While it might be convenient to also have a Fetch() function, that would encourage sequential fetching, so use the free Fetch() function instead.
type Fetcher interface {
	fetch()
}

// FuncFetcher returns a Fetcher backed by the given function. The function must not be nil.
func FuncFetcher(fetch func()) Fetcher {
	return &funcFetcher{
		fetcher: fetch,
	}
}

// A Node represents a blocking calculation.
type Node interface {
	Fetcher
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

type funcFetcher struct {
	fetcher func()
	once    sync.Once
}

var _ Fetcher = (*funcFetcher)(nil)

func (ff *funcFetcher) fetch() {
	ff.once.Do(ff.fetcher)
}
