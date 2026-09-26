package deep

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// ReopenAfterLanding starts a new execution epoch without carrying forward
// stale terminal reporting. The previous landing identity remains durable in
// PreviousLandings for audit and publication reconciliation.
func (s *DeepState) ReopenAfterLanding(now time.Time) bool {
	if s.Phase != PhaseLanded {
		return false
	}
	record := LandingRecord{
		At:                  now.UTC(),
		Reason:              s.LandingReason,
		Commit:              s.LandingCommit,
		CheckpointTreeSHA:   s.LandingCheckpointTreeSHA,
		Verification:        s.LandingVerify,
		VerificationSubject: s.LandingVerificationSubject,
	}
	if s.LandedAt != nil {
		record.At = s.LandedAt.UTC()
	}
	if s.LandingHandoff != "" {
		record.HandoffSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(s.LandingHandoff)))
	}
	s.PreviousLandings = append(s.PreviousLandings, record)
	s.Phase = PhaseExecuting
	s.LandedAt = nil
	s.LandingReason = ""
	s.LandingCommit = ""
	s.LandingCheckpointTreeSHA = ""
	s.LandingVerify = ""
	s.LandingVerifyDone = false
	s.LandingVerificationSubject = nil
	s.LandingVerificationBookkeeping = nil
	s.LandingHandoff = ""
	return true
}
