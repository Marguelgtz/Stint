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
	// Keep the retry policy below the Go lifecycle so every SSH call benefits.
	//
	// Bootstrap commands are intentionally noisy (apt, git, cmake, compiler). When
	// the remote command contains the llama.cpp build, collapse that stream into a
	// single terminal line showing the latest message and any CMake percentage.
	wrapper := filepath.Join(os.TempDir(), "stint-ssh-retry")
	script := fmt.Sprintf(`#!/bin/sh
ssh_bin=%q

progress_mode=0
case "$*" in
  *"cmake --build"*) progress_mode=1 ;;
esac

run_ssh() {
  if [ "$progress_mode" -ne 1 ] || [ ! -t 1 ]; then
    "$ssh_bin" -o IdentitiesOnly=yes "$@"
    return $?
  fi

  log_file="$(mktemp "${TMPDIR:-/tmp}/stint-runtime.XXXXXX")" || return 1
  "$ssh_bin" -o IdentitiesOnly=yes "$@" >"$log_file" 2>&1 &
  ssh_pid=$!

  latest="Preparing remote runtime..."
  while kill -0 "$ssh_pid" 2>/dev/null; do
    next="$(tail -n 1 "$log_file" 2>/dev/null | tr '\r\n' ' ' | cut -c1-92)"
    if [ -n "$next" ]; then
      latest="$next"
    fi

    pct="$(printf '%%s\n' "$latest" | sed -n 's/.*\[ *\([0-9][0-9]*\)%%\].*/\1/p' | head -n 1)"
    if [ -z "$pct" ]; then
      pct=0
      pct_label=" --%%"
    else
      pct_label="$(printf '%%3d%%%%' "$pct")"
    fi

    width=24
    filled=$((pct * width / 100))
    bar=""
    i=0
    while [ "$i" -lt "$width" ]; do
      if [ "$i" -lt "$filled" ]; then
        bar="${bar}="
      else
        bar="${bar}-"
      fi
      i=$((i + 1))
    done

    printf '\r\033[2K  Runtime [%%s] %%s  %%s' "$bar" "$pct_label" "$latest"
    sleep 0.25
  done

  wait "$ssh_pid"
  status=$?
  latest="$(tail -n 1 "$log_file" 2>/dev/null | tr '\r\n' ' ' | cut -c1-92)"

  if [ "$status" -eq 0 ]; then
    printf '\r\033[2K  Runtime [========================] 100%%%%  llama-server ready\n'
  else
    printf '\r\033[2K  Runtime [------------------------] FAILED  %%s\n' "$latest" >&2
    tail -n 20 "$log_file" >&2
  fi
  rm -f "$log_file"
  return "$status"
}

attempt=1
while :; do
  run_ssh "$@"
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
