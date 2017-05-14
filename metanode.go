package lazyexp

import (
	"context"
	"sync"
	"sync/atomic"
)

// NewMetaNode creates a new node that itself yields nodes that are fetched when it is fetched.
func NewMetaNode(dependencies Dependencies, fetch func(context.Context, []error) (Node, error)) Node {
	return &metaNode{
		fetcher:      fetch,
		dependencies: dependencies,
	}
}

type metaNode struct {
	fetcher      func(context.Context, []error) (Node, error)
	dependencies Dependencies
	once         sync.Once
	fetcherNode  *node
	result       Node
	err          error
	iFetched     int32
}

func (m *metaNode) Fetch(ctx context.Context) error { return m.fetch(ctx, false) }

func (m *metaNode) FetchStrict(ctx context.Context) error { return m.fetch(ctx, true) }

func (m *metaNode) fetch(ctx context.Context, strict bool) error {
	m.once.Do(func() {
		m.fetcherNode = newNode(m.dependencies, func(subCtx context.Context, errs []error) error {
			var err error
			m.result, err = m.fetcher(subCtx, errs)
			return err
		})
		m.err = m.fetcherNode.fetch(ctx, strict)
		if m.err == nil {
			m.err = m.result.Fetch(ctx)
		}
		atomic.StoreInt32(&m.iFetched, 1)
	})
	return m.err
}

func (m *metaNode) fetched() bool {
	return atomic.LoadInt32(&m.iFetched) != 0
}

func (m *metaNode) noUserImplementations() {}
