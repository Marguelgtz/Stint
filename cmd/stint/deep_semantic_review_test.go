package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestMalformedSemanticReviewGetsOneFreshFormatRetry(t *testing.T) {
	env := newSemanticReviewEnv(t, "", "", deep.RepositoryChangeOptional, "test -e work-1.txt")
	env.fake.script = map[int]execResult{
		2: {exitCode: 0, completed: true, finishReason: "completed", outputText: `{"outcome":"clear","findings":[]}`},
		3: clearReviewResult(),
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run with a malformed first semantic-review frame: %v", err)
	}
	task := env.state.Tasks[0]
	if env.fake.calls != 3 || task.ReviewOutcome != deep.ReviewOutcomeClear ||
		task.AcceptanceOutcome != deep.AcceptanceAccepted || env.state.MissionOutcome != deep.MissionOutcomeSucceeded {
		t.Fatalf("bounded semantic-review retry did not recover: calls=%d task=%+v mission=%s", env.fake.calls, task, env.state.MissionOutcome)
	}
	if !strings.Contains(env.fake.inputs[2].prompt, "FORMAT RETRY") || !env.fake.inputs[2].semanticReviewer ||
		env.fake.inputs[2].allowedCommands != nil || env.fake.inputs[2].executorRunID != "" {
		t.Fatalf("format retry did not use a fresh no-tools reviewer context: %+v", env.fake.inputs[2])
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var outcomes []deep.ReviewOutcome
	for _, event := range events {
		if event.Type == deep.RunEventReviewResult && event.ReviewCycle != nil {
			outcomes = append(outcomes, event.ReviewCycle.Outcome)
		}
	}
	if len(outcomes) != 2 || outcomes[0] != deep.ReviewOutcomeUnresolved || outcomes[1] != deep.ReviewOutcomeClear {
		t.Fatalf("protocol retry history = %v, want unresolved then clear", outcomes)
	}
}

func TestResumeRetriesProtocolFailureWithoutRepeatingAcceptedWork(t *testing.T) {
	env := newSemanticReviewEnv(t, "", "", deep.RepositoryChangeOptional, "test -e work-1.txt")
	env.fake.script = map[int]execResult{
		2: {exitCode: 0, completed: true, finishReason: "completed", outputText: "decorated response"},
		3: {exitCode: 0, completed: true, finishReason: "completed", outputText: "decorated response"},
		4: clearReviewResult(),
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	task := env.state.Tasks[0]
	if env.fake.calls != 3 || task.ReviewReason != semanticReviewProtocolRetryExhaustedReason || env.state.Phase != deep.PhaseLanded {
		t.Fatalf("first epoch did not stop after one format retry: calls=%d task=%+v phase=%s", env.fake.calls, task, env.state.Phase)
	}
	checkpoint := task.AcceptanceCheckpointEventID
	env.clock.now = env.clock.now.Add(time.Second)
	if err := deep.BeginResumeEpoch(env.coord.stateDir, env.state, deep.PhaseLanded, env.clock.now); err != nil {
		t.Fatal(err)
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	task = env.state.Tasks[0]
	if env.fake.calls != 4 || !env.fake.inputs[3].semanticReviewer || task.Attempts != 1 || task.AcceptanceCheckpointEventID != checkpoint ||
		task.ReviewOutcome != deep.ReviewOutcomeClear || env.state.MissionOutcome != deep.MissionOutcomeSucceeded {
		t.Fatalf("resume failed to preserve accepted work: calls=%d task=%+v mission=%s", env.fake.calls, task, env.state.MissionOutcome)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	starts, reviews := 0, 0
	for _, event := range events {
		if event.Type == deep.RunEventExecutorStarted {
			starts++
		}
		if event.Type == deep.RunEventReviewResult {
			reviews++
		}
	}
	if starts != 1 || reviews != 3 {
		t.Fatalf("resume lost history or repeated execution: executor starts=%d reviews=%d", starts, reviews)
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
			if tc.want == deep.ReviewOutcomeFindings && (len(task.ReviewFindings) != 1 ||
				task.ReviewFindings[0].Disposition != deep.ReviewFindingRepairCreated || task.ReviewFindings[0].RepairTaskID == "") {
				t.Fatalf("structured review finding was not routed to a durable repair Work Unit: %+v", task.ReviewFindings)
			}
		})
	}
}

func TestSemanticFindingRoutesRepairWorkUnitAndResolvesOnlyAfterBoundRereview(t *testing.T) {
	for _, tc := range []struct {
		name          string
		secondFinding string
		reviewCount   int
		repairCount   int
	}{
		{name: "one repair", reviewCount: 2, repairCount: 1},
		{name: "nested follow-up", secondFinding: `{"outcome":"findings","findings":[{"id":"F-2","severity":"medium","summary":"The repair missed a related behavior.","evidence":"The repair checkpoint still omits the edge case."}]}`, reviewCount: 3, repairCount: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newSemanticReviewEnv(t, "", "", deep.RepositoryChangeOptional, "test -e work-1.txt")
			findings := framedReviewOutput(`{"outcome":"findings","findings":[{"id":"F-1","severity":"high","summary":"The requested API is not exposed.","evidence":"The original checkpoint only adds an internal helper."}]}`)
			env.fake.script = map[int]execResult{
				2: {exitCode: 0, completed: true, finishReason: "completed", outputText: findings},
				4: {exitCode: 0, completed: true, finishReason: "completed", outputText: framedReviewOutput(`{"outcome":"clear","findings":[]}`)},
			}
			if tc.secondFinding != "" {
				env.fake.script[4] = execResult{exitCode: 0, completed: true, finishReason: "completed", outputText: framedReviewOutput(tc.secondFinding)}
				env.fake.script[6] = execResult{exitCode: 0, completed: true, finishReason: "completed", outputText: framedReviewOutput(`{"outcome":"clear","findings":[]}`)}
			}
			if err := env.coord.run(context.Background()); err != nil {
				t.Fatalf("run finding repair graph: %v", err)
			}
			// The parent, each generated repair and each semantic review gets
			// its own Hermes invocation.
			wantCalls := 1 + tc.repairCount + tc.reviewCount
			if env.state.MissionOutcome != deep.MissionOutcomeSucceeded || env.fake.calls != wantCalls {
				t.Fatalf("mission/repair graph = %s calls=%d, want success and %d calls", env.state.MissionOutcome, env.fake.calls, wantCalls)
			}
			if len(env.state.Tasks) != 1+tc.repairCount {
				t.Fatalf("dynamic Work Unit count = %d, want %d: %+v", len(env.state.Tasks), 1+tc.repairCount, env.state.Tasks)
			}
			rootFinding := env.state.Tasks[0].ReviewFindings[0]
			if rootFinding.Disposition != deep.ReviewFindingResolved || rootFinding.RepairTaskID != env.state.Tasks[1].ID {
				t.Fatalf("root finding was not resolved through its repair Work Unit: %+v", rootFinding)
			}
			if tc.repairCount == 2 && env.state.Tasks[1].ReviewFindings[0].Disposition != deep.ReviewFindingResolved {
				t.Fatalf("nested repair finding was not resolved: %+v", env.state.Tasks[1].ReviewFindings)
			}
			for _, task := range env.state.Tasks[1:] {
				if task.Status != deep.StatusAccepted || !deep.TaskHasSatisfiedReviewGate(task.ID, env.state.Tasks) {
					t.Fatalf("repair Work Unit lacks its own accepted, reviewed checkpoint: %+v", task)
				}
			}
			reviewerIndex := 3
			if !env.fake.inputs[reviewerIndex].semanticReviewer || !strings.Contains(env.fake.inputs[reviewerIndex].prompt, "repairContext") ||
				!strings.Contains(env.fake.inputs[reviewerIndex].prompt, "The original checkpoint only adds an internal helper") {
				t.Fatalf("repair re-review did not receive its source finding in a fresh reviewer context: %+v", env.fake.inputs[reviewerIndex])
			}
			events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			created, resolved := 0, 0
			for i, event := range events {
				if event.Type == deep.RunEventReviewRepairCreated {
					created++
				}
				if event.Type == deep.RunEventReviewFindingResolved {
					resolved++
				}
				if event.Sequence != uint64(i+1) {
					t.Fatalf("repair graph event sequence is not run-wide monotonic at index %d: %+v", i, event)
				}
			}
			if created != tc.repairCount || resolved != tc.repairCount {
				t.Fatalf("journal repair transitions create=%d resolved=%d, want %d each", created, resolved, tc.repairCount)
			}
		})
	}
}

func TestSemanticDependentRemainsQueuedUntilRepairChainResolves(t *testing.T) {
	mission := deep.Mission{
		Name: "review repair dependency fixture", Objective: "complete both ordered Objectives",
		AcceptanceContractVersion:     deep.DeterministicAcceptanceContractVersion,
		SemanticReviewContractVersion: deep.SemanticReviewContractVersion,
		Tasks: []deep.Task{
			{ID: "OBJ-1", Objective: "expose the requested API", RepositoryChange: deep.RepositoryChangeOptional, AcceptanceCheck: "test -e work-1.txt", Status: deep.StatusQueued},
			{ID: "OBJ-2", Objective: "continue after the API is ready", DependsOn: []string{"OBJ-1"}, RepositoryChange: deep.RepositoryChangeOptional, AcceptanceCheck: "test -e work-1.txt", Status: deep.StatusQueued},
		},
	}
	env := newV2AcceptanceEnvForMission(t, mission)
	env.coord.verify = func(_ context.Context, command, workdir string) verificationResult {
		outcome, code, output := verificationPassed, 0, "fixture acceptance check passed"
		if _, err := os.Stat(filepath.Join(workdir, "work-1.txt")); err != nil {
			outcome, code, output = verificationFailed, 1, err.Error()
		}
		return verificationResult{Command: command, Outcome: outcome, HasExitCode: true, ExitCode: code, Output: output}
	}
	env.fake.script = map[int]execResult{
		2: {exitCode: 0, completed: true, finishReason: "completed", outputText: framedReviewOutput(`{"outcome":"findings","findings":[{"id":"F-1","severity":"high","summary":"The public API is missing.","evidence":"The checkpoint exposes only an internal helper."}]}`)},
		4: {exitCode: 0, completed: true, finishReason: "completed", outputText: framedReviewOutput(`{"outcome":"clear","findings":[]}`)},
		6: {exitCode: 0, completed: true, finishReason: "completed", outputText: framedReviewOutput(`{"outcome":"clear","findings":[]}`)},
	}
	if err := env.coord.run(context.Background()); err != nil {
		t.Fatalf("run dependent task after semantic repair: %v", err)
	}
	if env.fake.calls != 6 || len(env.state.Tasks) != 3 || env.state.Tasks[1].Status != deep.StatusAccepted ||
		env.state.Tasks[1].Attempts != 1 || env.state.MissionOutcome != deep.MissionOutcomeSucceeded {
		t.Fatalf("dependent task did not wait and then run: calls=%d tasks=%+v outcome=%s", env.fake.calls, env.state.Tasks, env.state.MissionOutcome)
	}
	events, err := deep.ReadRunEvents(env.coord.stateDir, env.state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	resolutionSequence, dependentStartSequence := uint64(0), uint64(0)
	for _, event := range events {
		if event.Type == deep.RunEventReviewFindingResolved {
			resolutionSequence = event.Sequence
		}
		if event.Type == deep.RunEventExecutorStarted && event.ExecutorRun != nil && event.ExecutorRun.TaskID == "OBJ-2" {
			dependentStartSequence = event.Sequence
		}
	}
	if resolutionSequence == 0 || dependentStartSequence <= resolutionSequence {
		t.Fatalf("dependent executor started before its prerequisite repair resolved: resolution=%d dependent=%d", resolutionSequence, dependentStartSequence)
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
