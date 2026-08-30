package vast

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateInstanceUsesSelectedOfferAndSSHDirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v0/asks/123" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["image"] != "ghcr.io/ggml-org/llama.cpp:server-cuda" {
			t.Fatalf("image = %#v", payload["image"])
		}
		if payload["disk"] != float64(50) {
			t.Fatalf("disk = %#v", payload["disk"])
		}
		if payload["runtype"] != "ssh_direct" {
			t.Fatalf("runtype = %#v", payload["runtype"])
		}
		if payload["target_state"] != "running" {
			t.Fatalf("target_state = %#v", payload["target_state"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"new_contract":9876}`))
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	id, err := client.CreateInstance(context.Background(), "123", CreateInstanceOptions{
		Image: "ghcr.io/ggml-org/llama.cpp:server-cuda", DiskGB: 50, Label: "stint-interactive",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != 9876 {
		t.Fatalf("id = %d", id)
	}
}

func TestCreateInstancePinsAutomaticVastBaseImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["image"] != stintCUDA124BaseImage {
			t.Fatalf("image = %#v, want %q", payload["image"], stintCUDA124BaseImage)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"new_contract":9876}`))
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	if _, err := client.CreateInstance(context.Background(), "123", CreateInstanceOptions{Image: vastAutomaticBaseImage, DiskGB: 50}); err != nil {
		t.Fatal(err)
	}
}

func TestAttachSSHKeyUsesInstanceEndpointAndRepairsStrictModes(t *testing.T) {
	request := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request++
		switch request {
		case 1:
			if r.Method != http.MethodPost || r.URL.Path != "/api/v0/instances/9876/ssh" {
				t.Fatalf("attach request = %s %s", r.Method, r.URL.Path)
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["ssh_key"] != "ssh-ed25519 AAAATEST stint" {
				t.Fatalf("ssh_key = %#v", payload["ssh_key"])
			}
			_, _ = w.Write([]byte(`{"success":true,"msg":"SSH key attached successfully"}`))
		case 2:
			if r.Method != http.MethodPut || r.URL.Path != "/api/v0/instances/command/9876/" {
				t.Fatalf("repair request = %s %s", r.Method, r.URL.Path)
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			command, _ := payload["command"].(string)
			for _, required := range []string{"chown root:root", "chmod 700", "chmod 600 /root/.ssh/authorized_keys"} {
				if !strings.Contains(command, required) {
					t.Fatalf("repair command missing %q: %s", required, command)
				}
			}
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			t.Fatalf("unexpected request %d: %s %s", request, r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	if err := client.AttachSSHKey(context.Background(), 9876, "ssh-ed25519 AAAATEST stint"); err != nil {
		t.Fatal(err)
	}
	if request != 2 {
		t.Fatalf("requests = %d, want 2", request)
	}
}

func TestShowAndDestroyInstanceEndpoints(t *testing.T) {
	request := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request++
		switch request {
		case 1:
			if r.Method != http.MethodGet || r.URL.Path != "/api/v0/instances/9876" {
				t.Fatalf("show request = %s %s", r.Method, r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"instances":{"id":9876,"actual_status":"running","ssh_host":"ssh1.vast.ai","ssh_port":12345,"gpu_name":"RTX 3090","gpu_ram":24576,"dph_total":0.14}}`))
		case 2:
			if r.Method != http.MethodDelete || r.URL.Path != "/api/v0/instances/9876" {
				t.Fatalf("destroy request = %s %s", r.Method, r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"success":true,"msg":"destroyed"}`))
		default:
			t.Fatalf("unexpected request %d", request)
		}
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	instance, err := client.ShowInstance(context.Background(), 9876)
	if err != nil {
		t.Fatal(err)
	}
	if instance.SSHHost != "ssh1.vast.ai" || instance.SSHPort != 12345 || instance.ActualStatus != "running" {
		t.Fatalf("instance = %#v", instance)
	}
	if err := client.DestroyInstance(context.Background(), 9876); err != nil {
		t.Fatal(err)
	}
}

func TestShowInstanceUsesSSHDirectPortMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v0/instances/9876" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"instances":{"id":9876,"actual_status":"running","image_runtype":"ssh_direct","ssh_host":"ssh7.vast.ai","ssh_port":35446,"public_ipaddr":"189.79.25.23","ports":{"22/tcp":[{"HostIp":"0.0.0.0","HostPort":"42831"},{"HostIp":"::","HostPort":"42831"}]}}}`))
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	instance, err := client.ShowInstance(context.Background(), 9876)
	if err != nil {
		t.Fatal(err)
	}
	if instance.SSHHost != "189.79.25.23" || instance.SSHPort != 42831 {
		t.Fatalf("SSH endpoint = %s:%d, want 189.79.25.23:42831", instance.SSHHost, instance.SSHPort)
	}
}

func TestCreateInstanceExplainsMissingWritePermission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden","msg":"permission denied"}`))
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	_, err := client.CreateInstance(context.Background(), "123", CreateInstanceOptions{Image: "ubuntu:22.04", DiskGB: 50})
	if err == nil || err.Error() != "Vast API key lacks instance_write permission; enable instance_write for the Stint key, then run: stint auth vast" {
		t.Fatalf("err = %v", err)
	}
}
