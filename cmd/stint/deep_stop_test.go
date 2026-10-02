package main

import (
	"path/filepath"
	"testing"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func TestDeepStopCommandLandsJournaledSession(t *testing.T) {
	env := newTestEnv(t, nil, 2)
	env.state.Verify = ""
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	env.coord.stateDir = filepath.Join(stateHome, "stint")
	if err := deep.BeginNewRun(env.coord.stateDir, env.state, env.clock.now); err != nil {
		t.Fatal(err)
	}
	if err := runDeepStop(nil); err != nil {
		t.Fatalf("stop command: %v", err)
	}
	state, err := deep.LoadLatestState(env.coord.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != deep.PhaseLanded || state.MissionOutcome != deep.MissionOutcomeIncomplete {
		t.Fatalf("stop outcome = %s/%s, want landed/incomplete", state.Phase, state.MissionOutcome)
	}
	events := readRunEventFixture(t, env.coord.stateDir, state.SessionID)
	if len(events) != 3 || events[1].Type != deep.RunEventLandingStarted || events[2].Type != deep.RunEventLanded {
		t.Fatalf("stop did not persist the canonical landing: %+v", events)
	}
}
