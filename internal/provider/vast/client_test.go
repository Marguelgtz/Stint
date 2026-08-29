package vast

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		if r.URL.Path != "/api/v1/instances" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"instances_found":0,"instances":[]}`))
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	if err := client.VerifyAuth(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyAuthRejectsUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewClient("bad-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	if err := client.VerifyAuth(context.Background()); err == nil {
		t.Fatal("expected authentication error")
	}
}
