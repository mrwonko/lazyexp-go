package lazyexp_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mrwonko/lazyexp-go"
)

type ConstNode struct {
	lazyexp.Node
	value int
}

func NewConstNode(fetch func() int) *ConstNode {
	// we need this set-Fetcher-later idiom because we need a pointer into the object we're creating
	// (what we really want is to override a private virtual member function)
	res := &ConstNode{}
	res.Node = lazyexp.NewNode(nil, func([]error) error {
		res.value = fetch()
		return nil
	})
	return res
}

func (c *ConstNode) Value() int {
	// could Fetch(c) here to be safe, but we know what we're doing
	return c.value
}

type SumNode struct {
	lazyexp.Node
	sum int
}

func NewSumNode(lhs, rhs *ConstNode) *SumNode {
	res := &SumNode{}
	res.Node = lazyexp.NewNode(
		lazyexp.Dependencies{lazyexp.AbortOnError(lhs), lazyexp.AbortOnError(rhs)},
		func([]error) error {
			res.sum = lhs.Value() + rhs.Value()
			return nil
		})
	return res
}

func (s *SumNode) Value() int {
	// could Fetch() here to be safe, but we know what we're doing
	return s.sum
}

func TestShouldFetchLazilyOnce(t *testing.T) {
	oneFetchCount := 0
	one := NewConstNode(func() int {
		oneFetchCount++
		return 1
	})
	sum := NewSumNode(one, one)
	sum.Fetch()
	if oneFetchCount != 1 {
		t.Errorf("expected 1 fetch after fetching sum initially, got %d", oneFetchCount)
	}
	if got := sum.Value(); got != 2 {
		t.Errorf("expected 1+1=2, got %d", got)
	}
	if oneFetchCount != 1 {
		t.Errorf("expected 1 fetch after getting sum initially, got %d", oneFetchCount)
	}
	sum.Fetch()
	if oneFetchCount != 1 {
		t.Errorf("still expected 1 fetch after fetching sum again, got %d", oneFetchCount)
	}
	if got := sum.Value(); got != 2 {
		t.Errorf("still expected 1+1=2, got %d", got)
	}
	if oneFetchCount != 1 {
		t.Errorf("still expected 1 fetch after getting sum again, got %d", oneFetchCount)
	}
}

func TestShouldFetchInParallel(t *testing.T) {
	// two leafs that block until the other one is being evaluated
	fetchStarted := [...]chan struct{}{
		make(chan struct{}, 1),
		make(chan struct{}, 1),
	}
	leafs := [2]*ConstNode{}
	for i := range leafs {
		j := i
		leafs[i] = NewConstNode(func() int {
			close(fetchStarted[j])
			// wait for other node to be fetched
			<-fetchStarted[1-j]
			return j
		})
	}
	root := NewSumNode(leafs[0], leafs[1])
	// this will block indefinitely if the values are fetched sequentially
	root.Fetch()
	if got := root.Value(); got != 1 {
		t.Errorf("expected 0+1=1, got %d", got)
	}
}

func TestCancelOnError(t *testing.T) {
	// strictly speaking reusing these nodes means the prefetch optimization will take different code paths on later subtests, so their order could matter... but it shouldn't
	var (
		nilNode      = lazyexp.NewNode(nil, func([]error) error { return nil })
		err1         = errors.New("child error 1")
		err2         = errors.New("child error 2")
		fetchErr     = errors.New("root fetch failure")
		errNode1     = lazyexp.NewNode(nil, func([]error) error { return err1 })
		errNode2     = lazyexp.NewNode(nil, func([]error) error { return err2 })
		ctx, cancel  = context.WithCancel(context.Background())
		noreturnNode = lazyexp.NewNode(nil, func(_ []error) error {
			<-ctx.Done() // does not return until test is over
			return ctx.Err()
		})
	)
	defer cancel()
	for _, testIO := range []struct {
		name         string
		dependencies lazyexp.Dependencies
		expectErrs   []error // nil if no fetch expected
		expectResult error
	}{
		{"ContinueOnError: single error", lazyexp.Dependencies{lazyexp.ContinueOnError(errNode1)}, []error{err1}, fetchErr},
		{"ContinueOnError: multi error", lazyexp.Dependencies{lazyexp.ContinueOnError(errNode1), lazyexp.ContinueOnError(errNode2)}, []error{err1, err2}, fetchErr},

		{"AbortOnError: single success", lazyexp.Dependencies{lazyexp.AbortOnError(nilNode)}, []error{nil}, fetchErr},
		{"AbortOnError: single error", lazyexp.Dependencies{lazyexp.AbortOnError(errNode1)}, nil, err1},
		{"AbortOnError: cancelling error", lazyexp.Dependencies{lazyexp.AbortOnError(errNode1), lazyexp.ContinueOnError(noreturnNode)}, nil, err1},

		{"CancelOnError: single success", lazyexp.Dependencies{lazyexp.CancelOnError(nilNode)}, []error{nil}, fetchErr},
		{"CancelOnError: multi success", lazyexp.Dependencies{lazyexp.CancelOnError(nilNode), lazyexp.CancelOnError(nilNode)}, []error{nil, nil}, fetchErr},
		{"CancelOnError: single failure", lazyexp.Dependencies{lazyexp.CancelOnError(errNode1)}, []error{err1}, fetchErr},
		{"CancelOnError: cancelling failure", lazyexp.Dependencies{lazyexp.CancelOnError(errNode1), lazyexp.ContinueOnError(noreturnNode)}, []error{err1, context.Canceled}, fetchErr},

		// TODO: CancelOnCompletion
		// TODO: CancelOnSuccess
	} {
		t.Run(testIO.name, func(t *testing.T) {
			var (
				fetched = false
				check   = lazyexp.NewNode(
					testIO.dependencies,
					func(errs []error) error {
						if testIO.expectErrs == nil {
							t.Errorf("did not expect fetch to be called!")
						} else if !reflect.DeepEqual(errs, testIO.expectErrs) {
							t.Errorf("expected errs %v, got %v", testIO.expectErrs, errs)
						}
						fetched = true
						return fetchErr
					},
				)
				result = check.Fetch()
			)
			if result != testIO.expectResult {
				t.Errorf("expected fetch to return %v, got %v", testIO.expectResult, result)
			}
			if testIO.expectErrs != nil && !fetched {
				t.Errorf("expected fetch function to be called")
			}
		})
	}
}

// TODO: Think of a way to test strict mode. But that's testing for the absence of an event (early completion) again, which isn't reliable, right?

// TODO: test precheckDependencies
