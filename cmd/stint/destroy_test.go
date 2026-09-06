package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/provider/vast"
)

type fakeDestroyClient struct {
	destroyErrs []error
	showErrs    []error
	showStates  []vast.Instance
	destroys    int
	shows       int
}

func (f *fakeDestroyClient) DestroyInstance(context.Context, int64) error {
	idx := f.destroys
	f.destroys++
	if idx < len(f.destroyErrs) {
		return f.destroyErrs[idx]
	}
	return nil
}

func (f *fakeDestroyClient) ShowInstance(context.Context, int64) (vast.Instance, error) {
	idx := f.shows
	f.shows++
	if idx < len(f.showErrs) && f.showErrs[idx] != nil {
		return vast.Instance{}, f.showErrs[idx]
	}
	if idx < len(f.showStates) {
		return f.showStates[idx], nil
	}
	return vast.Instance{ActualStatus: "running"}, nil
}

func TestDestroyAndConfirmTreatsMissingAsConfirmed(t *testing.T) {
	client := &fakeDestroyClient{showErrs: []error{errors.New("instance not found")}}
	result := destroyAndConfirmWithDelays(context.Background(), client, 42, []time.Duration{0})
	if !result.Confirmed {
		t.Fatalf("expected confirmed, got %+v", result)
	}
}

func TestDestroyAndConfirmPreservesUnconfirmedFailure(t *testing.T) {
	client := &fakeDestroyClient{
		destroyErrs: []error{errors.New("dns timeout"), errors.New("dns timeout")},
		showErrs:    []error{errors.New("dns timeout"), errors.New("dns timeout")},
	}
	result := destroyAndConfirmWithDelays(context.Background(), client, 42, []time.Duration{0, 0})
	if result.Confirmed {
		t.Fatalf("unexpected confirmation: %+v", result)
	}
	if result.Attempts != 2 || result.LastError == nil {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestDestroyAndConfirmRetriesTransientFailureThenConfirmsMissing(t *testing.T) {
	client := &fakeDestroyClient{
		destroyErrs: []error{errors.New("temporary DNS failure"), nil},
		showErrs:    []error{errors.New("temporary DNS failure"), errors.New("instance not found")},
	}
	result := destroyAndConfirmWithDelays(context.Background(), client, 42, []time.Duration{0, 0})
	if !result.Confirmed {
		t.Fatalf("expected confirmation after retry, got %+v", result)
	}
	if result.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", result.Attempts)
	}
}

func TestDestroyAndConfirmAcceptsTerminalProviderStatus(t *testing.T) {
	client := &fakeDestroyClient{showStates: []vast.Instance{{ActualStatus: "destroyed"}}}
	result := destroyAndConfirmWithDelays(context.Background(), client, 42, []time.Duration{0})
	if !result.Confirmed {
		t.Fatalf("expected terminal status to confirm teardown, got %+v", result)
	}
}
