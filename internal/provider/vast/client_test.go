package vast

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Marguelgtz/Stint/internal/core"
)

func TestVerifyAuthUsesV1Instances(t *testing.T) {
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

func TestSearchOffersBuildsBroadReadOnlyDiscoveryRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v0/bundles" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		assertFilter(t, payload, "gpu_name", "eq", "RTX_4090")
		assertFilter(t, payload, "num_gpus", "eq", float64(1))
		assertFilter(t, payload, "dph_total", "lte", discoveryPriceCeilingUSD)
		for _, key := range []string{"reliability", "inet_down", "direct_port_count", "gpu_ram", "gpu_max_power"} {
			if _, ok := payload[key]; ok {
				t.Fatalf("provider discovery unexpectedly hard-filtered %q: %#v", key, payload[key])
			}
		}
		if got := payload["type"]; got != vastOnDemandType {
			t.Fatalf("type = %#v", got)
		}
		if got := payload["allocated_storage"]; got != float64(50) {
			t.Fatalf("allocated_storage = %#v", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"offers":[{"id":123,"gpu_name":"RTX_4090","gpu_ram":24576,"gpu_max_power":300,"dph_total":0.347,"reliability":0.995,"dlperf":113.2,"inet_down":40,"inet_up":200,"inet_down_cost":0.001,"direct_port_count":2,"geolocation":"NL","machine_id":77,"verification":"verified","rentable":true,"rented":false,"num_gpus":1}]}`))
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	offers, err := client.SearchOffers(context.Background(), core.BuiltinProfiles["interactive"], SearchOptions{Hours: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 {
		t.Fatalf("offers = %#v", offers)
	}
	offer := offers[0]
	if offer.ID != "123" || offer.GPUModel != "RTX_4090" || offer.HourlyUSD != 0.347 || !offer.Verified {
		t.Fatalf("offer = %#v", offer)
	}
}

func TestSearchOffersBisectsZeroInventory(t *testing.T) {
	request := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if got := payload["type"]; got != vastOnDemandType {
			t.Fatalf("request %d type = %#v", request, got)
		}
		count := 0
		switch request {
		case 1:
			count = 0 // full query triggers trace
		case 2:
			count = 4 // rentable inventory
		case 3:
			count = 3 // RTX 4090
		case 4:
			count = 2 // one GPU
		case 5:
			count = 2 // duration
		case 6:
			count = 0 // price collapses the search
		case 7:
			count = 0 // storage/full shape remains zero
		default:
			t.Fatalf("unexpected request %d", request)
		}
		offers := make([]map[string]any, 0, count)
		for i := 0; i < count; i++ {
			offers = append(offers, map[string]any{"id": i + 1, "gpu_name": "RTX_4090"})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"offers": offers})
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	_, err := client.SearchOffers(context.Background(), core.BuiltinProfiles["interactive"], SearchOptions{Hours: 1})
	if err == nil {
		t.Fatal("expected discovery empty error")
	}
	var empty *DiscoveryEmptyError
	if !errors.As(err, &empty) {
		t.Fatalf("err = %T %v", err, err)
	}
	want := []DiscoveryStage{{"rentable", 4}, {"gpu", 3}, {"one_gpu", 2}, {"duration", 2}, {"price", 0}, {"storage", 0}}
	if len(empty.Stages) != len(want) {
		t.Fatalf("stages = %#v", empty.Stages)
	}
	for i := range want {
		if empty.Stages[i] != want[i] {
			t.Fatalf("stage %d = %#v, want %#v", i, empty.Stages[i], want[i])
		}
	}
}

func TestSearchOffersPermissionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden","msg":"permission denied"}`))
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	_, err := client.SearchOffers(context.Background(), core.BuiltinProfiles["interactive"], SearchOptions{Hours: 1})
	if err == nil || err.Error() != "Vast API key lacks misc/search permission" {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchOffersRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"offers":{"id":123}}`))
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	if _, err := client.SearchOffers(context.Background(), core.BuiltinProfiles["interactive"], SearchOptions{Hours: 1}); err == nil {
		t.Fatal("expected malformed response error")
	}
}

func TestIntegrationSearchOffers(t *testing.T) {
	if os.Getenv("STINT_VAST_INTEGRATION") != "1" {
		t.Skip("set STINT_VAST_INTEGRATION=1 to query the real Vast marketplace")
	}
	key := os.Getenv("VAST_API_KEY")
	if key == "" {
		t.Fatal("VAST_API_KEY is required for integration test")
	}
	client := NewClient(key)
	if _, err := client.SearchOffers(context.Background(), core.BuiltinProfiles["interactive"], SearchOptions{Hours: 1, Limit: 10}); err != nil {
		t.Fatal(err)
	}
}

func assertFilter(t *testing.T, payload map[string]any, key, operator string, expected any) {
	t.Helper()
	filter, ok := payload[key].(map[string]any)
	if !ok {
		t.Fatalf("%s filter = %#v", key, payload[key])
	}
	if got := filter[operator]; got != expected {
		t.Fatalf("%s.%s = %#v, want %#v", key, operator, got, expected)
	}
}
