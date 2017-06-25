package lazyexp

import (
	"sync"
	"sync/atomic"
	"time"
)

// A Node represents a blocking calculation. Create one using NewNode().
//
// You'll probably want to embed a Node in your struct that will contain the result.
type Node interface {
	Fetch() error
	// FetchStrict never cancels siblings on error and can be useful for debugging, but should generally be avoided.
	FetchStrict() error
	String() string
	fetched() bool
	flatten(*nodeFlattener) ID
	noUserImplementations()
}

// A Dependency is a Node whose result another Node depends on.
//
// Created using ContinueOnError(), CancelOnError(), CancelOnCompletion() and AbortOnError().
type Dependency struct {
	node         Node
	onCompletion completionStrategy
}

// Discarded is the error for Dependencies whose results were discarded
var Discarded DiscardedError

// DiscardedError is the type of Discarded
type DiscardedError struct{}

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

// DiscardOnError returns a Dependency where any Node Fetch() error causes sibling Dependencies' Fetches to be discarded, but still continues with the fetch function.
func DiscardOnError(node Node) Dependency {
	return Dependency{
		node:         node,
		onCompletion: csError | csCancel,
	}
}

// DiscardOnCompletion returns a Dependency that upon Node Fetch() completion causes sibling Dependencies' Fetches to be discarded, but still continues with the fetch function.
func DiscardOnCompletion(node Node) Dependency {
	return Dependency{
		node:         node,
		onCompletion: csError | csSuccess | csCancel,
	}
}

