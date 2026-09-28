package deep

import (
	"testing"
	"time"
)

func TestSaveDirRejectsDirectJournalBackedReviewMutation(t *testing.T) {
	stateDir, state, cycle, now := reviewCycleFixture(t)
	started, err := BeginReviewCycle(stateDir, &state, cycle)
	if err != nil {
		t.Fatal(err)
	}
	started.EndedAt = now.Add(55 * time.Second)
	started.DurationMilliseconds = started.EndedAt.Sub(started.StartedAt).Milliseconds()
	started.Outcome = ReviewOutcomeClear
	if err := CompleteReviewCycle(stateDir, &state, started); err != nil {
		t.Fatal(err)
	}

	forged := state
	forged.Tasks = append([]Task(nil), state.Tasks...)
	forged.Tasks[0].ReviewOutcome = ReviewOutcomeFindings
	if err := forged.SaveDir(stateDir); err == nil {
		t.Fatal("direct deep.json mutation changed journal-backed review outcome")
	}
}
