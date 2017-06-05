package lazyexp

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestPrivateFunctions(t *testing.T) {
	var (
		err            = errors.New("err")
		fetchedSuccess = NewNode(nil, func([]error) error { return nil }, func(bool) string { return "success" })
		fetchedFailure = NewNode(nil, func([]error) error { return err }, func(bool) string { return "failure" })
		ctx, cancel    = context.WithCancel(context.Background())
		unfechted      = NewNode(nil, func([]error) error { <-ctx.Done(); return nil }, func(bool) string { return "fetching" })
		continuing     = ContinueOnError(unfechted)
		cancelling     = DiscardOnError(unfechted)
		aborting       = AbortOnError(unfechted)
		canceled       = DiscardOnError(fetchedFailure)
		aborted        = AbortOnError(fetchedFailure)
		success        = ContinueOnError(fetchedSuccess)
		failure        = ContinueOnError(fetchedFailure)
	)
	fetchedSuccess.Fetch()
	fetchedFailure.Fetch()
	defer cancel()
	t.Run("precheckDependencies", func(t *testing.T) {
		for _, testIO := range []struct {
			name         string
			dependencies Dependencies
			expected     precheckedDependencies
		}{
			{
				"no dependencies",
				nil,
				precheckedDependencies{
					abort:    false,
					abortErr: nil,
					cancel:   false,
					errs:     []error{},
					cancellingDependencies:    []dependencyIndex{},
					nonCancellingDependencies: []dependencyIndex{},
				},
			},
			{
				"success",
				Dependencies{success},
				precheckedDependencies{
					abort:    false,
					abortErr: nil,
					cancel:   false,
					errs:     []error{nil},
					cancellingDependencies:    []dependencyIndex{},
					nonCancellingDependencies: []dependencyIndex{},
				},
			},
			{
				"failure",
				Dependencies{failure},
				precheckedDependencies{
					abort:    false,
					abortErr: nil,
					cancel:   false,
					errs:     []error{err},
					cancellingDependencies:    []dependencyIndex{},
					nonCancellingDependencies: []dependencyIndex{},
				},
			},
			{
				"unfetched continue",
				Dependencies{continuing},
				precheckedDependencies{
					abort:    false,
					abortErr: nil,
					cancel:   false,
					errs:     []error{nil},
					cancellingDependencies:    []dependencyIndex{},
					nonCancellingDependencies: []dependencyIndex{{continuing, 0}},
				},
			},
			{
				"unfetched cancel",
				Dependencies{cancelling},
				precheckedDependencies{
					abort:    false,
					abortErr: nil,
					cancel:   false,
					errs:     []error{nil},
					cancellingDependencies:    []dependencyIndex{{cancelling, 0}},
					nonCancellingDependencies: []dependencyIndex{},
				},
			},
			{
				"unfetched abort",
				Dependencies{aborting},
				precheckedDependencies{
					abort:    false,
					abortErr: nil,
					cancel:   false,
					errs:     []error{nil},
					cancellingDependencies:    []dependencyIndex{{aborting, 0}},
					nonCancellingDependencies: []dependencyIndex{},
				},
			},
			{
				"cancelled",
				Dependencies{canceled, continuing, cancelling},
				precheckedDependencies{
					abort:    false,
					abortErr: nil,
					cancel:   true,
					errs:     []error{err, Discarded, Discarded},
					cancellingDependencies:    []dependencyIndex{{cancelling, 2}},
					nonCancellingDependencies: []dependencyIndex{{continuing, 1}},
				},
			},
			{
				"aborted",
				Dependencies{aborted, continuing},
				precheckedDependencies{
					abort:    true,
					abortErr: err,
					cancel:   false,
					errs:     []error{err, nil},
					cancellingDependencies:    []dependencyIndex{},
					nonCancellingDependencies: []dependencyIndex{{continuing, 1}},
				},
			},
		} {
			t.Run(testIO.name, func(t *testing.T) {
				got := precheckDependencies(testIO.dependencies)
				if !reflect.DeepEqual(got, testIO.expected) {
					t.Errorf("expected\n %#v\n, got\n %#v", testIO.expected, got)
				}
			})
		}
	})
	t.Run("fetchSingleCancel", func(t *testing.T) {
		for _, testIO := range []struct {
			name              string
			cancellingDep     dependencyIndex
			nonCancellingDeps []dependencyIndex
			expectedErrs      []error
			expectedAbort     bool
			expectedAbortErr  error
		}{
			{
				"fetch all",
				dependencyIndex{failure, 0},
				[]dependencyIndex{{success, 1}},
				[]error{err, nil},
				false,
				nil,
			},
			{
				"cancel nothing",
				dependencyIndex{canceled, 0},
				[]dependencyIndex{},
				[]error{err},
				false,
				nil,
			},
			{
				"abort nothing",
				dependencyIndex{aborted, 0},
				[]dependencyIndex{},
				[]error{err},
				true,
				err,
			},
			{
				"cancel",
				dependencyIndex{canceled, 0},
				[]dependencyIndex{{continuing, 1}},
				[]error{err, Discarded},
				false,
				nil,
			},
			{
				"abort",
				dependencyIndex{aborted, 0},
				[]dependencyIndex{{continuing, 1}},
				[]error{err, Discarded},
				true,
				err,
			},
		} {
			errs := make([]error, len(testIO.expectedErrs))
			abort, abortErr := fetchSingleCancel(testIO.cancellingDep, testIO.nonCancellingDeps, func(dep dependencyIndex) error { return dep.node.Fetch() }, errs)
			if abort != testIO.expectedAbort {
				t.Errorf("expected abort = %t, got %t", testIO.expectedAbort, abort)
			}
			if abortErr != testIO.expectedAbortErr {
				t.Errorf("expected abort error %v, got %v", testIO.expectedAbortErr, abortErr)
			}
			if !reflect.DeepEqual(testIO.expectedErrs, errs) {
				t.Errorf("expected errs %v, got %v", testIO.expectedErrs, errs)
			}
		}
	})
	t.Run("fetchMultiCancel", func(t *testing.T) {
		for _, testIO := range []struct {
			name              string
			cancellingDeps    []dependencyIndex
			nonCancellingDeps []dependencyIndex
			expectedErrs      []error
			expectedAbort     bool
			expectedAbortErr  error
		}{
			{
				"uncancelled",
				[]dependencyIndex{{success, 0}, {failure, 1}},
				[]dependencyIndex{{failure, 2}, {success, 3}},
				[]error{nil, err, err, nil},
				false,
				nil,
			},
			{
				"cancelled",
				[]dependencyIndex{{continuing, 0}, {canceled, 1}},
				[]dependencyIndex{{continuing, 2}},
				[]error{Discarded, err, Discarded},
				false,
				nil,
			},
			{
				"aborted",
				[]dependencyIndex{{cancelling, 0}, {aborted, 1}},
				[]dependencyIndex{{continuing, 2}},
				[]error{Discarded, err, Discarded},
				true,
				err,
			},
		} {
			errs := make([]error, len(testIO.expectedErrs))
			abort, abortErr := fetchMultiCancel(testIO.cancellingDeps, testIO.nonCancellingDeps, func(dep dependencyIndex) error { return dep.node.Fetch() }, errs)
			if abort != testIO.expectedAbort {
				t.Errorf("expected abort = %t, got %t", testIO.expectedAbort, abort)
			}
			if abortErr != testIO.expectedAbortErr {
				t.Errorf("expected abort error %v, got %v", testIO.expectedAbortErr, abortErr)
			}
			if !reflect.DeepEqual(testIO.expectedErrs, errs) {
				t.Errorf("expected errs %v, got %v", testIO.expectedErrs, errs)
			}
		}
	})
}
