package lazyexp_test

import (
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
	res.Node = lazyexp.NewNode(nil, func() {
		res.value = fetch()
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
		lazyexp.Dependencies{lhs, rhs},
		func() {
			res.sum = lhs.Value() + rhs.Value()
		})
	return res
}

func (s *SumNode) Value() int {
	// could Fetch(s) here to be safe, but we know what we're doing
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
