package lazyexp

import (
	"sync"
)

// MetaNodeFetcher is the backend for a MetaNode that does the actual retrieval.
type MetaNodeFetcher interface {
	Dependencies() Dependencies
	Fetch([]error) (Node, error)
	String(successfullyFetched bool) string
}

type funcMetaNodeFetcher struct {
	dependencies Dependencies
	fetch        func([]error) (Node, error)
	toString     func(successfullyFetched bool) string
}

var _ MetaNodeFetcher = funcMetaNodeFetcher{}

func (fmnf funcMetaNodeFetcher) Dependencies() Dependencies {
	return fmnf.dependencies
}

func (fmnf funcMetaNodeFetcher) Fetch(errs []error) (Node, error) {
	return fmnf.fetch(errs)
}

func (fmnf funcMetaNodeFetcher) String(successfullyFetched bool) string {
	return fmnf.toString(successfullyFetched)
}

// NewFuncMetaNodeFetcher creates a MetaNodeFetcher backed by the given functions, in case you don't want to write a whole type.
func NewFuncMetaNodeFetcher(dependencies Dependencies, fetch func([]error) (Node, error), toString func(successfullyFetched bool) string) MetaNodeFetcher {
	return funcMetaNodeFetcher{
		dependencies,
		fetch,
		toString,
	}
}

type metaNodeAdapater struct {
	fetcher MetaNodeFetcher
	node    *metaNode
}

var _ NodeFetcher = metaNodeAdapater{}

func (mna metaNodeAdapater) Dependencies() Dependencies {
	return mna.fetcher.Dependencies()
}

func (mna metaNodeAdapater) Fetch(errs []error) error {
	var err error
	mna.node.result, err = mna.fetcher.Fetch(errs)
	return err
}

func (mna metaNodeAdapater) String(successfullyFetched bool) string {
	return mna.fetcher.String(successfullyFetched)
}

// NewMetaNode creates a new node that itself yields nodes that are fetched when it is fetched.
func NewMetaNode(fetcher MetaNodeFetcher) Node {
	m := metaNode{}
	m.fetcherNode = newNode(metaNodeAdapater{fetcher, &m})
	return &m
}

type metaNode struct {
	once        sync.Once
	fetcherNode *node
	result      Node
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

func (m *metaNode) flatten(nf *nodeFlattener) ID {
	fetcherID := m.fetcherNode.flatten(nf)
	// if the fetcherNode is not done evaluating, return it instead
	if !nf.result[fetcherID].Evaluated {
		return fetcherID
	}
	// if the fetcherNode encountered an error, return it
	if m.fetcherNode.err != nil {
		return fetcherID
	}
	fn := nf.result[fetcherID]
	fn.Child = m.result.flatten(nf)
	nf.result[fetcherID] = fn
	return fetcherID
}

func (m *metaNode) String() string { return m.fetcherNode.String() }

func (m *metaNode) noUserImplementations() {}
