package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

type fakeDeadlineWatch struct {
	now         time.Time
	state       sessionstate.State
	missing     bool
	destroyedAt []time.Time
	destroyErrs []error
	failures    int
	waits       int
	onWait      func(*fakeDeadlineWatch)
	onLock      func(*fakeDeadlineWatch)
}

func (f *fakeDeadlineWatch) deps() deadlineWatchdogDeps {
	return deadlineWatchdogDeps{
		load: func() (sessionstate.State, error) {
			if f.missing {
				return sessionstate.State{}, os.ErrNotExist
			}
			return f.state, nil
		},
		now: func() time.Time { return f.now },
		wait: func(_ context.Context, d time.Duration) error {
			f.now = f.now.Add(d)
			f.waits++
			if f.onWait != nil {
				f.onWait(f)
			}
			return nil
		},
		lock: func() (func(), error) {
			if f.onLock != nil {
				f.onLock(f)
			}
			return func() {}, nil
		},
		destroy: func(_ sessionstate.State) error {
			f.destroyedAt = append(f.destroyedAt, f.now)
			if len(f.destroyErrs) == 0 {
				f.missing = true
				return nil
			}
			err := f.destroyErrs[0]
			f.destroyErrs = f.destroyErrs[1:]
			if err == nil {
				f.missing = true
			}
			return err
		},
		recordFail: func(_ sessionstate.State, _ error) { f.failures++ },
	}
}

func newFakeDeadlineWatch(duration time.Duration) *fakeDeadlineWatch {
	now := time.Date(2026, 8, 31, 7, 0, 0, 0, time.UTC)
	return &fakeDeadlineWatch{
		now: now,
		state: sessionstate.State{
			InstanceID: 42,
			StartedAt:  now,
			Deadline:   now.Add(duration),
		},
	}
}

func TestWatchSessionDeadlineDestroysAtUnchangedDeadline(t *testing.T) {
	fake := newFakeDeadlineWatch(2 * time.Second)
	if err := watchSessionDeadline(context.Background(), 42, time.Second, time.Second, fake.deps()); err != nil {
		t.Fatal(err)
	}
	if len(fake.destroyedAt) != 1 {
		t.Fatalf("destroy calls = %d, want 1", len(fake.destroyedAt))
	}
	if !fake.destroyedAt[0].Equal(fake.state.Deadline) {
		t.Fatalf("destroyed at %s, want %s", fake.destroyedAt[0], fake.state.Deadline)
	}
}

func TestWatchSessionDeadlineObservesExtension(t *testing.T) {
	fake := newFakeDeadlineWatch(2 * time.Second)
	original := fake.state.Deadline
	extended := original.Add(8 * time.Second)
	fake.onWait = func(f *fakeDeadlineWatch) {
		if f.waits == 1 {
			f.state.Deadline = extended
		}
	}
	if err := watchSessionDeadline(context.Background(), 42, time.Second, time.Second, fake.deps()); err != nil {
		t.Fatal(err)
	}
	if len(fake.destroyedAt) != 1 || !fake.destroyedAt[0].Equal(extended) {
		t.Fatalf("destroyed at %v, want only %s", fake.destroyedAt, extended)
	}
}

func TestWatchSessionDeadlineObservesShortening(t *testing.T) {
	fake := newFakeDeadlineWatch(10 * time.Second)
	shortened := fake.now.Add(3 * time.Second)
	fake.onWait = func(f *fakeDeadlineWatch) {
		if f.waits == 1 {
			f.state.Deadline = shortened
		}
	}
	if err := watchSessionDeadline(context.Background(), 42, time.Second, time.Second, fake.deps()); err != nil {
		t.Fatal(err)
	}
	if len(fake.destroyedAt) != 1 || !fake.destroyedAt[0].Equal(shortened) {
		t.Fatalf("destroyed at %v, want only %s", fake.destroyedAt, shortened)
	}
}

func TestWatchSessionDeadlineRechecksAfterLifecycleLock(t *testing.T) {
	fake := newFakeDeadlineWatch(time.Second)
	oldDeadline := fake.state.Deadline
	newDeadline := oldDeadline.Add(5 * time.Second)
	locked := false
	fake.onLock = func(f *fakeDeadlineWatch) {
		if !locked {
			locked = true
			f.state.Deadline = newDeadline
		}
	}
	if err := watchSessionDeadline(context.Background(), 42, time.Second, time.Second, fake.deps()); err != nil {
		t.Fatal(err)
	}
	if len(fake.destroyedAt) != 1 || !fake.destroyedAt[0].Equal(newDeadline) {
		t.Fatalf("destroyed at %v, want only new deadline %s", fake.destroyedAt, newDeadline)
	}
}

func TestWatchSessionDeadlineExitsForReplacementInstance(t *testing.T) {
	fake := newFakeDeadlineWatch(2 * time.Second)
	fake.onWait = func(f *fakeDeadlineWatch) {
		f.state.InstanceID = 99
	}
	if err := watchSessionDeadline(context.Background(), 42, time.Second, time.Second, fake.deps()); err != nil {
		t.Fatal(err)
	}
	if len(fake.destroyedAt) != 0 {
		t.Fatalf("destroy calls = %d, want 0", len(fake.destroyedAt))
	}
}

func TestWatchSessionDeadlineExitsWhenStateDisappears(t *testing.T) {
	fake := newFakeDeadlineWatch(2 * time.Second)
	fake.onWait = func(f *fakeDeadlineWatch) { f.missing = true }
	if err := watchSessionDeadline(context.Background(), 42, time.Second, time.Second, fake.deps()); err != nil {
		t.Fatal(err)
	}
	if len(fake.destroyedAt) != 0 {
		t.Fatalf("destroy calls = %d, want 0", len(fake.destroyedAt))
	}
}

func TestWatchSessionDeadlineRetriesDestroyFailure(t *testing.T) {
	fake := newFakeDeadlineWatch(time.Second)
	fake.destroyErrs = []error{errors.New("provider unavailable"), nil}
	if err := watchSessionDeadline(context.Background(), 42, time.Second, 5*time.Second, fake.deps()); err != nil {
		t.Fatal(err)
	}
	if len(fake.destroyedAt) != 2 {
		t.Fatalf("destroy calls = %d, want 2", len(fake.destroyedAt))
	}
	if fake.failures != 1 {
		t.Fatalf("recorded failures = %d, want 1", fake.failures)
	}
}

func TestWatchSessionDeadlineRejectsInvalidInstance(t *testing.T) {
	fake := newFakeDeadlineWatch(time.Second)
	if err := watchSessionDeadline(context.Background(), 0, time.Second, time.Second, fake.deps()); err == nil {
		t.Fatal("expected invalid instance id to fail")
	}
}
