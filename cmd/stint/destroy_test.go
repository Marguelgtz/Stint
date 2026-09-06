package main

import (
	"context"
	"errors"
	"testing"

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
	old := destroyRetryDelays
	destroyRetryDelays = []time.Duration{0}
	defer func() { destroyRetryDelays = old }()
	client := &fakeDestroyClient{showErrs: []error{errors.New("instance not found")}}
	result := destroyAndConfirm(context.Background(), client, 42)
	if !result.Confirmed {
		t.Fatalf("expected confirmed, got %+v", result)
	}
}

func TestDestroyAndConfirmPreservesUnconfirmedFailure(t *testing.T) {
	old := destroyRetryDelays
	destroyRetryDelays = []time.Duration{0, 0}
	defer func() { destroyRetryDelays = old }()
	client := &fakeDestroyClient{
		destroyErrs: []error{errors.New("dns timeout"), errors.New("dns timeout")},
		showErrs: []error{errors.New("dns timeout"), errors.New("dns timeout")},
	}
	result := destroyAndConfirm(context.Background(), client, 42)
	if result.Confirmed {
		t.Fatalf("unexpected confirmation: %+v", result)
	}
	if result.Attempts != 2 || result.LastError == nil {
		t.Fatalf("unexpected result: %+v", result)
	}
}
