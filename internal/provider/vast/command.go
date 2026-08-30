package vast

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const repairSSHPermissionsCommand = `install -d -m 700 -o root -g root /root/.ssh; chown root:root /root /root/.ssh 2>/dev/null || true; chmod 700 /root /root/.ssh 2>/dev/null || true; if [ -e /root/.ssh/authorized_keys ]; then chown root:root /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys; fi; stat -c '%U:%G %a %n' /root /root/.ssh /root/.ssh/authorized_keys 2>/dev/null || true`

type instanceCommandResponse struct {
	ResultURL string `json:"result_url"`
}

// RepairSSHPermissions uses Vast's instance command channel, which does not
// depend on a working SSH login, to normalize the files OpenSSH StrictModes
// checks before accepting root public-key authentication.
func (c *Client) RepairSSHPermissions(ctx context.Context, instanceID int64) error {
	if instanceID <= 0 {
		return errors.New("invalid Vast instance id")
	}
	if _, err := c.ExecuteInstanceCommand(ctx, instanceID, repairSSHPermissionsCommand); err != nil {
		return fmt.Errorf("repair Vast SSH permissions: %w", err)
	}
	return nil
}

func (c *Client) ExecuteInstanceCommand(ctx context.Context, instanceID int64, command string) (string, error) {
	if instanceID <= 0 {
		return "", errors.New("invalid Vast instance id")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return "", errors.New("Vast instance command is empty")
	}
	body, err := json.Marshal(map[string]any{"command": command})
	if err != nil {
		return "", fmt.Errorf("encode Vast instance command: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.endpoint(fmt.Sprintf("/api/v0/instances/command/%d/", instanceID)), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build Vast instance command request: %w", err)
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("execute Vast instance command: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", classifyInstanceWriteError(decodeAPIError(resp))
	}
	var result instanceCommandResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", fmt.Errorf("decode Vast instance command response: %w", err)
	}
	if strings.TrimSpace(result.ResultURL) == "" {
		return "", nil
	}
	return c.waitForInstanceCommandResult(ctx, result.ResultURL, 15*time.Second)
}

func (c *Client) waitForInstanceCommandResult(ctx context.Context, resultURL string, timeout time.Duration) (string, error) {
	resultURL = strings.TrimSpace(resultURL)
	if strings.HasPrefix(resultURL, "/") {
		resultURL = c.endpoint(resultURL)
	}
	deadline := time.Now().Add(timeout)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, resultURL, nil)
		if err != nil {
			return "", fmt.Errorf("build Vast command result request: %w", err)
		}
		resp, err := c.httpClient().Do(req)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
			if readErr != nil {
				return "", fmt.Errorf("read Vast command result: %w", readErr)
			}
			if resp.StatusCode == http.StatusOK {
				return strings.TrimSpace(string(body)), nil
			}
			if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
				return "", fmt.Errorf("Vast command result returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
			}
		}
		if time.Now().After(deadline) {
			return "", errors.New("timed out waiting for Vast instance command result")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}
