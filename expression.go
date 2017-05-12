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
	fetch(ctx context.Context) FetchError
}

// FetchError is returned by the user fetch() function passed to NewNode() to indicate whether fetching of other dependencies should be canceled.
//
// See FatalIfError(), AcceptableError() and Cancel(), or use nil to continue normally.
type FetchError interface {
	fetchError()
}

// AcceptableError returns a FetchError that is simply forwarded to the fetch function without affecting any other nodes.
func AcceptableError(err error) FetchError {
	return fetchErrorAcceptable{err: err}
}

// FatalIfError returns a FetchError that will immediately cancel the complete Fetch recursively and return the given error at top level, unless it's called with nil, in which case it returns nil.
func FatalIfError(err error) FetchError {
	if err == nil {
		return nil
	}
	return fetchErrorFatal{err: err}
}

// Cancel returns a FetchError that will cancel the siblings of the Node being Fetch()ed and return the given error for it (which may be nil).
func Cancel(err error) FetchError {
	return fetchErrorCancel{err: err}
}

// Dependencies are Nodes that must be Fetched before the given one can be.
type Dependencies []Node // TODO: should this be Node + ErrorStrategy?

// NewNode returns a Node backed by the given function.
//
// The fetch function must not be nil, unless you never intend to Fetch() this Node. It will be called with the errors from the optional dependencies, unless they are fatal, or context.Canceled if they returned CancelFetchSuccess().
//
// The dependencies will be fetched in parallel before fetch() is called and must not contain nil.
//
// If you need to cancel fetching of other dependencies as soon as one is available, make them OptionalDependencies and cancel the context.
func NewNode(dependencies Dependencies, fetch func(context.Context, []error) FetchError) Node {
	// TODO: fetch func(context.Context) error - think about recoverable errors in particular
	return &node{
		fetcher:      fetch,
		dependencies: dependencies,
	}
}

type node struct {
	fetcher      func(context.Context, []error) FetchError
	dependencies Dependencies
	once         sync.Once
	err          FetchError
}

func (n *node) Fetch(ctx context.Context) error {
	switch err := n.fetch(ctx).(type) {
	case fetchErrorAcceptable:
		return err.err
	case fetchErrorCancel:
		return err.err
	case fetchErrorFatal:
		return err.err
	default: // must be nil
		return nil
	}
}

func (n *node) fetch(ctx context.Context) FetchError {
	n.once.Do(func() {
		var errs []error
		// fetch dependencies in parallel
		if l := len(n.dependencies); l > 0 {
			errs = make([]error, l)
			if l == 1 {
				// no need to fetch single dependency in parallel
				switch err := n.dependencies[0].fetch(ctx).(type) {
				case fetchErrorAcceptable:
					errs[0] = err.err
				case fetchErrorCancel:
					// no siblings to cancel
					errs[0] = err.err
				case fetchErrorFatal:
					n.err = err
					return
				default: // must be nil
				}
			} else {
				// we can save one goroutine by fetching that dependency on the current one
				var wg sync.WaitGroup
				var fatalErr atomic.Value
				subCtx, cancel := context.WithCancel(ctx)
				wg.Add(l - 1)
				for i := 1; i < l; i++ {
					go func(i int) {
						errs[i] = fetchDependency(subCtx, cancel, n.dependencies[i], &fatalErr)
						wg.Done()
					}(i)
				}
				errs[0] = fetchDependency(subCtx, cancel, n.dependencies[0], &fatalErr)
				wg.Wait()
				cancel()
				if iErr := fatalErr.Load(); iErr != nil {
					n.err = iErr.(fetchErrorFatal)
					return
				}
			}

		}
		n.err = n.fetcher(ctx, errs)
	})
	return n.err
}

func fetchDependency(ctx context.Context, cancel func(), dependency Node, outFatalErr *atomic.Value) error {
	switch err := dependency.fetch(ctx).(type) {
	case fetchErrorAcceptable:
		return err.err
	case fetchErrorCancel:
		cancel()
		return err.err
	case fetchErrorFatal:
		outFatalErr.Store(err)
		return err.err // will be ignored due to fatal error anyway
	default: // must be nil
		return nil
	}
}

// TODO: use struct with err + enum instead?

type fetchErrorCancel struct {
	err error
}

func (err fetchErrorCancel) fetchError() {}

type fetchErrorFatal struct {
	err error // never nil
}

func (err fetchErrorFatal) fetchError() {}

type fetchErrorAcceptable struct {
	err error // never nil
}

func (err fetchErrorAcceptable) fetchError() {}

// ensure interface implementation
var (
	_ Node       = (*node)(nil)
	_ FetchError = fetchErrorCancel{}
	_ FetchError = fetchErrorFatal{}
	_ FetchError = fetchErrorAcceptable{}
)
