package deep

import (
	"strings"
	"testing"
	"time"
)

func TestComputeBindingAndExplicitRebind(t *testing.T) {
	at := time.Date(2026, 9, 22, 12, 0, 0, 0, time.FixedZone("BST", 3600))
	state := &DeepState{}
	if err := state.BindCompute(" vast ", 101, at); err != nil {
		t.Fatalf("BindCompute: %v", err)
	}
	if state.ComputeBinding.Provider != "vast" || state.ComputeBinding.InstanceID != 101 || !state.ComputeBinding.BoundAt.Equal(at.UTC()) {
		t.Fatalf("binding = %+v", state.ComputeBinding)
	}
	if err := state.BindCompute("vast", 101, at); err == nil {
		t.Fatal("second BindCompute succeeded; expected explicit rebind requirement")
	}
	if err := state.RebindCompute("vast", 202, "replacement instance after expiry", at.Add(time.Hour)); err != nil {
		t.Fatalf("RebindCompute: %v", err)
	}
	binding := state.ComputeBinding
	if binding.InstanceID != 202 || len(binding.Rebinds) != 1 {
		t.Fatalf("binding after rebind = %+v", binding)
	}
	transition := binding.Rebinds[0]
	if transition.FromInstanceID != 101 || transition.ToInstanceID != 202 || transition.Reason != "replacement instance after expiry" || !transition.At.Equal(at.Add(time.Hour).UTC()) {
		t.Errorf("rebind transition = %+v", transition)
	}
	if err := state.RebindCompute("vast", 202, "already current", at); err == nil {
		t.Fatal("rebind to already-bound instance succeeded")
	}
	if err := state.RebindCompute("other", 303, "provider change", at); err == nil {
		t.Fatal("provider-changing rebind succeeded")
	}
}

func TestComputeRebindRequiresAuditReason(t *testing.T) {
	state := &DeepState{}
	if err := state.RebindCompute("vast", 202, " ", time.Now()); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("RebindCompute err = %v, want required reason", err)
	}
}

func TestComputeBindingPersists(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	mission, err := ParseMission(sampleMission)
	if err != nil {
		t.Fatal(err)
	}
	state := NewState("20260922-120000", mission, "/repo", "/worktree", now.Add(time.Hour), now.Add(50*time.Minute), 3, now)
	if err := state.BindCompute("vast", 101, now); err != nil {
		t.Fatal(err)
	}
	if err := state.RebindCompute("vast", 202, "operator confirmed replacement", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveDir(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(dir, state.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ComputeBinding == nil || loaded.ComputeBinding.InstanceID != 202 || len(loaded.ComputeBinding.Rebinds) != 1 || loaded.ComputeBinding.Rebinds[0].FromInstanceID != 101 {
		t.Fatalf("persisted binding = %+v", loaded.ComputeBinding)
	}
}
