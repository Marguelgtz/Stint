package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Marguelgtz/Stint/internal/deep"
)

func framedReviewOutput(payload string) string {
	return "Hermes session header\n" + semanticReviewBeginMarker + "\n" + payload + "\n" + semanticReviewEndMarker + "\n"
}

func TestSemanticReviewParserRejectsAmbiguousJSONAndUnknownFields(t *testing.T) {
	for _, payload := range []string{
		`{"outcome":"findings","outcome":"clear","findings":[]}`,
		`{"outcome":"clear","findings":[],"futurePolicyOverride":true}`,
	} {
		if _, _, _, err := parseSemanticReviewResponse(framedReviewOutput(payload)); err == nil {
			t.Errorf("review result %s was accepted despite ambiguous or unknown fields", payload)
		}
	}
}

func TestSemanticReviewGateCompletesOnlyOnClearCheckpointBoundResult(t *testing.T) {
	env := newSemanticReviewEnv(t, "", "", deep.RepositoryChangeRequired, "test -e work-1.txt")
	env.fake.script = map[int]execResult{
		2: {exitCode: 0, completed: true, finishReason: "completed", outputText: framedReviewOutput(`{"outcome":"clear","findings":[]}`)},
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run deterministic plus semantic acceptance: %v", err)
	}
	task := env.state.Tasks[0]
	if task.Status != deep.StatusAccepted || task.AcceptanceOutcome != deep.AcceptanceAccepted ||
		task.ReviewOutcome != deep.ReviewOutcomeClear || !task.HasClearReviewForCurrentAcceptance() ||
		env.state.MissionOutcome != deep.MissionOutcomeSucceeded || env.fake.calls != 2 {
		t.Fatalf("semantic acceptance result = task %+v mission=%s calls=%d", task, env.state.MissionOutcome, env.fake.calls)
	}
	if !env.fake.inputs[1].semanticReviewer || env.fake.inputs[1].executorRunID != "" || env.fake.inputs[1].actionPlan != "" ||
		env.fake.inputs[1].allowedCommands != nil || !strings.Contains(env.fake.inputs[1].prompt, "checkpointTreeSha") || !strings.Contains(env.fake.inputs[1].prompt, "gitDiff") {
		t.Fatalf("reviewer did not receive a bounded checkpoint packet in a separate no-tools invocation: %+v", env.fake.inputs[1])
	}
	if _, found, err := deep.LoadReviewCycle(env.coord.stateDir, env.state.SessionID, task.ReviewCycleID); err != nil || !found {
		t.Fatalf("semantic review cycle is not durable: found=%t err=%v", found, err)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	executorStarts, reviewResults := 0, 0
	for _, event := range events {
		if event.Type == deep.RunEventExecutorStarted {
			executorStarts++
		}
		if event.Type == deep.RunEventReviewResult {
			reviewResults++
		}
	}
	if executorStarts != 1 || reviewResults != 1 {
		t.Fatalf("review was not separately journaled: executor starts=%d review results=%d", executorStarts, reviewResults)
	}
}

func TestSemanticReviewFindingsAndMalformedOutputCannotCompleteMission(t *testing.T) {
	for _, tc := range []struct {
		name, result string
		want         deep.ReviewOutcome
	}{
		{
			name:   "findings",
			result: framedReviewOutput(`{"outcome":"findings","findings":[{"id":"F-1","severity":"high","summary":"The requested API is not exposed.","evidence":"The checkpoint diff only adds an internal helper."}]}`),
			want:   deep.ReviewOutcomeFindings,
		},
		{name: "malformed", result: framedReviewOutput(`{"outcome":"clear","findings":[{"id":]}`), want: deep.ReviewOutcomeUnresolved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newSemanticReviewEnv(t, "", "", deep.RepositoryChangeOptional, "test -e work-1.txt")
			env.fake.script = map[int]execResult{
				2: {exitCode: 0, completed: true, finishReason: "completed", outputText: tc.result},
			}
			if err := env.coord.run(context.Background()); err != nil {
				t.Fatalf("run semantic review with %s: %v", tc.name, err)
			}
			task := env.state.Tasks[0]
			if task.ReviewOutcome != tc.want || env.state.MissionOutcome == deep.MissionOutcomeSucceeded || task.AcceptanceOutcome != deep.AcceptanceAccepted {
				t.Fatalf("%s review promoted mission or changed deterministic acceptance: task=%+v mission=%s", tc.name, task, env.state.MissionOutcome)
			}
			if tc.want == deep.ReviewOutcomeFindings && (len(task.ReviewFindings) != 1 || task.ReviewFindings[0].Disposition != deep.ReviewFindingOpen) {
				t.Fatalf("structured review finding was not retained open: %+v", task.ReviewFindings)
			}
		})
	}
}

func TestSemanticReviewSubjectMutationLeavesAcceptanceUnresolved(t *testing.T) {
	env := newSemanticReviewEnv(t, "", "", deep.RepositoryChangeRequired, "test -e work-1.txt")
	env.fake.script = map[int]execResult{
		2: {exitCode: 0, completed: true, finishReason: "completed", outputText: framedReviewOutput(`{"outcome":"clear","findings":[]}`)},
	}
	env.fake.before = func(in execInput) {
		if in.semanticReviewer {
			if err := os.WriteFile(filepath.Join(env.wt, "mutation-during-review.txt"), []byte("changed"), 0o644); err != nil {
				t.Fatalf("mutate repository during reviewer execution: %v", err)
			}
		}
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("land with changed review subject: %v", err)
	}
	if task := env.state.Tasks[0]; task.ReviewOutcome != deep.ReviewOutcomeUnresolved || task.AcceptanceOutcome != deep.AcceptanceAccepted || env.state.MissionOutcome == deep.MissionOutcomeSucceeded {
		t.Fatalf("review of a mutated subject produced false mission success: task=%+v outcome=%s", task, env.state.MissionOutcome)
	}
}

func TestSemanticDependencyWaitsForClearReviewOfAcceptedPrerequisite(t *testing.T) {
	env := newSemanticReviewEnv(t, "", "", deep.RepositoryChangeOptional, "test -e work-1.txt")
	env.state.Tasks = append(env.state.Tasks, deep.Task{
		ID: "OBJ-2", Objective: "continue after OBJ-1", Status: deep.StatusQueued,
		DependsOn: []string{"OBJ-1"}, RepositoryChange: deep.RepositoryChangeOptional, AcceptanceCheck: "true",
	})
	blocker := env.coord.failedPrerequisite(env.state.Tasks[1])
	if !strings.Contains(blocker, "requires it to be accepted and clear-reviewed") {
		t.Fatalf("dependency was satisfied by deterministic acceptance without review: %q", blocker)
	}
}

func TestSemanticReviewCanRunAfterExecutorAttemptCap(t *testing.T) {
	env := newSemanticReviewEnv(t, "", "", deep.RepositoryChangeOptional, "test -e work-1.txt")
	env.state.TaskAttemptCap = 1
	env.fake.script = map[int]execResult{
		2: {exitCode: 0, completed: true, finishReason: "completed", outputText: framedReviewOutput(`{"outcome":"clear","findings":[]}`)},
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run semantic review at task attempt cap: %v", err)
	}
	if env.fake.calls != 2 || env.state.Tasks[0].ReviewOutcome != deep.ReviewOutcomeClear || env.state.MissionOutcome != deep.MissionOutcomeSucceeded {
		t.Fatalf("review was skipped at attempt cap: calls=%d task=%+v outcome=%s", env.fake.calls, env.state.Tasks[0], env.state.MissionOutcome)
	}
}
