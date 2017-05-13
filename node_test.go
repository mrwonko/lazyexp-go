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
	res.Node = lazyexp.NewNode(nil, func(context.Context, []error) error {
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
		func(context.Context, []error) error {
			res.sum = lhs.Value() + rhs.Value()
			return nil
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
	sum.Fetch(context.Background())
	if oneFetchCount != 1 {
		t.Errorf("expected 1 fetch after fetching sum initially, got %d", oneFetchCount)
	}
	if got := sum.Value(); got != 2 {
		t.Errorf("expected 1+1=2, got %d", got)
	}
	if oneFetchCount != 1 {
		t.Errorf("expected 1 fetch after getting sum initially, got %d", oneFetchCount)
	}
	sum.Fetch(context.Background())
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
	root.Fetch(context.Background())
	if got := root.Value(); got != 1 {
		t.Errorf("expected 0+1=1, got %d", got)
	}
}

func TestContinueOnError(t *testing.T) {
	var (
		err1     = errors.New("error 1")
		err2     = errors.New("error 2")
		err3     = errors.New("error 3")
		errNode1 = lazyexp.NewNode(nil, func(context.Context, []error) error { return err1 })
		errNode2 = lazyexp.NewNode(nil, func(context.Context, []error) error { return err2 })
	)
	t.Run("single", func(t *testing.T) {
		var (
			checked = false
			check   = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.ContinueOnError(errNode1)},
				func(_ context.Context, errs []error) error {
					expected := []error{err1}
					if !reflect.DeepEqual(errs, expected) {
						t.Errorf("expected errors %v, got %v", expected, errs)
					}
					checked = true
					return err3
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err3 {
			t.Errorf("expected single fetch to return %v, got %v", err3, err)
		}
		if !checked {
			t.Errorf("expected single fetch function to be called")
		}
	})
	t.Run("multi", func(t *testing.T) {
		var (
			checked = false
			check   = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.ContinueOnError(errNode1), lazyexp.ContinueOnError(errNode2)},
				func(_ context.Context, errs []error) error {
					expected := []error{err1, err2}
					if !reflect.DeepEqual(errs, expected) {
						t.Errorf("expected errors %v, got %v", expected, errs)
					}
					checked = true
					return err3
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err3 {
			t.Errorf("expected multi fetch to return %v, got %v", err3, err)
		}
		if !checked {
			t.Errorf("expected multi fetch function to be called")
		}
	})
}

func TestAbortOnError(t *testing.T) {
	var (
		err1    = errors.New("error 1")
		err2    = errors.New("error 3")
		nilNode = lazyexp.NewNode(nil, func(context.Context, []error) error { return nil })
	)
	t.Run("single success", func(t *testing.T) {
		var (
			checked = false
			check   = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.AbortOnError(nilNode)},
				func(_ context.Context, errs []error) error {
					expected := []error{nil}
					if !reflect.DeepEqual(errs, expected) {
						t.Errorf("expected errs %v, got %v", expected, errs)
					}
					checked = true
					return err2
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err2 {
			t.Errorf("expected fetch to return %v, got %v", err2, err)
		}
		if !checked {
			t.Errorf("expected fetch function to be called")
		}
	})
	t.Run("multi success", func(t *testing.T) {
		var (
			checked = false
			check   = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.AbortOnError(nilNode), lazyexp.AbortOnError(nilNode)},
				func(_ context.Context, errs []error) error {
					expected := []error{nil, nil}
					if !reflect.DeepEqual(errs, expected) {
						t.Errorf("expected errs %v, got %v", expected, errs)
					}
					checked = true
					return err2
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err2 {
			t.Errorf("expected fetch to return %v, got %v", err2, err)
		}
		if !checked {
			t.Errorf("expected fetch function to be called")
		}
	})
	t.Run("single failure", func(t *testing.T) {
		var (
			errNode = lazyexp.NewNode(nil, func(context.Context, []error) error { return err1 })
			check   = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.AbortOnError(errNode)},
				func(context.Context, []error) error {
					t.Errorf("expected fetch to not be called")
					return err2
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err1 {
			t.Errorf("expected error %v, got %v", err1, err)
		}
	})
	t.Run("canceling failure", func(t *testing.T) {
		var (
			errNode    = lazyexp.NewNode(nil, func(context.Context, []error) error { return err1 })
			canceled   = false
			cancelNode = lazyexp.NewNode(nil, func(ctx context.Context, _ []error) error {
				<-ctx.Done()
				canceled = true
				if ctx.Err() != context.Canceled {
					t.Errorf("expected %v error, got %v", context.Canceled, ctx.Err())
				}
				return ctx.Err()
			})
			check = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.AbortOnError(errNode), lazyexp.AbortOnError(cancelNode)},
				func(context.Context, []error) error {
					t.Errorf("expected fetch not to be called")
					return err2
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err1 {
			t.Errorf("expected error %v, got %v", err1, err)
		}
		if !canceled {
			t.Errorf("expected sibling to get canceled")
		}
	})
}

func TestCancelOnError(t *testing.T) {
	var (
		nilNode = lazyexp.NewNode(nil, func(context.Context, []error) error { return nil })
		err1    = errors.New("error 1")
		err2    = errors.New("error 2")
		errNode = lazyexp.NewNode(nil, func(context.Context, []error) error { return err1 })
	)
	t.Run("single success", func(t *testing.T) {
		var (
			checked = false
			check   = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.CancelOnError(nilNode)},
				func(_ context.Context, errs []error) error {
					expected := []error{nil}
					if !reflect.DeepEqual(errs, expected) {
						t.Errorf("expected errs %v, got %v", expected, errs)
					}
					checked = true
					return err2
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err2 {
			t.Errorf("expected fetch to return %v, got %v", err2, err)
		}
		if !checked {
			t.Errorf("expected fetch function to be called")
		}
	})
	t.Run("multi success", func(t *testing.T) {
		var (
			checked = false
			check   = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.CancelOnError(nilNode), lazyexp.CancelOnError(nilNode)},
				func(_ context.Context, errs []error) error {
					expected := []error{nil, nil}
					if !reflect.DeepEqual(errs, expected) {
						t.Errorf("expected errs %v, got %v", expected, errs)
					}
					checked = true
					return err2
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err2 {
			t.Errorf("expected fetch to return %v, got %v", err2, err)
		}
		if !checked {
			t.Errorf("expected fetch function to be called")
		}
	})
	t.Run("single failure", func(t *testing.T) {
		var (
			checked = false
			check   = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.CancelOnError(errNode)},
				func(_ context.Context, errs []error) error {
					expected := []error{err1}
					if !reflect.DeepEqual(errs, expected) {
						t.Errorf("expected errs %v, got %v", expected, errs)
					}
					checked = true
					return err2
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err2 {
			t.Errorf("expected error %v, got %v", err2, err)
		}
	})
	t.Run("multi success", func(t *testing.T) {
		var (
			checked = false
			check   = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.CancelOnError(nilNode), lazyexp.CancelOnError(nilNode)},
				func(_ context.Context, errs []error) error {
					expected := []error{nil, nil}
					if !reflect.DeepEqual(errs, expected) {
						t.Errorf("expected errs %v, got %v", expected, errs)
					}
					checked = true
					return err2
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err2 {
			t.Errorf("expected error %v, got %v", err2, err)
		}
	})
	t.Run("canceling failure", func(t *testing.T) {
		var (
			checked     = false
			siblingNode = lazyexp.NewNode(nil, func(ctx context.Context, _ []error) error {
				<-ctx.Done()
				return ctx.Err()
			})
			check = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.CancelOnError(errNode), lazyexp.ContinueOnError(siblingNode)},
				func(_ context.Context, errs []error) error {
					expected := []error{err1, context.Canceled}
					if !reflect.DeepEqual(errs, expected) {
						t.Errorf("expected errs %v, got %v", expected, errs)
					}
					checked = true
					return err2
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err2 {
			t.Errorf("expected error %v, got %v", err2, err)
		}
	})
}

func TestCancelOnCompletion(t *testing.T) {
	var (
		nilNode = lazyexp.NewNode(nil, func(context.Context, []error) error { return nil })
		err1    = errors.New("error 1")
		err2    = errors.New("error 2")
		errNode = lazyexp.NewNode(nil, func(context.Context, []error) error { return err1 })
	)
	t.Run("single success", func(t *testing.T) {
		var (
			checked = false
			check   = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.CancelOnCompletion(nilNode)},
				func(_ context.Context, errs []error) error {
					expected := []error{nil}
					if !reflect.DeepEqual(errs, expected) {
						t.Errorf("expected errs %v, got %v", expected, errs)
					}
					checked = true
					return err2
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err2 {
			t.Errorf("expected fetch to return %v, got %v", err2, err)
		}
		if !checked {
			t.Errorf("expected fetch function to be called")
		}
	})
	t.Run("multi success", func(t *testing.T) {
		var (
			checked = false
			check   = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.CancelOnCompletion(nilNode), lazyexp.CancelOnCompletion(nilNode)},
				func(_ context.Context, errs []error) error {
					expected := []error{nil, nil}
					if !reflect.DeepEqual(errs, expected) {
						t.Errorf("expected errs %v, got %v", expected, errs)
					}
					checked = true
					return err2
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err2 {
			t.Errorf("expected fetch to return %v, got %v", err2, err)
		}
		if !checked {
			t.Errorf("expected fetch function to be called")
		}
	})
	t.Run("single failure", func(t *testing.T) {
		var (
			checked = false
			check   = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.CancelOnCompletion(errNode)},
				func(_ context.Context, errs []error) error {
					expected := []error{err1}
					if !reflect.DeepEqual(errs, expected) {
						t.Errorf("expected errs %v, got %v", expected, errs)
					}
					checked = true
					return err2
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err2 {
			t.Errorf("expected error %v, got %v", err2, err)
		}
	})
	t.Run("canceling success", func(t *testing.T) {
		var (
			checked     = false
			siblingNode = lazyexp.NewNode(nil, func(ctx context.Context, _ []error) error {
				<-ctx.Done()
				return ctx.Err()
			})
			check = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.CancelOnCompletion(nilNode), lazyexp.ContinueOnError(siblingNode)},
				func(_ context.Context, errs []error) error {
					expected := []error{nil, context.Canceled}
					if !reflect.DeepEqual(errs, expected) {
						t.Errorf("expected errs %v, got %v", expected, errs)
					}
					checked = true
					return err2
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err2 {
			t.Errorf("expected error %v, got %v", err2, err)
		}
	})
	t.Run("canceling failure", func(t *testing.T) {
		var (
			checked     = false
			siblingNode = lazyexp.NewNode(nil, func(ctx context.Context, _ []error) error {
				<-ctx.Done()
				return ctx.Err()
			})
			check = lazyexp.NewNode(
				lazyexp.Dependencies{lazyexp.CancelOnCompletion(errNode), lazyexp.ContinueOnError(siblingNode)},
				func(_ context.Context, errs []error) error {
					expected := []error{err1, context.Canceled}
					if !reflect.DeepEqual(errs, expected) {
						t.Errorf("expected errs %v, got %v", expected, errs)
					}
					checked = true
					return err2
				},
			)
			err = check.Fetch(context.Background())
		)
		if err != err2 {
			t.Errorf("expected error %v, got %v", err2, err)
		}
	})
}

func TestFetchStrict(t *testing.T) {
	var (
		done        = make(chan struct{})
		err1        = errors.New("error 1")
		errNode     = lazyexp.NewNode(nil, func(context.Context, []error) error { return err1 })
		siblingNode = lazyexp.NewNode(nil, func(ctx context.Context, _ []error) error {
			go func() {
				select {
				case <-ctx.Done():
					t.Errorf("did not expect sibling to get canceled in strict mode")
				case <-done:
				}
			}()
			return nil
		})
		check = lazyexp.NewNode(
			lazyexp.Dependencies{lazyexp.AbortOnError(errNode), lazyexp.ContinueOnError(siblingNode)},
			func(context.Context, []error) error {
				t.Errorf("did not expect fetch to get called")
				return errors.New("wat")
			},
		)
		err = check.FetchStrict(context.Background())
	)
	done <- struct{}{}
	if err != err1 {
		t.Errorf("expected %v, got %v", err1, err)
	}
}
