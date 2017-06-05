package lazyexp

import (
	"sync"
)

// NewMetaNode creates a new node that itself yields nodes that are fetched when it is fetched.
func NewMetaNode(dependencies Dependencies, fetch func([]error) (Node, error)) Node {
	m := metaNode{
		dependencies: dependencies,
	}
	m.fetcherNode = newNode(m.dependencies, func(errs []error) error {
		var err error
		m.result, err = fetch(errs)
		return err
	})
	return &m
}

type metaNode struct {
	dependencies Dependencies
	once         sync.Once
	fetcherNode  *node
	result       Node
}

func (m *metaNode) Fetch() error { return m.fetch(false) }

func (m *metaNode) FetchStrict() error { return m.fetch(true) }

func (m *metaNode) fetch(strict bool) error {
	m.once.Do(func() {
		m.fetcherNode.fetch(strict)
		if m.fetcherNode.err == nil {
			m.result.Fetch()
		}
	})
	if m.fetcherNode.err != nil {
		return m.fetcherNode.err
	}
	return m.result.Fetch()
}

func (m *metaNode) fetched() bool {
	return m.fetcherNode.fetched() &&
		(m.fetcherNode.err != nil ||
			m.result.fetched())
}

func (m *metaNode) noUserImplementations() {}
