package main

import "testing"

func TestDashboardContextsProjectObservedLaneState(t *testing.T) {
	used, contexts := dashboardClientContexts([]inferenceLane{
		{ID: 0, Session: "old-session", NPrompt: 7005, NCached: 6500, Retained: true},
		{ID: 1, Session: "stint-session", NPrompt: 148000, NCached: 140000, Processing: true},
	})
	if used != 155005 {
		t.Fatalf("used = %d, want 155005", used)
	}
	if len(contexts) != 2 {
		t.Fatalf("contexts = %d, want 2", len(contexts))
	}
	if contexts[0].Key != "lane:0" || contexts[0].Label != "lane 1 · idle retained · cache 93% · depth" || contexts[0].Tokens != 7005 {
		t.Fatalf("unexpected retained lane: %#v", contexts[0])
	}
	if contexts[1].Key != "lane:1" || contexts[1].Label != "lane 2 · active · cache 95% · depth" || contexts[1].Tokens != 148000 {
		t.Fatalf("unexpected processing lane: %#v", contexts[1])
	}
}

func TestDashboardContextsDoNotTreatSessionDigestAsClientIdentity(t *testing.T) {
	used, contexts := dashboardClientContexts([]inferenceLane{
		{ID: 0, Session: "same-runtime-digest", NPrompt: 48000, Retained: true},
		{ID: 1, Session: "same-runtime-digest", NPrompt: 50000, Processing: true},
	})
	if used != 98000 {
		t.Fatalf("used = %d, want both observed lane depths 98000", used)
	}
	if len(contexts) != 2 {
		t.Fatalf("contexts = %d, want two observed lanes", len(contexts))
	}
	if contexts[0].Key == contexts[1].Key {
		t.Fatalf("lane keys should remain distinct: %#v", contexts)
	}
	for _, context := range contexts {
		if context.Label == "client same-r" {
			t.Fatalf("runtime digest must not become a client label: %#v", contexts)
		}
	}
}

func TestDashboardContextsAllowDifferentHistoriesToReuseLane(t *testing.T) {
	_, first := dashboardClientContexts([]inferenceLane{
		{ID: 1, Session: "stint-history", NPrompt: 148000, Processing: true},
	})
	_, second := dashboardClientContexts([]inferenceLane{
		{ID: 1, Session: "spark-history", NPrompt: 109000, Processing: true},
	})
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("expected one observed lane in each sample: first=%#v second=%#v", first, second)
	}
	if first[0].Key != second[0].Key || first[0].Label != second[0].Label {
		t.Fatalf("lane identity should remain lane-based across history reuse: first=%#v second=%#v", first[0], second[0])
	}
	if first[0].Tokens == second[0].Tokens {
		t.Fatalf("test requires distinct resident depths: first=%#v second=%#v", first[0], second[0])
	}
}

func TestDashboardContextsKeepEmptyLanesVisible(t *testing.T) {
	used, contexts := dashboardClientContexts([]inferenceLane{
		{ID: 0, NPrompt: 25000, NCached: 20000},
		{ID: 1, NPrompt: 0},
		{ID: 2, NPrompt: 10000, NCached: 5000},
	})
	if used != 35000 {
		t.Fatalf("used = %d, want 35000", used)
	}
	if len(contexts) != 3 {
		t.Fatalf("contexts = %d, want all 3 observed lanes", len(contexts))
	}
	if contexts[0].Label != "lane 1 · idle resident · cache 80% · depth" {
		t.Fatalf("unexpected first lane label: %#v", contexts[0])
	}
	if contexts[1].Label != "lane 2 · idle · cache — · depth" || contexts[1].Tokens != 0 {
		t.Fatalf("empty lane should remain visible as idle: %#v", contexts[1])
	}
	if contexts[2].Label != "lane 3 · idle resident · cache 50% · depth" {
		t.Fatalf("unexpected third lane label: %#v", contexts[2])
	}
}
