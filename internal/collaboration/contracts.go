package collaboration

// WorkPacket is the narrow boundary Stint will eventually hand to Spark's
// collaboration engine. Stint owns compute lifecycle; Spark owns evidence and
// reciprocal collaboration semantics.
type WorkPacket struct { ID string; RepoPath string; Branch string; Intent string }
