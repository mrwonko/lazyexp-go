package lazyexp

import (
	"context"
	"sync"
	"sync/atomic"
)

// A Node represents a blocking calculation. Create one using NewNode().
//
// You'll probably want to embed a Node in your struct that will contain the result.
type Node interface {
	Fetch(context.Context) error
	// FetchStrict never cancels siblings on error and can be useful for debugging, but should generally be avoided.
	FetchStrict(context.Context) error
	fetched() bool
	noUserImplementations()
}

// A Dependency is a Node whose result another Node depends on.
//
// Created using ContinueOnError(), CancelOnError(), CancelOnCompletion() and AbortOnError().
type Dependency struct {
	node         Node
	onCompletion completionStrategy
}

// ContinueOnError returns a Dependency where any Node Fetch() errors are simply passed on without affecting any other Nodes.
func ContinueOnError(node Node) Dependency {
	return Dependency{
		node:         node,
		onCompletion: onErrorContinue,
	}
}

// CancelOnError returns a Dependency where any Node Fetch() error causes sibling Dependencies' Fetches to be canceled, but still continues with the fetch function.
func CancelOnError(node Node) Dependency {
	return Dependency{
		node:         node,
		onCompletion: onErrorCancel,
	}
}

// CancelOnCompletion returns a Dependency that upon Node Fetch() completion causes sibling Dependencies' Fetches to be canceled, but still continues with the fetch function.
func CancelOnCompletion(node Node) Dependency {
	return Dependency{
		node:         node,
		onCompletion: onCompletionCancel,
	}
}

// AbortOnError returns a Dependency where any Node Fetch() error causes sibling Dependencies' Fetches to be canceled and propagates the error.
//
// TODO: enrich the error with context before passing it on once Nodes have a description
func AbortOnError(node Node) Dependency {
	return Dependency{
		node:         node,
		onCompletion: onErrorAbort,
	}
}

// Dependencies are Nodes that must be Fetched before the given one can be.
type Dependencies []Dependency

// NewNode returns a Node backed by the given function.
//
// The fetch function must not be nil, unless you never intend to Fetch() this Node. It will be called with the errors from the optional dependencies, unless they are fatal, or context.Canceled if they returned CancelFetchSuccess().
//
// The dependencies will be fetched in parallel before fetch() is called and must not contain zero values. Don't introduce circular dependencies or the Fetch will deadlock waiting for itself to finish.
func NewNode(dependencies Dependencies, fetch func(context.Context, []error) error) Node {
	return newNode(dependencies, fetch)
}

func newNode(dependencies Dependencies, fetch func(context.Context, []error) error) *node {
	return &node{
		fetcher:      fetch,
		dependencies: dependencies,
	}
}

type node struct {
	fetcher      func(context.Context, []error) error
	dependencies Dependencies
	once         sync.Once
	err          error
	iFetched     int32
}

func (n *node) Fetch(ctx context.Context) error { return n.fetch(ctx, false) }

func (n *node) FetchStrict(ctx context.Context) error { return n.fetch(ctx, true) }

func (n *node) fetch(ctx context.Context, strict bool) error {
	n.once.Do(func() {
		var errs []error
		// fetch dependencies in parallel
		if l := len(n.dependencies); l > 0 {
			errs = make([]error, l)
			if l == 1 {
				// no need to fetch single dependency in parallel
				err := n.dependencies[0].node.Fetch(ctx)
				switch n.dependencies[0].onCompletion {
				case onErrorAbort:
					if err != nil {
						n.err = err
						atomic.StoreInt32(&n.iFetched, 1)
						return
					}
				case onErrorCancel:
					fallthrough
				case onCompletionCancel:
					// no siblings to cancel
					errs[0] = err
				case onErrorContinue:
					errs[0] = err
				}
			} else {
				// we can save one goroutine by fetching that dependency on the current one
				var wg sync.WaitGroup
				var mu sync.Mutex
				subCtx, cancel := ctx, func() {}
				if !strict {
					subCtx, cancel = context.WithCancel(ctx)
				}
				wg.Add(l - 1)
				for i := 1; i < l; i++ {
					// save spawning a goroutine for fully fetched nodes
					if n.dependencies[i].node.fetched() {
						errs[i] = fetchDependency(subCtx, cancel, n.dependencies[i], &n.err, &mu)
						wg.Done()
					} else {
						go func(i int) {
							errs[i] = fetchDependency(subCtx, cancel, n.dependencies[i], &n.err, &mu)
							wg.Done()
						}(i)
					}
				}
				errs[0] = fetchDependency(subCtx, cancel, n.dependencies[0], &n.err, &mu)
				wg.Wait()
				cancel()
				if n.err != nil {
					atomic.StoreInt32(&n.iFetched, 1)
					return
				}
			}

		}
		n.err = n.fetcher(ctx, errs)
		atomic.StoreInt32(&n.iFetched, 1)
	})
	return n.err
}

func (n *node) fetched() bool {
	return atomic.LoadInt32(&n.iFetched) != 0
}

func (n *node) noUserImplementations() {}

func fetchDependency(ctx context.Context, cancel func(), dependency Dependency, outFatalErr *error, mu *sync.Mutex) error {
	err := dependency.node.Fetch(ctx)
	switch dependency.onCompletion {
	case onErrorCancel:
		if err == nil {
			break
		}
		fallthrough
	case onCompletionCancel:
		cancel()
	case onErrorAbort:
		if err != nil {
			// only store first error, otherwise we'd likely get context.Canceled
			mu.Lock()
			if *outFatalErr == nil {
				*outFatalErr = err
			}
			mu.Unlock()
			cancel()
		}
	case onErrorContinue:
	}
	return err
}

type completionStrategy int

const (
	onErrorContinue completionStrategy = iota
	onErrorCancel
	onCompletionCancel
	onErrorAbort
)