// DiscardOnSuccess returns a Dependency that upon Node Fetch() success causes sibling Dependencies' Fetches to be discarded, but still continues with the fetch function.
func DiscardOnSuccess(node Node) Dependency {
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

// NodeFetcher is the backend for a Node that does the actual retrieval.
//
// Fetch will be called with the errors from the dependencies, unless they are fatal, or Discarded if they were discarded.
//
// The dependencies will be fetched in parallel before fetch() is called and must not contain zero values. Don't introduce circular dependencies or the Fetch will deadlock waiting for itself to finish.
//
// This intentionally has a function named Fetch(), just like Node, because you're not supposed to implement this on your own node types that embed Node. You should instead have a separate, private fetcher type so that the fetching backend is not exposed on your node type:
//
//    type PubliclyVisibleNode struct {
//    	lazyexp.Node // embed Node so this can be passed as a Dependency for other Nodes
//    	data         string
//    }
//
//    func (pvn *PubliclyVisibleNode) Data() string {
//    	pvn.Fetch()
//    	return pvn.data
//    }
//
//    func NewPubliclyVisibleNode() *PubliclyVisibleNode {
//    	node := PubliclyVisibleNode{}
//    	node.Node = lazyexp.NewNode(privateNodeFetcher{&node})
//    	return &node
//    }
//
//    // separate fetch type to keep PubliclyVisibleNode's interface clean
//    type privateNodeFetcher struct {
//    	node *PubliclyVisibleNode
//    }
//
//    func (fetcher privateNodeFetcher) Dependencies() lazyexp.Dependencies {
//    	return nil // this example uses no Dependencies
//    }
//
//    func (fetcher privateNodeFetcher) Fetch([]error) error {
//    	var err error
//    	fetcher.node.data, err = someSlowFetchingFunction()
//    	return err
//    }
//
//    func (fetcher privateNodeFetcher) String(successfullyFetched bool) string {
//    	if successfullyFetched {
//    		return "PubliclyVisibleNode: " + fetcher.node.data
//    	}
//    	return "PubliclyVisibleNode (not fetched)"
//    }
type NodeFetcher interface {
	Dependencies() Dependencies
	Fetch([]error) error
	String(successfullyFetched bool) string
}

type funcNodeFetcher struct {
	dependencies Dependencies
	fetch        func([]error) error
	toString     func(successfullyFetched bool) string
}

var _ NodeFetcher = funcNodeFetcher{}

func (fnf funcNodeFetcher) Dependencies() Dependencies {
	return fnf.dependencies
}

func (fnf funcNodeFetcher) Fetch(errs []error) error {
	return fnf.fetch(errs)
}

func (fnf funcNodeFetcher) String(successfullyFetched bool) string {
	return fnf.toString(successfullyFetched)
}

// NewFuncNodeFetcher creates a NodeFetcher backed by the given functions, in case you don't want to write a whole type.
func NewFuncNodeFetcher(dependencies Dependencies, fetch func([]error) error, toString func(successfullyFetched bool) string) NodeFetcher {
	return funcNodeFetcher{
		dependencies,
		fetch,
		toString,
	}
}

// NewNode returns a Node backed by the given NodeFetcher.
func NewNode(nodeFetcher NodeFetcher) Node {
	return newNode(nodeFetcher)
}

// Join is a shorthand for creating a Node that simply waits on all its Dependencies, ignoring their errors (if they continue on error) unless you supply the optional collapseErrors function.
func Join(dependencies Dependencies, collapseErrors func([]error) error) Node {
	if collapseErrors == nil {
		collapseErrors = func([]error) error { return nil }
	}
	return NewNode(NewFuncNodeFetcher(dependencies, collapseErrors, func(bool) string { return "join" }))
}

func newNode(nodeFetcher NodeFetcher) *node {
	return &node{
		fetcher:      nodeFetcher,
		dependencies: nodeFetcher.Dependencies(),
		fetchedChan:  make(chan struct{}),
		fetchingChan: make(chan struct{}),
	}
}

var _ error = DiscardedError{}

func (d DiscardedError) Error() string {
	return "discarded"
}

type node struct {
	fetcher      NodeFetcher
	dependencies Dependencies
	depErrs      []error
	once         sync.Once
	err          error
	iFetched     int32
	fetchedChan  chan struct{}
	iFetching    int32
	fetchingChan chan struct{}
	start, end   time.Time
}

func (n *node) Fetch() error { return n.fetch(false) }

func (n *node) FetchStrict() error { return n.fetch(true) }

func (n *node) complete(err error) {
	n.err = err
	atomic.StoreInt32(&n.iFetched, 1)
	close(n.fetchedChan)
}

func (n *node) fetch(strict bool) error {
	n.once.Do(func() {
		data := precheckDependencies(n.dependencies)
		n.depErrs = data.errs
		if data.abort && !strict {
			n.complete(data.abortErr)
			return
		}
		fetch := func(dep dependencyIndex) error { return dep.node.Fetch() }
		if strict {
			fetch = func(dep dependencyIndex) error { return dep.node.FetchStrict() }
			// in strict mode, nothing cancels
			data.nonCancellingDependencies = append(data.nonCancellingDependencies, data.cancellingDependencies...)
			data.cancellingDependencies = nil
		}
		if !data.cancel || strict {
			// fetch dependencies
			switch len(data.cancellingDependencies) {
			case 0: // no cancelling dependencies, just fetch everything
				fetchUncanceled(data.nonCancellingDependencies, fetch, data.errs)
			case 1: // exactly one cancelling dependency - we can do that on this thread
				if abort, abortErr := fetchSingleCancel(data.cancellingDependencies[0], data.nonCancellingDependencies, fetch, data.errs); !data.abort {
					data.abort = abort
					data.abortErr = abortErr
				}
			default: // multiple cancelling dependencies
				if abort, abortErr := fetchMultiCancel(data.cancellingDependencies, data.nonCancellingDependencies, fetch, data.errs); !data.abort {
					data.abort = abort
					data.abortErr = abortErr
				}
			}
		}
		if data.abort {
			n.complete(data.abortErr)
		} else {
			// fetch this node
			atomic.StoreInt32(&n.iFetching, 1)
			n.start = time.Now()
			close(n.fetchingChan)
			err := n.fetcher.Fetch(data.errs)
			n.end = time.Now()
			n.complete(err)
		}
	})
	return n.err
}

type precheckedDependencies struct {
	cancellingDependencies    []dependencyIndex
	nonCancellingDependencies []dependencyIndex
	errs                      []error // result of fetching the dependencies
	abort                     bool
	abortErr                  error // if abort == true, this is the reason for abortion
	cancel                    bool
}

func precheckDependencies(dependencies Dependencies) (result precheckedDependencies) {
	result.cancellingDependencies = make([]dependencyIndex, 0, len(dependencies))
	result.nonCancellingDependencies = make([]dependencyIndex, 0, len(dependencies))
	result.errs = make([]error, len(dependencies))
	// find dependencies that are not yet fetched
	for i, dep := range dependencies {
		if dep.node.fetched() {
			// but dependencies that are already fetched may cause cancellation/abortion
			result.errs[i] = dep.node.Fetch()
			if dep.onCompletion.Cancel() && dep.onCompletion.Match(result.errs[i]) {
				result.cancel = true
			} else if dep.onCompletion.Abort() && dep.onCompletion.Match(result.errs[i]) {
				result.abortErr = result.errs[i]
				result.abort = true
			}
		} else {
			if dep.onCompletion.Cancel() || dep.onCompletion.Abort() {
				result.cancellingDependencies = append(result.cancellingDependencies, dependencyIndex{Dependency: dep, index: i})
			} else {
				result.nonCancellingDependencies = append(result.nonCancellingDependencies, dependencyIndex{Dependency: dep, index: i})
			}
		}
	}
	if result.cancel {
		for _, dep := range result.cancellingDependencies {
			result.errs[dep.index] = Discarded
		}
		for _, dep := range result.nonCancellingDependencies {
			result.errs[dep.index] = Discarded
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

func fetchSingleCancel(cancellingDep dependencyIndex, nonCancellingDeps []dependencyIndex, fetch func(dependencyIndex) error, errs []error) (abort bool, abortErr error) {
	if len(nonCancellingDeps) == 0 {
		// nothing being canceled
		errs[cancellingDep.index] = fetch(cancellingDep)
		if cancellingDep.onCompletion.Abort() && cancellingDep.onCompletion.Match(errs[cancellingDep.index]) {
			abort = true
			abortErr = errs[cancellingDep.index]
		}
	} else {
		// need to be careful to only set each err once
		once := make([]sync.Once, len(nonCancellingDeps))
		var wg sync.WaitGroup
		wg.Add(len(nonCancellingDeps))
		for i := range nonCancellingDeps {
			go func(i int) {
				err := fetch(nonCancellingDeps[i])
				once[i].Do(func() { errs[nonCancellingDeps[i].index] = err })
				wg.Done()
			}(i)
		}
		errs[cancellingDep.index] = fetch(cancellingDep)
		if cancellingDep.onCompletion.Match(errs[cancellingDep.index]) {
			// set unfinished dependencies' error
			for i := range nonCancellingDeps {
				once[i].Do(func() { errs[nonCancellingDeps[i].index] = Discarded })
			}
			if cancellingDep.onCompletion.Abort() {
				abort = true
				abortErr = errs[cancellingDep.index]
			}
		} else {
			// did not cause cancellation, await other dependencies
			wg.Wait()
		}
	}
	return
}

func fetchMultiCancel(cancelingDeps []dependencyIndex, nonCancelingDeps []dependencyIndex, fetch func(dependencyIndex) error, errs []error) (abort bool, abortErr error) {
	type doneEvent struct {
		index int
		err   error
	}
	// these channels are chosen large enough to hold all events in case of cancellation/abortion
	doneChan := make(chan doneEvent, len(cancelingDeps)+len(nonCancelingDeps))
	cancelChan := make(chan doneEvent, len(cancelingDeps))
	abortChan := make(chan doneEvent, len(cancelingDeps))
	remainingIndices := map[int]struct{}{}
	for _, dep := range nonCancelingDeps {
		remainingIndices[dep.index] = struct{}{}
		go func(dep dependencyIndex) {
			doneChan <- doneEvent{dep.index, fetch(dep)}
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
				errs[i] = Discarded
			}
			remainingIndices = nil
		case ev := <-abortChan:
			abort = true
			abortErr = ev.err
			// the errors are still of interest in serialization
			errs[ev.index] = ev.err
			delete(remainingIndices, ev.index)
			for i := range remainingIndices {
				errs[i] = Discarded
			}
			remainingIndices = nil
		}
	}
	return
}

// fetched implies that n.depErrs has been set and n.start and n.end won't change any more (though they may be zero in case of abortion)
func (n *node) fetched() bool {
	fetched := atomic.LoadInt32(&n.iFetched) != 0
	if fetched {
		// atomic reads are no write barriers, so we still need to synchronize before we can access the result
		<-n.fetchedChan
	}
	return fetched
}

// fetching implies that n.start and n.depErrs have been set
func (n *node) fetching() bool {
	fetching := atomic.LoadInt32(&n.iFetching) != 0
	if fetching {
		// atomic reads are no write barriers, so we still need to synchronize before we can access the result
		<-n.fetchingChan
	}
	return fetching
}

func (n *node) flatten(nf *nodeFlattener) ID {
	id, visited := nf.getID(n)
	if visited {
		return id
	}
	fn := FlatNode{
		ID:           id,
		Child:        NoChild,
		Dependencies: make([]FlatDependency, len(n.dependencies)),
	}
	fetching := n.fetching()
	fetched := n.fetched()
	if fetching {
		fn.FetchStartTime = n.start
	}
	if fetched {
		fn.Evaluated = true
		fn.Err = n.err
		fn.FetchEndTime = n.end
	}
	fn.Description = n.String()
	dependenciesComplete := fetched || fetching
	for i, dep := range n.dependencies {
		fn.Dependencies[i].ID = dep.node.flatten(nf)
		if dependenciesComplete {
			// FIXME: handle abortion
			fn.Dependencies[i].Err = n.depErrs[i]
		}
	}
	nf.result[id] = fn
	return id
}

func (n *node) String() string {
	return n.fetcher.String(n.fetched() && n.err == nil)
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
