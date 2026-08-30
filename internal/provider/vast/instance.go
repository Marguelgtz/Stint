package vast

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const (
	vastAutomaticBaseImage = "vastai/base-image:@vastai-automatic-tag"
	stintCUDA124BaseImage  = "vastai/base-image:cuda-12.4.1-cudnn-devel-ubuntu22.04-py310"
)

type CreateInstanceOptions struct {
	Image   string
	DiskGB  float64
	Label   string
	OnStart string
}

type PortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type Instance struct {
	ID           int64                    `json:"id"`
	ActualStatus string                   `json:"actual_status"`
	StatusMsg    string                   `json:"status_msg"`
	ImageRuntype string                   `json:"image_runtype"`
	SSHHost      string                   `json:"ssh_host"`
	SSHPort      int                      `json:"ssh_port"`
	PublicIPAddr string                   `json:"public_ipaddr"`
	Ports        map[string][]PortBinding `json:"ports"`
	GPUName      string                   `json:"gpu_name"`
	GPURAM       int                      `json:"gpu_ram"`
	DPHTotal     float64                  `json:"dph_total"`
	Verification string                   `json:"verification"`
}

type createInstanceResponse struct {
	Success     bool  `json:"success"`
	NewContract int64 `json:"new_contract"`
}

type showInstanceResponse struct {
	Instances Instance `json:"instances"`
}

type successResponse struct {
	Success bool   `json:"success"`
	Msg     string `json:"msg"`
}

func (c *Client) CreateInstance(ctx context.Context, offerID string, options CreateInstanceOptions) (int64, error) {
	if c.APIKey == "" {
		return 0, errors.New("Vast API key is empty")
	}
	id, err := strconv.ParseInt(offerID, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid Vast offer id %q", offerID)
	}
	if strings.TrimSpace(options.Image) == "" {
		return 0, errors.New("instance image is required")
	}
	if options.DiskGB <= 0 {
		return 0, errors.New("instance disk size must be greater than zero")
	}

	image := normalizeInstanceImage(options.Image)
	payload := map[string]any{
		"image":          image,
		"disk":           options.DiskGB,
		"runtype":        "ssh_direct",
		"target_state":   "running",
		"cancel_unavail": true,
	}
	if options.Label != "" {
		payload["label"] = options.Label
	}
	if options.OnStart != "" {
		payload["onstart"] = options.OnStart
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("encode Vast create instance request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.endpoint(fmt.Sprintf("/api/v0/asks/%d", id)), bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build Vast create instance request: %w", err)
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return 0, fmt.Errorf("create Vast instance: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, classifyInstanceWriteError(decodeAPIError(resp))
	}
	var result createInstanceResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode Vast create instance response: %w", err)
	}
	if !result.Success || result.NewContract <= 0 {
		return 0, errors.New("Vast did not return a valid instance id")
	}
	return result.NewContract, nil
}

func normalizeInstanceImage(image string) string {
	image = strings.TrimSpace(image)
	if image == vastAutomaticBaseImage {
		// Vast's automatic tag resolver currently returns no_compatible_tag on some
		// consumer 4090 hosts even when a compatible base-image tag exists. Pin a
		// CUDA 12.4 image that is valid on the observed CUDA 12.6 hosts instead.
		return stintCUDA124BaseImage
	}
	return image
}

func (i Instance) resolvedSSHEndpoint() (string, int) {
	if strings.EqualFold(strings.TrimSpace(i.ImageRuntype), "ssh_direct") {
		host := strings.TrimSpace(i.PublicIPAddr)
		if host != "" {
			for _, binding := range i.Ports["22/tcp"] {
				port, err := strconv.Atoi(strings.TrimSpace(binding.HostPort))
				if err == nil && port > 0 && port <= 65535 {
					return host, port
				}
			}
		}
	}
	return strings.TrimSpace(i.SSHHost), i.SSHPort
}

func (c *Client) ShowInstance(ctx context.Context, instanceID int64) (Instance, error) {
	if instanceID <= 0 {
		return Instance{}, errors.New("invalid Vast instance id")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(fmt.Sprintf("/api/v0/instances/%d", instanceID)), nil)
	if err != nil {
		return Instance{}, fmt.Errorf("build Vast show instance request: %w", err)
	}
	c.authorize(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Instance{}, fmt.Errorf("show Vast instance: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Instance{}, decodeAPIError(resp)
	}
	var result showInstanceResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result); err != nil {
		return Instance{}, fmt.Errorf("decode Vast show instance response: %w", err)
	}
	instance := result.Instances
	instance.SSHHost, instance.SSHPort = instance.resolvedSSHEndpoint()
	return instance, nil
}

func (c *Client) AttachSSHKey(ctx context.Context, instanceID int64, publicKey string) error {
	if instanceID <= 0 {
		return errors.New("invalid Vast instance id")
	}
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return errors.New("SSH public key is empty")
	}
	body, err := json.Marshal(map[string]any{"ssh_key": publicKey})
	if err != nil {
		return fmt.Errorf("encode SSH key request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(fmt.Sprintf("/api/v0/instances/%d/ssh", instanceID)), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build Vast attach SSH key request: %w", err)
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("attach Vast SSH key: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return classifyInstanceWriteError(decodeAPIError(resp))
	}

	// The per-instance attach endpoint can report success even when a host has
	// created authorized_keys with modes OpenSSH StrictModes refuses. Vast's
	// instance command channel does not depend on SSH, so use it as a best-effort
	// repair path. Fresh instances also receive an onstart permission watcher.
	_ = c.RepairSSHPermissions(ctx, instanceID)
	return nil
}

func (c *Client) DestroyInstance(ctx context.Context, instanceID int64) error {
	if instanceID <= 0 {
		return errors.New("invalid Vast instance id")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint(fmt.Sprintf("/api/v0/instances/%d", instanceID)), nil)
	if err != nil {
		return fmt.Errorf("build Vast destroy instance request: %w", err)
	}
	c.authorize(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("destroy Vast instance: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return classifyInstanceWriteError(decodeAPIError(resp))
	}
	var result successResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err == nil && !result.Success {
		return fmt.Errorf("Vast destroy failed: %s", result.Msg)
	}
	return nil
}

func classifyInstanceWriteError(err error) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusForbidden:
			return errors.New("Vast API key lacks instance_write permission; enable instance_write for the Stint key, then run: stint auth vast")
		case http.StatusUnauthorized:
			return errors.New("Vast API key was rejected; run: stint auth vast")
		case http.StatusNotFound, http.StatusGone:
			return errors.New("selected Vast offer is no longer available; rerun stint start")
		case http.StatusTooManyRequests:
			return errors.New("Vast API rate limit reached; retry later")
		}
	}
	return err
}
