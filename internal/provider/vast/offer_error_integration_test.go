package vast

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateInstancePreservesRetryableNoSuchAsk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"msg":"error 404/3603: no_such_ask Instance type by id 37888568 is not available."}`))
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	_, err := client.CreateInstance(context.Background(), "37888568", CreateInstanceOptions{Image: "ubuntu:24.04", DiskGB: 50})
	if err == nil {
		t.Fatal("CreateInstance unexpectedly succeeded")
	}
	if !IsOfferUnavailableError(err) {
		t.Fatalf("CreateInstance no_such_ask error was not classified retryable: %v", err)
	}
}

func TestCreateInstanceDoesNotRetryArbitraryBadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"msg":"invalid image"}`))
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	_, err := client.CreateInstance(context.Background(), "123", CreateInstanceOptions{Image: "bad-image", DiskGB: 50})
	if err == nil {
		t.Fatal("CreateInstance unexpectedly succeeded")
	}
	if IsOfferUnavailableError(err) {
		t.Fatalf("arbitrary 400 must remain fatal, got retryable classification: %v", err)
	}
}
