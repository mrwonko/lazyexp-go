package lazyexp

import (
	"sync"
	"testing"
)

type ConstNode struct {
	FetchValue func() int
	value      int
	once       sync.Once
}

func (c *ConstNode) Value() int {
	c.once.Do(func() {
		c.value = c.FetchValue()
	})
	return c.value
}

type SumNode struct {
	LHS  *ConstNode // must be pointers as we want to share nodes
	RHS  *ConstNode
	sum  int
	once sync.Once
}

func (s *SumNode) Value() int {
	s.once.Do(func() {
		var lhsVal, rhsVal int
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			lhsVal = s.LHS.Value()
			wg.Done()
		}()
		go func() {
			rhsVal = s.RHS.Value()
			wg.Done()
		}()
		wg.Wait()
		s.sum = lhsVal + rhsVal
	})
	return s.sum
}

func TestShouldFetchLazilyOnce(t *testing.T) {
	oneFetchCount := 0
	one := ConstNode{
		FetchValue: func() int {
			oneFetchCount++
			return 1
		},
	}
	sum := SumNode{
		LHS: &one,
		RHS: &one,
	}
	if got := sum.Value(); got != 2 {
		t.Errorf("expected 1+1=2, got %d", got)
	}
	if oneFetchCount != 1 {
		t.Errorf("expected 1 fetch after getting sum initially, got %d", oneFetchCount)
	}
	if got := sum.Value(); got != 2 {
		t.Errorf("still expected 1+1=2, got %d", got)
	}
	if oneFetchCount != 1 {
		t.Errorf("still expected 1 fetch after getting sum again, got %d", oneFetchCount)
	}
}
