package lazyexp

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
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

type dependencyIndex struct {
	Dependency
	index int
}

// ContinueOnError returns a Dependency where any Node Fetch() errors are simply passed on without affecting any other Nodes.
func ContinueOnError(node Node) Dependency {
	return Dependency{
		node:         node,
		onCompletion: 0,
	}
}

// CancelOnError returns a Dependency where any Node Fetch() error causes sibling Dependencies' Fetches to be canceled, but still continues with the fetch function.
func CancelOnError(node Node) Dependency {
	return Dependency{
		node:         node,
		onCompletion: csError | csCancel,
	}
}

// CancelOnCompletion returns a Dependency that upon Node Fetch() completion causes sibling Dependencies' Fetches to be canceled, but still continues with the fetch function.
func CancelOnCompletion(node Node) Dependency {
	return Dependency{
		node:         node,
		onCompletion: csError | csSuccess | csCancel,
	}
}

// CancelOnSuccess returns a Dependency that upon Node Fetch() success causes sibling Dependencies' Fetches to be canceled, but still continues with the fetch function.
func CancelOnSuccess(node Node) Dependency {
	return Dependency{
		node:         node,
		onCompletion: csSuccess | csCancel,
	}
}

// AbortOnError returns a Dependency where any Node Fetch() error causes sibling Dependencies' Fetches to be canceled and propagates the error.
//
// TODO: enrich the error with context before passing it on once Nodes have a description
func AbortOnError(node Node) Dependency {
	return Dependency{
		node:         node,
		onCompletion: csError | csAbort,
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
		fetchedChan:  make(chan struct{}),
	}
}

type node struct {
	fetcher      func(context.Context, []error) error
	dependencies Dependencies
	once         sync.Once
	err          error
	iFetched     int32
	fetchedChan  chan struct{}
	start, end   time.Time
}

func (n *node) Fetch(ctx context.Context) error { return n.fetch(ctx, false) }

func (n *node) FetchStrict(ctx context.Context) error { return n.fetch(ctx, true) }

func (n *node) complete(err error) {
	n.err = err
	atomic.StoreInt32(&n.iFetched, 1)
	close(n.fetchedChan)
}

func (n *node) fetch(ctx context.Context, strict bool) error {
	n.once.Do(func() {

		data := precheckDependencies(ctx, n.dependencies)
		if data.abort && !strict {
			n.complete(data.abortErr)
			return
		}
		fetch := func(dep dependencyIndex) error { return dep.node.Fetch(ctx) }
		if strict {
			fetch = func(dep dependencyIndex) error { return dep.node.FetchStrict(ctx) }
			// in strict mode, nothing cancels
			data.nonCancelingDependencies = append(data.nonCancelingDependencies, data.cancelingDependencies...)
			data.cancelingDependencies = nil
		}
		if !data.cancel || strict {
			// fetch dependencies
			switch len(data.cancelingDependencies) {
			case 0: // no cancelling dependencies, just fetch everything
				fetchUncanceled(data.nonCancelingDependencies, fetch, data.errs)
			case 1: // exactly one cancelling dependency - we can do that on this thread
				if abort, abortErr := fetchSingleCancel(data.cancelingDependencies[0], data.nonCancelingDependencies, fetch, data.errs); !data.abort {
					data.abort = abort
					data.abortErr = abortErr
				}
			default: // multiple cancelling dependencies
				if abort, abortErr := fetchMultiCancel(data.cancelingDependencies, data.nonCancelingDependencies, strict, fetch, data.errs); !data.abort {
					data.abort = abort
					data.abortErr = abortErr
				}
			}
		}
		if data.abort {
			n.complete(data.abortErr)
		} else {
			// fetch this node
			n.start = time.Now()
			err := n.fetcher(ctx, data.errs)
			n.end = time.Now()
			n.complete(err)
		}
	})
	return n.err
}

func precheckDependencies(ctx context.Context, dependencies Dependencies) (result struct {
	cancelingDependencies    []dependencyIndex
	nonCancelingDependencies []dependencyIndex
	errs                     []error // result of fetching the dependencies
	abort                    bool
	abortErr                 error // if abort == true, this is the reason for abortion
	cancel                   bool
}) {
	result.cancelingDependencies = make([]dependencyIndex, 0, len(dependencies))
	result.nonCancelingDependencies = make([]dependencyIndex, 0, len(dependencies))
	result.errs = make([]error, len(dependencies))
	// find dependencies that are not yet fetched
	for i, dep := range dependencies {
		if dep.node.fetched() {
			// but dependencies that are already fetched may cause cancellation/abortion
			result.errs[i] = dep.node.Fetch(ctx)
			if dep.onCompletion.Cancel() && dep.onCompletion.Match(result.errs[i]) {
				result.cancel = true
			} else if dep.onCompletion.Abort() && dep.onCompletion.Match(result.errs[i]) {
				result.abortErr = result.errs[i]
				result.abort = true
			}
		} else {
			if dep.onCompletion.Cancel() || dep.onCompletion.Abort() {
				result.cancelingDependencies = append(result.cancelingDependencies, dependencyIndex{Dependency: dep, index: i})
			} else {
				result.nonCancelingDependencies = append(result.nonCancelingDependencies, dependencyIndex{Dependency: dep, index: i})
			}
		}
	}
	if result.cancel {
		for _, dep := range result.cancelingDependencies {
			result.errs[dep.index] = context.Canceled
		}
		for _, dep := range result.nonCancelingDependencies {
			result.errs[dep.index] = context.Canceled
		}
	}
	return
}

