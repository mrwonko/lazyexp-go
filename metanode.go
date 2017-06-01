package lazyexp

import (
	"sync"
	"sync/atomic"
)

// NewMetaNode creates a new node that itself yields nodes that are fetched when it is fetched.
func NewMetaNode(dependencies Dependencies, fetch func([]error) (Node, error)) Node {
	return &metaNode{
		fetcher:      fetch,
		dependencies: dependencies,
	}
}

type metaNode struct {
	fetcher      func([]error) (Node, error)
	dependencies Dependencies
	once         sync.Once
	fetcherNode  *node
	result       Node
	err          error
	iFetched     int32
}

func (m *metaNode) Fetch() error { return m.fetch(false) }

func (m *metaNode) FetchStrict() error { return m.fetch(true) }

func (m *metaNode) fetch(strict bool) error {
	m.once.Do(func() {
		m.fetcherNode = newNode(m.dependencies, func(errs []error) error {
			var err error
			m.result, err = m.fetcher(errs)
			return err
		})
		m.err = m.fetcherNode.fetch(strict)
		if m.err == nil {
			m.err = m.result.Fetch()
		}
		atomic.StoreInt32(&m.iFetched, 1)
	})
	return m.err
}

func (m *metaNode) fetched() bool {
	return atomic.LoadInt32(&m.iFetched) != 0
}

func (m *metaNode) noUserImplementations() {}
