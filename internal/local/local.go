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

	// Vast SSH authentication can be transient while an instance is settling.
	// More importantly, once one connection succeeds, keep that authenticated
	// transport alive and multiplex later lifecycle commands over it. This avoids
	// re-authenticating between runtime bootstrap, model launch, log checks, and
	// the long-lived Cline forward.
	//
	// Bootstrap commands are intentionally noisy (apt, git, cmake, compiler). When
	// the remote command contains the llama.cpp build, collapse that stream into a
	// single terminal-width-aware status line. The renderer only redraws when the
	// visible stage or percentage changes, never allows the line to wrap, and keeps
	// build progress monotonic even when parallel CMake output arrives out of order.
	wrapper := filepath.Join(os.TempDir(), "stint-ssh-retry")
	script := fmt.Sprintf(`#!/bin/sh
ssh_bin=%q

progress_mode=0
case "$*" in
  *"cmake --build"*) progress_mode=1 ;;
esac

run_ssh() {
  if [ "$progress_mode" -ne 1 ] || [ ! -t 1 ]; then
    "$ssh_bin" \
      -o IdentitiesOnly=yes \
      -o ControlMaster=auto \
      -o ControlPersist=15m \
      -o "ControlPath=${TMPDIR:-/tmp}/stint-ssh-%%C" \
      "$@"
    return $?
  fi

  log_file="$(mktemp "${TMPDIR:-/tmp}/stint-runtime.XXXXXX")" || return 1
  "$ssh_bin" \
    -o IdentitiesOnly=yes \
    -o ControlMaster=auto \
    -o ControlPersist=15m \
    -o "ControlPath=${TMPDIR:-/tmp}/stint-ssh-%%C" \
    "$@" >"$log_file" 2>&1 &
  ssh_pid=$!

  cols="$(tput cols 2>/dev/null || true)"
  case "$cols" in
    ''|*[!0-9]*) cols=100 ;;
  esac

  bar_width=20
  if [ "$cols" -lt 80 ]; then bar_width=12; fi
  if [ "$cols" -lt 60 ]; then bar_width=8; fi
  if [ "$cols" -lt 44 ]; then bar_width=4; fi

  max_pct=0
  last_render=""
  latest="Preparing remote runtime..."

  while kill -0 "$ssh_pid" 2>/dev/null; do
    next="$(tail -c 8192 "$log_file" 2>/dev/null | tr '\r' '\n' | sed '/^[[:space:]]*$/d' | tail -n 1 | sed 's/[[:cntrl:]]//g')"
    if [ -n "$next" ]; then
      latest="$next"
    fi

    raw_pct="$(printf '%%s\n' "$latest" | sed -n 's/.*\[ *\([0-9][0-9]*\)%%\].*/\1/p' | head -n 1)"
    if [ -n "$raw_pct" ] && [ "$raw_pct" -gt "$max_pct" ] 2>/dev/null; then
      max_pct="$raw_pct"
    fi

    if [ "$max_pct" -gt 0 ]; then
      pct="$max_pct"
      pct_label="$(printf '%%3d%%%%' "$pct")"
    else
      pct=0
      pct_label=" --%%"
    fi

    display="$latest"
    case "$latest" in
      Get:*|Hit:*|Reading\ package*|Reading\ database*|Unpacking*|Setting\ up*|Processing\ triggers*)
        display="Installing build dependencies"
        ;;
      *"Cloning into"*|*"Updating files:"*)
        display="Fetching llama.cpp source"
        ;;
      *"Configuring done"*|*"Generating done"*|*"Build files have been written"*)
        display="Configuring CUDA build"
        ;;
      *"UI: downloading"*)
        display="Preparing llama-server assets"
        ;;
      *"Building CUDA object"*)
        display="Compiling CUDA kernels"
        ;;
      *"Building CXX object"*)
        display="Compiling C++ runtime"
        ;;
      *"Building C object"*)
        display="Compiling C runtime"
        ;;
      *"Linking CXX executable"*)
        display="Linking llama-server"
        ;;
      *"Built target llama-server"*)
        display="llama-server built"
        ;;
    esac

    filled=$((pct * bar_width / 100))
    bar=""
    i=0
    while [ "$i" -lt "$bar_width" ]; do
      if [ "$i" -lt "$filled" ]; then
        bar="${bar}="
      else
        bar="${bar}-"
      fi
      i=$((i + 1))
    done

    prefix="$(printf '  Runtime [%%s] %%s  ' "$bar" "$pct_label")"
    prefix_len=${#prefix}
    msg_width=$((cols - prefix_len - 1))
    if [ "$msg_width" -lt 1 ]; then
      msg_width=1
    fi
    short="$(printf '%%s' "$display" | cut -c1-"$msg_width")"
    render="${prefix}${short}"

    if [ "$render" != "$last_render" ]; then
      printf '\r\033[2K%%s' "$render"
      last_render="$render"
    fi
    sleep 0.4
  done

  wait "$ssh_pid"
  status=$?
  latest="$(tail -c 8192 "$log_file" 2>/dev/null | tr '\r' '\n' | sed '/^[[:space:]]*$/d' | tail -n 1 | sed 's/[[:cntrl:]]//g')"

  if [ "$status" -eq 0 ]; then
    bar=""
    i=0
    while [ "$i" -lt "$bar_width" ]; do bar="${bar}="; i=$((i + 1)); done
    printf '\r\033[2K  Runtime [%%s] 100%%%%  llama-server ready\n' "$bar"
  else
    printf '\r\033[2K  Runtime FAILED  %%s\n' "$latest" >&2
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