func fetchUncanceled(deps []dependencyIndex, fetch func(dependencyIndex) error, errs []error) {
	switch len(deps) {
	case 0: // nothing to do
	case 1:
		errs[deps[0].index] = fetch(deps[0])
	default:
		var wg sync.WaitGroup
		// one calculation can be done on this thread
		wg.Add(len(deps) - 1)
		for i := 1; i < len(deps); i++ {
			go func(i int) {
				errs[deps[i].index] = fetch(deps[i])
				wg.Done()
			}(i)
		}
		errs[deps[0].index] = fetch(deps[0])
		wg.Wait()
	}
}

func fetchSingleCancel(cancelingDep dependencyIndex, nonCancelingDeps []dependencyIndex, fetch func(dependencyIndex) error, errs []error) (abort bool, abortErr error) {
	if len(nonCancelingDeps) == 0 {
		// nothing being canceled
		errs[cancelingDep.index] = fetch(cancelingDep)
		if cancelingDep.onCompletion.Abort() && cancelingDep.onCompletion.Match(errs[cancelingDep.index]) {
			abort = true
			abortErr = errs[cancelingDep.index]
		}
	} else {
		// need to be careful to only set each err once
		once := make([]sync.Once, len(nonCancelingDeps))
		var wg sync.WaitGroup
		wg.Add(len(nonCancelingDeps))
		for i := range nonCancelingDeps {
			go func(i int) {
				err := fetch(nonCancelingDeps[i])
				once[i].Do(func() { errs[nonCancelingDeps[i].index] = err })
				wg.Done()
			}(i)
		}
		errs[cancelingDep.index] = fetch(cancelingDep)
		if cancelingDep.onCompletion.Match(errs[cancelingDep.index]) {
			if cancelingDep.onCompletion.Abort() {
				abort = true
				abortErr = errs[cancelingDep.index]
			} else {
				// canceled, set unfinished dependencies' error
				for i := range nonCancelingDeps {
					once[i].Do(func() { errs[nonCancelingDeps[i].index] = context.Canceled })
				}
			}
		} else {
			// did not cause cancellation, await other dependencies
			wg.Wait()
		}
	}
	return
}

func fetchMultiCancel(cancelingDeps []dependencyIndex, nonCancelingDeps []dependencyIndex, strict bool, fetch func(dependencyIndex) error, errs []error) (abort bool, abortErr error) {
	type doneEvent struct {
		index int
		err   error
	}
	// these channels are chosen large enough to hold all events in case of cancellation/abortion
	doneChan := make(chan doneEvent, len(cancelingDeps)+len(nonCancelingDeps))
	cancelChan := make(chan doneEvent, len(cancelingDeps))
	abortChan := make(chan doneEvent, len(cancelingDeps))
	remainingIndices := map[int]struct{}{}
	var wg sync.WaitGroup
	wg.Add(len(cancelingDeps) + len(nonCancelingDeps))
	for _, dep := range nonCancelingDeps {
		remainingIndices[dep.index] = struct{}{}
		go func(dep dependencyIndex) {
			doneChan <- doneEvent{dep.index, fetch(dep)}
			wg.Done()
		}(dep)
	}
	for _, dep := range cancelingDeps {
		remainingIndices[dep.index] = struct{}{}
		go func(dep dependencyIndex) {
			ev := doneEvent{dep.index, fetch(dep)}
			if dep.onCompletion.Abort() && dep.onCompletion.Match(ev.err) {
				abortChan <- ev
			} else if dep.onCompletion.Cancel() && dep.onCompletion.Match(ev.err) {
				cancelChan <- ev
			} else {
				doneChan <- ev
			}
			wg.Done()
		}(dep)
	}
	for len(remainingIndices) > 0 {
		select {
		case ev := <-doneChan:
			errs[ev.index] = ev.err
			delete(remainingIndices, ev.index)
		case ev := <-cancelChan:
			errs[ev.index] = ev.err
			delete(remainingIndices, ev.index)
			for i := range remainingIndices {
				errs[i] = context.Canceled
			}
			remainingIndices = nil
		case ev := <-abortChan:
			abort = true
			abortErr = ev.err
			remainingIndices = nil
		}
	}
	if strict {
		wg.Wait()
	}
	return
}

func (n *node) fetched() bool {
	fetched := atomic.LoadInt32(&n.iFetched) != 0
	if fetched {
		// atomic reads are no write barriers, so we still need to synchronize before we can access the result
		<-n.fetchedChan
	}
	return fetched
}

func (n *node) noUserImplementations() {}

type completionStrategy int

func (cs completionStrategy) Match(err error) bool {
	return cs&csSuccess != 0 && err == nil ||
		cs&csError != 0 && err != nil
}

func (cs completionStrategy) Cancel() bool {
	return cs&csCancel != 0
}

func (cs completionStrategy) Abort() bool {
	return cs&csAbort != 0
}

const (
	csError completionStrategy = 1 << iota
	csSuccess
	csCancel
	csAbort
)
