package deep

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQualificationSnapshotIsSelfVerifyingAndBindsRunHistory(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	deepDir := DeepDir(stateDir, state.SessionID)
	if err := os.WriteFile(filepath.Join(deepDir, "mission.md"), []byte("# Qualification\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deepDir, "publication.json"), []byte(`{"checkpoints":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	bundle, manifest, err := ExportQualificationSnapshot(stateDir, state.SessionID, "epoch-1-seq-1", QualificationContext{
		StintSourceSHA: strings.Repeat("a", 40), StintPR183SHA: strings.Repeat("b", 40),
		SparkSourceSHA: strings.Repeat("c", 40), SparkBaseSHA: strings.Repeat("d", 40), ProviderID: 1234,
		StartedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano), HourlyUSD: 0.5,
	})
	if err != nil {
		t.Fatalf("export qualification snapshot: %v", err)
	}
	if manifest.RunID != state.RunID || manifest.FirstEventSequence != 1 || manifest.LastEventSequence != 1 ||
		manifest.ProjectionWatermark != state.RunEventWatermark || manifest.ProviderInstanceID != 1234 ||
		manifest.StintSourceSHA != strings.Repeat("a", 40) || manifest.StintPR183SHA != strings.Repeat("b", 40) ||
		manifest.SparkSourceSHA != strings.Repeat("c", 40) || manifest.RuntimeMilliseconds <= 0 ||
		manifest.RuntimeCostEstimateUSD <= 0 || manifest.HourlyUSD != 0.5 {
		t.Fatalf("manifest omitted run provenance: %+v", manifest)
	}
	verified, digest, err := VerifyQualificationBundle(bundle)
	if err != nil {
		t.Fatalf("verify exported bundle: %v", err)
	}
	if digest == "" || verified.RunID != state.RunID || len(verified.Artifacts) < 3 {
		t.Fatalf("verified manifest = %+v digest=%q", verified, digest)
	}

	journal := filepath.Join(bundle, "run-events.jsonl")
	original, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, []byte(strings.Repeat("x", len(original))), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := VerifyQualificationBundle(bundle); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("tampered journal verification = %v, want SHA-256 rejection", err)
	}
}

func TestQualificationExportRejectsInvalidCostProvenance(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ExportQualificationSnapshot(stateDir, state.SessionID, "bad-hourly-rate", QualificationContext{HourlyUSD: math.Inf(1)}); err == nil {
		t.Fatal("infinite hourly rate was accepted")
	}
}

func TestQualificationExportRejectsMalformedProvenanceIdentity(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ExportQualificationSnapshot(stateDir, state.SessionID, "bad-source-sha", QualificationContext{StintPR183SHA: "not-a-commit"}); err == nil {
		t.Fatal("malformed PR #183 provenance identity was accepted")
	}
}

func TestQualificationExportRejectsSecretLikeArtifactAndDuplicateSnapshot(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	deepDir := DeepDir(stateDir, state.SessionID)
	if err := os.WriteFile(filepath.Join(deepDir, "mission.md"), []byte("ghp_123456789012345678901234567890"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ExportQualificationSnapshot(stateDir, state.SessionID, "snapshot-secret", QualificationContext{}); err == nil || !strings.Contains(err.Error(), "credential-like") {
		t.Fatalf("secret-like artifact export = %v, want rejection", err)
	}
	if err := os.WriteFile(filepath.Join(deepDir, "mission.md"), []byte("safe mission"), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle, _, err := ExportQualificationSnapshot(stateDir, state.SessionID, "snapshot-secret", QualificationContext{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ExportQualificationSnapshot(stateDir, state.SessionID, "snapshot-secret", QualificationContext{}); err == nil {
		t.Fatal("duplicate snapshot ID was accepted")
	}
	if _, err := os.Stat(bundle); err != nil {
		t.Fatalf("first snapshot was not preserved: %v", err)
	}
}

func TestQualificationExportRequiresValidJournalWatermark(t *testing.T) {
	stateDir, state, now := journalFixture(t)
	if err := BeginNewRun(stateDir, &state, now); err != nil {
		t.Fatal(err)
	}
	state.RunEventWatermark++
	if err := state.SaveDir(stateDir); err == nil {
		t.Fatal("invalid projection watermark unexpectedly persisted")
	}
}
