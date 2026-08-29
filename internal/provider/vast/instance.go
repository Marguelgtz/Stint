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

type CreateInstanceOptions struct {
	Image   string
	DiskGB  float64
	Label   string
	OnStart string
}

type Instance struct {
	ID           int64   `json:"id"`
	ActualStatus string  `json:"actual_status"`
	StatusMsg    string  `json:"status_msg"`
	SSHHost      string  `json:"ssh_host"`
	SSHPort      int     `json:"ssh_port"`
	GPUName      string  `json:"gpu_name"`
	GPURAM       int     `json:"gpu_ram"`
	DPHTotal     float64 `json:"dph_total"`
	Verification string  `json:"verification"`
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
	payload := map[string]any{
		"image":        options.Image,
		"disk":         options.DiskGB,
		"runtype":      "ssh_direct",
		"target_state": "running",
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.endpoint(fmt.Sprintf("/api/v0/asks/%d/", id)), bytes.NewReader(body))
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

func (c *Client) ShowInstance(ctx context.Context, instanceID int64) (Instance, error) {
	if instanceID <= 0 {
		return Instance{}, errors.New("invalid Vast instance id")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(fmt.Sprintf("/api/v0/instances/%d/", instanceID)), nil)
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
	return result.Instances, nil
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(fmt.Sprintf("/api/v0/instances/%d/ssh/", instanceID)), bytes.NewReader(body))
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
	return nil
}

func (c *Client) DestroyInstance(ctx context.Context, instanceID int64) error {
	if instanceID <= 0 {
		return errors.New("invalid Vast instance id")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint(fmt.Sprintf("/api/v0/instances/%d/", instanceID)), nil)
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
		case http.StatusGone:
			return errors.New("selected Vast offer is no longer available; rerun stint start")
		case http.StatusTooManyRequests:
			return errors.New("Vast API rate limit reached; retry later")
		}
	}
	return err
}
