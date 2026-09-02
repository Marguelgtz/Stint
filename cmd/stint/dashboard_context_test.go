package main

import "testing"

func TestDashboardClientContextsGroupsBySession(t *testing.T) {
	used, clients := dashboardClientContexts([]inferenceLane{
		{ID: 0, Session: "aaaaaa111111", NPrompt: 50000, Retained: true},
		{ID: 1, Session: "bbbbbb222222", NPrompt: 50000, Processing: true},
	})
	if used != 100000 {
		t.Fatalf("used = %d, want 100000", used)
	}
	if len(clients) != 2 {
		t.Fatalf("clients = %d, want 2", len(clients))
	}
	if clients[0].Tokens != 50000 || clients[1].Tokens != 50000 {
		t.Fatalf("unexpected client token split: %#v", clients)
	}
	if clients[0].Key == clients[1].Key {
		t.Fatalf("client keys should be distinct: %#v", clients)
	}
}

func TestDashboardClientContextsDoesNotDoubleCountSessionHandoff(t *testing.T) {
	used, clients := dashboardClientContexts([]inferenceLane{
		{ID: 0, Session: "same-session", NPrompt: 48000, Retained: true},
		{ID: 1, Session: "same-session", NPrompt: 50000, Processing: true},
	})
	if used != 50000 {
		t.Fatalf("used = %d, want largest resident prompt 50000", used)
	}
	if len(clients) != 1 || clients[0].Tokens != 50000 {
		t.Fatalf("unexpected grouped clients: %#v", clients)
	}
}

func TestDashboardClientContextsFallsBackToLaneIdentity(t *testing.T) {
	used, clients := dashboardClientContexts([]inferenceLane{
		{ID: 0, NPrompt: 25000},
		{ID: 1, NPrompt: 0},
		{ID: 2, NPrompt: 10000},
	})
	if used != 35000 {
		t.Fatalf("used = %d, want 35000", used)
	}
	if len(clients) != 2 {
		t.Fatalf("clients = %d, want 2", len(clients))
	}
	if clients[0].Label != "client 1" || clients[1].Label != "client 3" {
		t.Fatalf("unexpected lane fallback labels: %#v", clients)
	}
}
