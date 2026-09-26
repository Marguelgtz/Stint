package deep

// VerificationSubject identifies the exact Git-visible product tree exercised
// by a verifier and the HEAD from which that worktree state was observed.
// Git-ignored inputs and external runtime state are outside this identity.
type VerificationSubject struct {
	HeadCommit string `json:"headCommit"`
	TreeSHA    string `json:"treeSha"`
}
