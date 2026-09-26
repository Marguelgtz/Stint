package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func TestJournaledRunStartAndLandingProjectCanonicalLifecycle(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Verify = ""
	env.coord.stateDir = t.TempDir() // emulate a new run with no legacy projection at this identity
	if err := deep.BeginNewRun(env.coord.stateDir, env.state, env.clock.now); err != nil {
		t.Fatalf("begin journaled run: %v", err)
	}
	if env.state.Phase != deep.PhaseExecuting || env.state.RunEventWatermark != 1 || env.state.RunID != env.state.SessionID {
		t.Fatalf("initial journal projection = run=%q phase=%q watermark=%d", env.state.RunID, env.state.Phase, env.state.RunEventWatermark)
	}
	if err := env.coord.land(context.Background(), "no safe work remains"); err != nil {
		t.Fatalf("land journaled run: %v", err)
	}
	if env.state.Phase != deep.PhaseLanded || env.state.RunEventWatermark != 3 || env.state.LandingCommit == "" {
		t.Fatalf("terminal journal projection = phase=%q watermark=%d checkpoint=%q", env.state.Phase, env.state.RunEventWatermark, env.state.LandingCommit)
	}
	events := readRunEventFixture(t, env.coord.stateDir, env.state.SessionID)
	if len(events) != 3 {
		t.Fatalf("RunEvent count = %d, want start/landing-start/landed", len(events))
	}
	wantTypes := []deep.RunEventType{deep.RunEventEpochStarted, deep.RunEventLandingStarted, deep.RunEventLanded}
	for i, event := range events {
		if event.Sequence != uint64(i+1) || event.RunID != env.state.RunID || event.EpochID != env.state.ExecutionEpochID || event.Type != wantTypes[i] {
			t.Fatalf("event %d = %+v, want type %q and sequence %d", i, event, wantTypes[i], i+1)
		}
	}
	if events[2].CheckpointCommit != env.state.LandingCommit || events[2].CheckpointTree != env.state.LandingCheckpointTreeSHA {
		t.Fatalf("landed event does not identify projected checkpoint: event=%+v state=%+v", events[2], env.state)
	}
	loaded, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil || loaded.RunEventWatermark != 3 || loaded.LandingCommit != env.state.LandingCommit {
		t.Fatalf("load landed journal projection = watermark %d checkpoint %q err %v", loaded.RunEventWatermark, loaded.LandingCommit, err)
	}
}

func TestJournaledLandingReusesCheckpointWhenCompletionEventIsMissing(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Verify = ""
	env.coord.stateDir = t.TempDir() // emulate a new run with no legacy projection at this identity
	if err := os.WriteFile(filepath.Join(env.wt, "journal-checkpoint.txt"), []byte("semantic change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := deep.BeginNewRun(env.coord.stateDir, env.state, env.clock.now); err != nil {
		t.Fatal(err)
	}
	env.coord.completeLanding = func(string, *deep.DeepState, string, string, time.Time) error {
		return errors.New("injected lost completion event")
	}
	if err := env.coord.land(context.Background(), "checkpoint recovery fixture"); err == nil {
		t.Fatal("landing succeeded after completion-event failure")
	}
	firstHead, err := env.coord.git.headCommit(env.wt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(firstHead) == env.state.BaseCommit {
		t.Fatal("fixture did not create the checkpoint side effect before losing its event")
	}
	if events := readRunEventFixture(t, env.coord.stateDir, env.state.SessionID); len(events) != 2 {
		t.Fatalf("journal after lost completion event has %d events, want start and landing-start only", len(events))
	}
	env.coord.completeLanding = nil
	if err := env.coord.land(context.Background(), "retry landing"); err != nil {
		t.Fatalf("resume after missing completion event: %v", err)
	}
	secondHead, err := env.coord.git.headCommit(env.wt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(firstHead) != strings.TrimSpace(secondHead) {
		t.Fatalf("landing recovery duplicated checkpoint commit: %s -> %s", firstHead, secondHead)
	}
	if env.state.Phase != deep.PhaseLanded || env.state.LandingCommit != strings.TrimSpace(firstHead) || env.state.RunEventWatermark != 3 {
		t.Fatalf("reconciled landing state = phase %q checkpoint %q watermark %d", env.state.Phase, env.state.LandingCommit, env.state.RunEventWatermark)
	}
}

func TestLandingWithStaleCoordinatorSnapshotDefersForActiveTaskQuiescence(t *testing.T) {
	env := newTestEnv(t, nil, 3)
	env.state.Verify = "must-not-run-after-active-task"
	env.coord.stateDir = t.TempDir() // isolate this as a journaled run
	if err := deep.BeginNewRun(env.coord.stateDir, env.state, env.clock.now); err != nil {
		t.Fatal(err)
	}
	latest, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	latest.Tasks[0].Status = deep.StatusActive
	latest.Tasks[0].Attempts = 1
	if err := latest.SaveDir(env.coord.stateDir); err != nil {
		t.Fatal(err)
	}

	finalVerifierCalls := 0
	env.coord.finalVerify = func(context.Context, string) verificationResult {
		finalVerifierCalls++
		return verificationResult{Outcome: verificationPassed}
	}
	if err := env.coord.land(context.Background(), "stale landing caller"); err == nil || !strings.Contains(err.Error(), "landing deferred until active task") {
		t.Fatalf("landing with durable active task = %v, want quiescence deferral", err)
	}
	if finalVerifierCalls != 0 {
		t.Fatalf("final verifier ran %d times while task quiescence was unresolved", finalVerifierCalls)
	}
	loaded, err := deep.LoadState(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Phase != deep.PhaseLanding || !loaded.ExecutionQuiescenceUnconfirmed || loaded.ExecutionQuiescenceTaskID != "T-001" {
		t.Fatalf("landing did not durably block on active-task quiescence: phase=%q blocked=%t task=%q", loaded.Phase, loaded.ExecutionQuiescenceUnconfirmed, loaded.ExecutionQuiescenceTaskID)
	}
}

func readRunEventFixture(t *testing.T, stateDir, sessionID string) []deep.RunEvent {
	t.Helper()
	path := filepath.Join(deep.DeepDir(stateDir, sessionID), "run-events.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var events []deep.RunEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event deep.RunEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}
