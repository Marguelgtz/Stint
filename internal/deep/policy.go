package deep

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Incident kinds in the session's incident log. The log records the
// coordinator's safety-relevant surface: the command policy it runs with,
// every executor invocation, every verification run, and every state or
// checkpoint failure. It is the audit trail an operator reads when a session
// did something they want to understand.
const (
	IncidentPolicy         = "policy"            // the session's worker command policy, at coordinator start/resume
	IncidentExecutorInvoke = "executor-invoke"   // one bounded coding-agent invocation started
	IncidentExecutorError  = "executor-error"    // the invocation errored (not: it produced unverified work)
	IncidentVerifyRun      = "verify-run"        // a verification command ran; result=pass|fail in the detail
	IncidentCheckpointFail = "checkpoint-failed" // the verified-task checkpoint commit failed
	IncidentExternalStop   = "external-stop"     // another process changed the durable phase mid-run
	IncidentStateSave      = "state-save-failed"
	IncidentLanded         = "landed"  // or stopped: the detail carries the reason
	IncidentResumed        = "resumed" // a coordinator restarted the session from durable state
)

// Incident is one machine-readable coordinator event. One JSON object per
// line in incidents.jsonl; a single-line write keeps the loss from a crash
// to at most one record.
type Incident struct {
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"`
	Task   string    `json:"task,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

// IncidentFile is the session's incident log, inside the state dir.
func IncidentFile(stateDir, sessionID string) string {
	return filepath.Join(DeepDir(stateDir, sessionID), "incidents.jsonl")
}

// AppendIncident records one incident (best effort: auditing must never fail
// the run, mirroring AppendLog).
func AppendIncident(stateDir string, s DeepState, kind, taskID, detail string) {
	rec := Incident{Time: time.Now().UTC(), Kind: kind, Task: taskID, Detail: detail}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	f, err := os.OpenFile(IncidentFile(stateDir, s.SessionID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// ReadIncidents returns the recorded incidents in order. Unparseable lines
// (a crash mid-write of the last record) are skipped.
func ReadIncidents(stateDir, sessionID string) ([]Incident, error) {
	data, err := os.ReadFile(IncidentFile(stateDir, sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Incident
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec Incident
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// CommandPolicySection renders the session's worker command policy for the
// reconstructed prompt. Enforcement comes from the Cline CLI's approval
// mode: with auto-approval OFF (the default) commands outside the list are
// denied, so the prompt statement is a hard boundary in practice; with
// auto-approval ON the CLI approves everything and the list is advisory,
// which the section says plainly.
func CommandPolicySection(allowed []string, autoApprove bool) string {
	if len(allowed) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nCOMMAND POLICY:\n")
	b.WriteString("The only shell commands permitted in this workspace are those beginning with:\n")
	for _, a := range allowed {
		fmt.Fprintf(&b, "- %s\n", a)
	}
	b.WriteString("Do not run any other command; if the task requires one, report it as a blocker instead of running it.\n")
	if autoApprove {
		b.WriteString("Note: tool auto-approval is ON for this session, so the CLI cannot deny commands on the operator's behalf; this policy is advisory — treat it as strict.\n")
	} else {
		b.WriteString("Note: tool auto-approval is OFF; commands outside this list are denied by the CLI.\n")
	}
	return b.String()
}
