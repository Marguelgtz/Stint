package deep

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	QualificationBundleSchema = "stint-deep-qualification/v1"
	qualificationBundleLimit  = int64(32 << 20)
	qualificationFileLimit    = int64(8 << 20)
)

var qualificationSnapshotID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)
var qualificationSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)gh[pousr]_[A-Za-z0-9]{24,}`),
	regexp.MustCompile(`(?i)github_pat_[A-Za-z0-9_]{30,}`),
	regexp.MustCompile(`(?i)sk-[A-Za-z0-9_-]{24,}`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/-]{24,}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

var qualificationCommitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// QualificationContext contains only explicitly approved run provenance. It
// is intentionally not a generic environment-variable dump.
type QualificationContext struct {
	StintSourceSHA string  `json:"stintSourceSha,omitempty"`
	StintPR183SHA  string  `json:"stintPr183Sha,omitempty"`
	SparkSourceSHA string  `json:"sparkSourceSha,omitempty"`
	SparkBaseSHA   string  `json:"sparkBaseSha,omitempty"`
	ProviderID     int64   `json:"providerInstanceId,omitempty"`
	StartedAt      string  `json:"startedAt,omitempty"`
	HourlyUSD      float64 `json:"hourlyUsd,omitempty"`
	CostUSD        float64 `json:"costUsd,omitempty"`
}

type QualificationArtifact struct {
	Path      string `json:"path"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"mediaType"`
}

type QualificationManifest struct {
	SchemaVersion                string                  `json:"schemaVersion"`
	CreatedAt                    time.Time               `json:"createdAt"`
	RunID                        string                  `json:"runId"`
	EpochIDs                     []string                `json:"epochIds,omitempty"`
	FirstEventSequence           uint64                  `json:"firstEventSequence,omitempty"`
	LastEventSequence            uint64                  `json:"lastEventSequence,omitempty"`
	ProjectionWatermark          uint64                  `json:"projectionWatermark,omitempty"`
	StintBinarySHA256            string                  `json:"stintBinarySha256,omitempty"`
	StintSourceSHA               string                  `json:"stintSourceSha,omitempty"`
	StintPR183SHA                string                  `json:"stintPr183Sha,omitempty"`
	SparkSourceSHA               string                  `json:"sparkSourceSha,omitempty"`
	SparkBaseSHA                 string                  `json:"sparkBaseSha,omitempty"`
	MissionSHA256                string                  `json:"missionSha256,omitempty"`
	AcceptanceContractSHA256     string                  `json:"acceptanceContractSha256,omitempty"`
	SemanticReviewContractSHA256 string                  `json:"semanticReviewContractSha256,omitempty"`
	AcceptanceCommands           map[string]string       `json:"acceptanceCommandIdentities,omitempty"`
	VerifierCommands             map[string]string       `json:"verifierCommandIdentities,omitempty"`
	Provider                     string                  `json:"provider,omitempty"`
	Model                        string                  `json:"model,omitempty"`
	Reasoning                    string                  `json:"reasoning,omitempty"`
	ProviderInstanceID           int64                   `json:"providerInstanceId,omitempty"`
	StartedAt                    string                  `json:"startedAt,omitempty"`
	RuntimeMilliseconds          int64                   `json:"runtimeMilliseconds,omitempty"`
	HourlyUSD                    float64                 `json:"hourlyUsd,omitempty"`
	CostUSD                      float64                 `json:"costUsd,omitempty"`
	RuntimeCostEstimateUSD       float64                 `json:"runtimeCostEstimateUsd,omitempty"`
	Artifacts                    []QualificationArtifact `json:"artifacts"`
	MissionOutcome               MissionOutcome          `json:"missionOutcome,omitempty"`
}

type qualificationEventIndex struct {
	Sequence      uint64                   `json:"sequence"`
	EventID       string                   `json:"eventId"`
	EpochID       string                   `json:"epochId"`
	Type          RunEventType             `json:"type"`
	TaskID        string                   `json:"taskId,omitempty"`
	ExecutorRunID string                   `json:"executorRunId,omitempty"`
	Checkpoint    *TaskCheckpoint          `json:"checkpoint,omitempty"`
	Acceptance    *AcceptanceRun           `json:"acceptance,omitempty"`
	Review        *ReviewCycle             `json:"review,omitempty"`
	Repair        *ReviewRepairWorkUnit    `json:"repair,omitempty"`
	Disposition   *ReviewFindingResolution `json:"findingResolution,omitempty"`
	MissionReview *MissionReviewCycle      `json:"missionReview,omitempty"`
}

// ExportQualificationSnapshot creates an immutable, allow-listed bundle under
// <stateDir>/deep/<runID>/qualification/snapshots/<snapshotID>. LoadState and
// ReadRunEvents validate the journal and projection before any bytes are copied.
func ExportQualificationSnapshot(stateDir, runID, snapshotID string, context QualificationContext) (string, QualificationManifest, error) {
	if !qualificationSnapshotID.MatchString(snapshotID) {
		return "", QualificationManifest{}, errors.New("qualification snapshot ID is invalid")
	}
	for name, value := range map[string]string{
		"Stint source": context.StintSourceSHA, "Stint PR #183": context.StintPR183SHA,
		"Spark source": context.SparkSourceSHA, "Spark base": context.SparkBaseSHA,
	} {
		if value != "" && !qualificationCommitSHA.MatchString(value) {
			return "", QualificationManifest{}, fmt.Errorf("qualification %s commit SHA is invalid", name)
		}
	}
	if math.IsNaN(context.HourlyUSD) || math.IsInf(context.HourlyUSD, 0) || context.HourlyUSD < 0 ||
		math.IsNaN(context.CostUSD) || math.IsInf(context.CostUSD, 0) || context.CostUSD < 0 {
		return "", QualificationManifest{}, errors.New("qualification cost provenance is invalid")
	}
	state, err := LoadState(stateDir, runID)
	if err != nil {
		return "", QualificationManifest{}, fmt.Errorf("validate qualification run state: %w", err)
	}
	events, err := ReadRunEvents(stateDir, runID)
	if err != nil {
		return "", QualificationManifest{}, fmt.Errorf("validate qualification event journal: %w", err)
	}
	if state.RunEventSchemaVersion != 0 && state.RunEventWatermark != uint64(len(events)) {
		return "", QualificationManifest{}, fmt.Errorf("projection watermark %d differs from journal sequence %d", state.RunEventWatermark, len(events))
	}
	if state.RunEventSchemaVersion != RunEventSchemaVersion || len(events) == 0 {
		return "", QualificationManifest{}, errors.New("qualification export requires a non-empty journaled run")
	}

	deepDir := DeepDir(stateDir, runID)
	bundleDir := filepath.Join(deepDir, "qualification", "snapshots", snapshotID)
	if _, err := os.Lstat(bundleDir); err == nil {
		return "", QualificationManifest{}, fmt.Errorf("qualification snapshot %q already exists", snapshotID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", QualificationManifest{}, err
	}
	if err := os.MkdirAll(filepath.Dir(bundleDir), 0o700); err != nil {
		return "", QualificationManifest{}, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(bundleDir), ".qualification-*.tmp")
	if err != nil {
		return "", QualificationManifest{}, err
	}
	defer os.RemoveAll(stage)
	if err := os.Chmod(stage, 0o700); err != nil {
		return "", QualificationManifest{}, err
	}

	files := []string{"deep.json", "mission.md", "publication.json", "incidents.jsonl", "qualification-timings.jsonl", "run-events.jsonl"}
	var total int64
	artifacts := make([]QualificationArtifact, 0, len(files)+8)
	for _, name := range files {
		path := filepath.Join(deepDir, name)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return "", QualificationManifest{}, err
		}
		artifact, err := qualificationCopyArtifact(path, stage, name)
		if err != nil {
			return "", QualificationManifest{}, err
		}
		total += artifact.Bytes
		artifacts = append(artifacts, artifact)
	}

	receiptDir := filepath.Join(deepDir, "executor-receipts")
	receiptEntries, err := os.ReadDir(receiptDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", QualificationManifest{}, err
	}
	for _, entry := range receiptEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		receiptPath := filepath.Join(receiptDir, entry.Name())
		data, err := readQualificationFile(receiptPath)
		if err != nil {
			return "", QualificationManifest{}, err
		}
		var receipt ExecutorReceipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			return "", QualificationManifest{}, fmt.Errorf("decode executor receipt %q: %w", entry.Name(), err)
		}
		if err := ValidateExecutorReceipt(receipt); err != nil {
			return "", QualificationManifest{}, fmt.Errorf("validate executor receipt %q: %w", entry.Name(), err)
		}
		rel := filepath.ToSlash(filepath.Join("executor-receipts", entry.Name()))
		artifact, err := qualificationWriteArtifact(stage, rel, data)
		if err != nil {
			return "", QualificationManifest{}, err
		}
		total += artifact.Bytes
		artifacts = append(artifacts, artifact)
	}
	faultMarker := filepath.Join(deepDir, "qualification", "fault-markers", "EXPORT-001-attempt-1-after-receipt.fired")
	if markerBytes, err := readQualificationFile(faultMarker); err == nil {
		if len(markerBytes) > 1024 || !strings.HasPrefix(string(markerBytes), "STINT_QUALIFICATION_FAULT_V1 ") {
			return "", QualificationManifest{}, errors.New("qualification fault marker is malformed or exceeds its size limit")
		}
		artifact, err := qualificationWriteArtifact(stage, "fault-markers/EXPORT-001-attempt-1-after-receipt.fired", markerBytes)
		if err != nil {
			return "", QualificationManifest{}, err
		}
		total += artifact.Bytes
		artifacts = append(artifacts, artifact)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", QualificationManifest{}, err
	}
	if total > qualificationBundleLimit {
		return "", QualificationManifest{}, fmt.Errorf("qualification snapshot exceeds %d bytes", qualificationBundleLimit)
	}

	index := make([]qualificationEventIndex, 0, len(events))
	epochs := make([]string, 0, 4)
	seenEpoch := map[string]bool{}
	for _, event := range events {
		item := qualificationEventIndex{Sequence: event.Sequence, EventID: event.EventID, EpochID: event.EpochID, Type: event.Type,
			Checkpoint: event.TaskCheckpoint, Acceptance: event.AcceptanceRun, Review: event.ReviewCycle,
			Repair: event.ReviewRepair, Disposition: event.ReviewDisposition, MissionReview: event.MissionReview}
		if event.ExecutorRun != nil {
			item.TaskID, item.ExecutorRunID = event.ExecutorRun.TaskID, event.ExecutorRun.ID
		}
		if event.TaskCheckpoint != nil {
			item.TaskID = event.TaskCheckpoint.TaskID
		}
		if event.AcceptanceRun != nil {
			item.TaskID = event.AcceptanceRun.TaskID
		}
		if event.ReviewCycle != nil {
			item.TaskID = event.ReviewCycle.TaskID
		}
		if event.ReviewRepair != nil {
			item.TaskID = event.ReviewRepair.TaskID
		}
		index = append(index, item)
		if event.EpochID != "" && !seenEpoch[event.EpochID] {
			seenEpoch[event.EpochID] = true
			epochs = append(epochs, event.EpochID)
		}
	}
	indexBytes, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return "", QualificationManifest{}, err
	}
	artifact, err := qualificationWriteArtifact(stage, "qualification-index.json", append(indexBytes, '\n'))
	if err != nil {
		return "", QualificationManifest{}, err
	}
	total += artifact.Bytes
	artifacts = append(artifacts, artifact)
	if total > qualificationBundleLimit {
		return "", QualificationManifest{}, fmt.Errorf("qualification snapshot exceeds %d bytes", qualificationBundleLimit)
	}

	missionSHA := ""
	for _, item := range artifacts {
		if item.Path == "mission.md" {
			missionSHA = item.SHA256
			break
		}
	}
	binarySHA := ""
	if executable, err := os.Executable(); err == nil {
		if data, err := readQualificationFile(executable); err == nil {
			sum := sha256.Sum256(data)
			binarySHA = hex.EncodeToString(sum[:])
		}
	}
	manifest := QualificationManifest{
		SchemaVersion: QualificationBundleSchema, CreatedAt: time.Now().UTC(), RunID: state.RunID,
		EpochIDs: epochs, ProjectionWatermark: state.RunEventWatermark,
		StintBinarySHA256: binarySHA, StintSourceSHA: context.StintSourceSHA, StintPR183SHA: context.StintPR183SHA,
		SparkSourceSHA: context.SparkSourceSHA, SparkBaseSHA: firstNonEmpty(context.SparkBaseSHA, state.BaseCommit),
		MissionSHA256: missionSHA, AcceptanceContractSHA256: state.AcceptanceContractSHA256,
		SemanticReviewContractSHA256: state.SemanticReviewContractSHA256,
		AcceptanceCommands:           qualificationCommandIdentities(state, true),
		VerifierCommands:             qualificationCommandIdentities(state, false),
		ProviderInstanceID:           context.ProviderID, StartedAt: context.StartedAt,
		HourlyUSD: context.HourlyUSD, CostUSD: context.CostUSD,
		Artifacts:      artifacts,
		MissionOutcome: state.MissionOutcome,
	}
	if context.StartedAt != "" {
		if startedAt, parseErr := time.Parse(time.RFC3339Nano, context.StartedAt); parseErr == nil && !startedAt.After(manifest.CreatedAt) {
			manifest.RuntimeMilliseconds = manifest.CreatedAt.Sub(startedAt).Milliseconds()
			if context.HourlyUSD > 0 {
				manifest.RuntimeCostEstimateUSD = float64(manifest.RuntimeMilliseconds) / float64(time.Hour/time.Millisecond) * context.HourlyUSD
			}
		}
	}
	if state.Exec != nil {
		manifest.Provider, manifest.Model, manifest.Reasoning = state.Exec.Provider, state.Exec.Model, state.Exec.Reasoning
	}
	if len(events) > 0 {
		manifest.FirstEventSequence, manifest.LastEventSequence = events[0].Sequence, events[len(events)-1].Sequence
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", QualificationManifest{}, err
	}
	if err := os.WriteFile(filepath.Join(stage, "qualification-manifest.json"), append(manifestBytes, '\n'), 0o600); err != nil {
		return "", QualificationManifest{}, err
	}
	if err := os.MkdirAll(filepath.Dir(bundleDir), 0o700); err != nil {
		return "", QualificationManifest{}, err
	}
	if err := os.Rename(stage, bundleDir); err != nil {
		return "", QualificationManifest{}, err
	}
	return bundleDir, manifest, nil
}

func VerifyQualificationBundle(bundleDir string) (QualificationManifest, string, error) {
	manifestPath := filepath.Join(bundleDir, "qualification-manifest.json")
	data, err := readQualificationFile(manifestPath)
	if err != nil {
		return QualificationManifest{}, "", err
	}
	var manifest QualificationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return QualificationManifest{}, "", err
	}
	if manifest.SchemaVersion != QualificationBundleSchema || manifest.RunID == "" {
		return QualificationManifest{}, "", errors.New("qualification manifest schema or run ID is invalid")
	}
	seen := map[string]bool{}
	var total int64
	for _, artifact := range manifest.Artifacts {
		clean := filepath.Clean(filepath.FromSlash(artifact.Path))
		if artifact.Path == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean != filepath.FromSlash(artifact.Path) || seen[artifact.Path] {
			return QualificationManifest{}, "", fmt.Errorf("qualification artifact path %q is invalid or duplicated", artifact.Path)
		}
		seen[artifact.Path] = true
		path := filepath.Join(bundleDir, clean)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() != artifact.Bytes || info.Size() > qualificationFileLimit {
			return QualificationManifest{}, "", fmt.Errorf("qualification artifact %q is missing or has an invalid size", artifact.Path)
		}
		content, err := readQualificationFile(path)
		if err != nil {
			return QualificationManifest{}, "", err
		}
		if sha256Hex(content) != artifact.SHA256 {
			return QualificationManifest{}, "", fmt.Errorf("qualification artifact %q failed its SHA-256 check", artifact.Path)
		}
		total += int64(len(content))
		if total > qualificationBundleLimit {
			return QualificationManifest{}, "", errors.New("qualification bundle exceeds its size limit")
		}
	}
	if _, eventArtifact := seen["run-events.jsonl"]; eventArtifact {
		if _, stateArtifact := seen["deep.json"]; !stateArtifact {
			return QualificationManifest{}, "", errors.New("qualification bundle has a journal but no run projection")
		}
		if err := verifyQualificationRun(filepath.Join(bundleDir, "run-events.jsonl"), filepath.Join(bundleDir, "deep.json"), manifest); err != nil {
			return QualificationManifest{}, "", err
		}
	}
	manifestSHA := sha256.Sum256(data)
	return manifest, hex.EncodeToString(manifestSHA[:]), nil
}

func verifyQualificationRun(journalPath, projectionPath string, manifest QualificationManifest) error {
	root, err := os.MkdirTemp("", "stint-qualification-verify-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	dir := DeepDir(root, manifest.RunID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for source, name := range map[string]string{journalPath: runEventFileName, projectionPath: "deep.json"} {
		data, err := readQualificationFile(source)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return err
		}
	}
	events, err := ReadRunEvents(root, manifest.RunID)
	if err != nil {
		return fmt.Errorf("validate archived run journal: %w", err)
	}
	if len(events) == 0 || events[0].Sequence != manifest.FirstEventSequence || events[len(events)-1].Sequence != manifest.LastEventSequence || uint64(len(events)) != manifest.ProjectionWatermark {
		return fmt.Errorf("archived journal sequence differs from manifest/watermark %d..%d/%d", manifest.FirstEventSequence, manifest.LastEventSequence, manifest.ProjectionWatermark)
	}
	state, err := LoadState(root, manifest.RunID)
	if err != nil {
		return fmt.Errorf("validate archived run projection and Git identities: %w", err)
	}
	if state.RunID != manifest.RunID || state.RunEventWatermark != manifest.ProjectionWatermark {
		return errors.New("archived run projection identity or watermark differs from manifest")
	}
	return nil
}

func qualificationCommandIdentities(state DeepState, acceptance bool) map[string]string {
	commands := map[string]string{}
	if state.Verify != "" && !acceptance {
		commands["mission"] = VerificationCommandIdentity(state.Verify)
	}
	for _, task := range state.Tasks {
		command := task.Verify
		if acceptance {
			command = task.AcceptanceCheck
		}
		if command != "" {
			commands[task.ID] = VerificationCommandIdentity(command)
		}
	}
	return commands
}

func qualificationCopyArtifact(source, stage, name string) (QualificationArtifact, error) {
	data, err := readQualificationFile(source)
	if err != nil {
		return QualificationArtifact{}, err
	}
	return qualificationWriteArtifact(stage, name, data)
}

func qualificationWriteArtifact(stage, name string, data []byte) (QualificationArtifact, error) {
	if int64(len(data)) > qualificationFileLimit {
		return QualificationArtifact{}, fmt.Errorf("qualification artifact %q exceeds %d bytes", name, qualificationFileLimit)
	}
	for _, pattern := range qualificationSecretPatterns {
		if pattern.Match(data) {
			return QualificationArtifact{}, fmt.Errorf("qualification artifact %q contains a credential-like value", name)
		}
	}
	destination := filepath.Join(stage, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return QualificationArtifact{}, err
	}
	if err := os.WriteFile(destination, data, 0o600); err != nil {
		return QualificationArtifact{}, err
	}
	sum := sha256.Sum256(data)
	mediaType := "application/octet-stream"
	if strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".jsonl") {
		mediaType = "application/json"
	}
	return QualificationArtifact{Path: filepath.ToSlash(name), Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), MediaType: mediaType}, nil
}

func readQualificationFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > qualificationFileLimit {
		return nil, fmt.Errorf("qualification input %q is not a bounded regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, qualificationFileLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > qualificationFileLimit {
		return nil, fmt.Errorf("qualification input %q exceeds its size limit", path)
	}
	return data, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
