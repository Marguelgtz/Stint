package local

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Marguelgtz/Stint/internal/config"
)

func SSHExecutable() (string, error) {
	path, err := exec.LookPath("ssh")
	if err != nil {
		return "", errors.New("OpenSSH client not found in PATH")
	}
	if runtime.GOOS == "windows" {
		return path, nil
	}

	// Vast explicitly notes that SSH authentication can be transient while an
	// instance is settling. A short-lived 255 should not tear down paid compute,
	// especially between bootstrap, model launch, and the long-lived tunnel.
	// Keep the retry policy below the Go lifecycle so every SSH call benefits,
	// including the detached -N tunnel if its first connection is rejected.
	wrapper := filepath.Join(os.TempDir(), "stint-ssh-retry")
	script := fmt.Sprintf(`#!/bin/sh
attempt=1
while :; do
  %q -o IdentitiesOnly=yes "$@"
  status=$?
  if [ "$status" -ne 255 ] || [ "$attempt" -ge 6 ]; then
    exit "$status"
  fi
  sleep "$((attempt * 2))"
  attempt="$((attempt + 1))"
done
`, path)
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		return "", fmt.Errorf("prepare resilient SSH wrapper: %w", err)
	}
	if err := os.Chmod(wrapper, 0o700); err != nil {
		return "", fmt.Errorf("secure resilient SSH wrapper: %w", err)
	}
	return wrapper, nil
}

func EnsureSSHKey(paths config.Paths) (publicKey string, created bool, err error) {
	if err := paths.Ensure(); err != nil {
		return "", false, err
	}
	privateExists := fileExists(paths.SSHPrivateKey)
	publicExists := fileExists(paths.SSHPublicKey)
	if privateExists != publicExists {
		return "", false, errors.New("Stint SSH keypair is incomplete; remove the partial keypair and run setup again")
	}
	if !privateExists {
		sshKeygen, lookupErr := exec.LookPath("ssh-keygen")
		if lookupErr != nil {
			return "", false, errors.New("ssh-keygen not found in PATH")
		}
		cmd := exec.Command(sshKeygen, "-q", "-t", "ed25519", "-N", "", "-C", "stint", "-f", paths.SSHPrivateKey)
		if output, runErr := cmd.CombinedOutput(); runErr != nil {
			return "", false, fmt.Errorf("generate Stint SSH key: %w: %s", runErr, strings.TrimSpace(string(output)))
		}
		created = true
	}
	if err := os.Chmod(paths.SSHPrivateKey, 0o600); err != nil {
		return "", created, fmt.Errorf("secure private key: %w", err)
	}
	if err := os.Chmod(paths.SSHPublicKey, 0o644); err != nil {
		return "", created, fmt.Errorf("secure public key: %w", err)
	}
	data, err := os.ReadFile(paths.SSHPublicKey)
	if err != nil {
		return "", created, fmt.Errorf("read public key: %w", err)
	}
	return strings.TrimSpace(string(data)), created, nil
}

func ReadSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	info, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	isTTY := info.Mode()&os.ModeCharDevice != 0
	if isTTY {
		stty, lookupErr := exec.LookPath("stty")
		if lookupErr != nil {
			return "", errors.New("stty is required for hidden credential input; alternatively set VAST_API_KEY and use --from-env")
		}
		disable := exec.Command(stty, "-echo")
		disable.Stdin = os.Stdin
		if err := disable.Run(); err != nil {
			return "", fmt.Errorf("disable terminal echo: %w", err)
		}
		defer func() {
			restore := exec.Command(stty, "echo")
			restore.Stdin = os.Stdin
			_ = restore.Run()
			fmt.Fprintln(os.Stderr)
		}()
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	secret := strings.TrimSpace(line)
	if secret == "" {
		return "", errors.New("empty API key")
	}
	return secret, nil
}

func PortAvailable(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func SSHKeyExists(paths config.Paths) bool {
	return fileExists(paths.SSHPrivateKey) && fileExists(paths.SSHPublicKey)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
