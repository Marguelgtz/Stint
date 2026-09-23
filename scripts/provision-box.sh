#!/usr/bin/env bash
# Prepare and verify a fresh Stint Deep Work box before the supervisor starts.
# Invoke on the GPU instance after Stint has qualified NInfer:
#
#   STINT_TARGET_REPO=/var/lib/stint-onbox/repo \
#   STINT_MODEL_ID=qwen3.8-27b \
#   scripts/provision-box.sh
#
# Installs Hermes/Node when missing, installs a current Go toolchain when the
# target repository declares go.mod, and proves the actual Stint test surface
# runs before the detached coordinator is allowed to report RUNNING.
set -Eeuo pipefail
export PATH="/usr/local/go/bin:/usr/local/bin:$PATH:$HOME/.local/bin"

TARGET_REPO="${STINT_TARGET_REPO:-}"
MODEL_ID="${STINT_MODEL_ID:-qwen3.8-27b}"
PHASING_DIR="${PHASING_DIR:-/root/stint-phasing}"
GO_STAGE=""

fail() { echo "PROVISION_FAIL $*" >&2; exit 1; }
RPT() { echo "REPORT $*"; }
cleanup() { [ -z "$GO_STAGE" ] || rm -rf "$GO_STAGE"; }
trap cleanup EXIT

[ "$(id -u)" -eq 0 ] || fail "run provisioning as root"
command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v python3 >/dev/null 2>&1 || fail "python3 is required"
command -v git >/dev/null 2>&1 || fail "git is required"
command -v timeout >/dev/null 2>&1 || fail "timeout is required"
[ -n "$TARGET_REPO" ] && [ -d "$TARGET_REPO" ] || fail "STINT_TARGET_REPO must name the transferred repository"

RPT "os: $(. /etc/os-release 2>/dev/null; echo "${PRETTY_NAME:-unknown}") user=$(id -un)"

if ! command -v node >/dev/null 2>&1; then
  command -v apt-get >/dev/null 2>&1 || fail "node is missing and apt-get is unavailable; provision Node.js 22+ before launching Deep Work"
  RPT "installing Node.js 22.x from NodeSource"
  curl -fsSL https://deb.nodesource.com/setup_22.x -o /tmp/stint-nodesource-setup.sh
  bash /tmp/stint-nodesource-setup.sh >/tmp/stint-nodesource.log 2>&1 || fail "NodeSource setup failed: $(tail -5 /tmp/stint-nodesource.log)"
  DEBIAN_FRONTEND=noninteractive apt-get install -y nodejs >>/tmp/stint-nodesource.log 2>&1 || fail "Node.js install failed: $(tail -5 /tmp/stint-nodesource.log)"
fi

if ! command -v hermes >/dev/null 2>&1; then
  RPT "installing Hermes"
  curl -fsSL https://hermes-agent.nousresearch.com/install.sh -o /tmp/stint-hermes-install.sh
  bash /tmp/stint-hermes-install.sh >/tmp/stint-hermes-install.log 2>&1 || fail "Hermes install failed: $(tail -5 /tmp/stint-hermes-install.log)"
  export PATH="$PATH:$HOME/.local/bin"
fi

install_go_for_repo() {
  local go_mod="$TARGET_REPO/go.mod"
  [ -f "$go_mod" ] || return 0
  local required
  required="$(awk '$1 == "go" {print $2; exit}' "$go_mod")"
  [ -n "$required" ] || fail "$go_mod has no go directive"
  local installed=""
  if command -v go >/dev/null 2>&1; then
    installed="$(go version | awk '{sub(/^go/, "", $3); print $3}')"
  fi
  if python3 - "$installed" "$required" <<'PY'
import re, sys
installed, required = sys.argv[1:]
def parts(value):
    match = re.match(r"^(\d+)\.(\d+)(?:\.(\d+))?", value)
    return tuple(int(piece or 0) for piece in match.groups()) if match else (0, 0, 0)
sys.exit(0 if installed and parts(installed) >= parts(required) else 1)
PY
  then
    RPT "go_toolchain: existing go$installed satisfies go $required"
    return 0
  fi

  local arch
  case "$(uname -m)" in
    x86_64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) fail "cannot install Go for unsupported architecture $(uname -m)" ;;
  esac
  RPT "installing a current stable Go toolchain for repository requirement go $required"
  local downloads archive sha filename
  GO_STAGE="$(mktemp -d /tmp/stint-go-install.XXXXXX)"
  downloads="$(curl -fsSL 'https://go.dev/dl/?mode=json')" || fail "could not read official Go release metadata"
  mapfile -t artifact < <(GO_RELEASES_JSON="$downloads" python3 - "$arch" <<'PY'
import json, os, sys
arch = sys.argv[1]
releases = json.loads(os.environ["GO_RELEASES_JSON"])
for release in releases:
    if not release.get("stable"):
        continue
    for item in release.get("files", []):
        if item.get("os") == "linux" and item.get("arch") == arch and item.get("kind") == "archive" and item.get("filename", "").endswith(".tar.gz"):
            print(item["filename"])
            print(item["sha256"])
            raise SystemExit(0)
raise SystemExit("no stable Linux Go archive found")
PY
  )
  [ "${#artifact[@]}" -eq 2 ] || fail "official Go metadata did not contain a stable Linux/$arch archive"
  filename="${artifact[0]}"
  sha="${artifact[1]}"
  archive="$GO_STAGE/$filename"
  curl -fL "https://go.dev/dl/$filename" -o "$archive" || fail "Go toolchain download failed"
  printf '%s  %s\n' "$sha" "$archive" | sha256sum -c - || fail "Go archive checksum failed"
  tar -C "$GO_STAGE" -xzf "$archive" || fail "Go archive extraction failed"
  [ -x "$GO_STAGE/go/bin/go" ] || fail "Go archive did not contain go/bin/go"
  if [ -d /usr/local/go ]; then
    rm -rf /usr/local/go.previous
    mv /usr/local/go /usr/local/go.previous || fail "could not move the old Go installation"
  fi
  if mv "$GO_STAGE/go" /usr/local/go; then
    rm -rf /usr/local/go.previous
  else
    [ ! -d /usr/local/go.previous ] || mv /usr/local/go.previous /usr/local/go
    fail "could not install the verified Go toolchain"
  fi
  rm -rf "$GO_STAGE"
  GO_STAGE=""
  export PATH="/usr/local/go/bin:$PATH"
  go version || fail "installed Go toolchain did not start"
}
install_go_for_repo

# Fresh images have no committer identity. Checkpoint creation is part of task
# acceptance, so establish the dedicated deterministic identity before work.
git config --global user.name "Stint Deep Work"
git config --global user.email "deepwork@stint.local"

for executable in git python3 node npm npx hermes timeout curl; do
  command -v "$executable" >/dev/null 2>&1 || fail "required Deep Work executable is unavailable: $executable"
done

if [ -f "$TARGET_REPO/go.mod" ]; then
  go version || fail "Go is unavailable for the target repository"
  if grep -Eq '^module[[:space:]]+github\.com/Marguelgtz/Stint([[:space:]]|$)' "$TARGET_REPO/go.mod"; then
    RPT "running Stint verification surface before Deep Work RUNNING"
    (cd "$TARGET_REPO" && timeout "${STINT_BOOTSTRAP_TEST_TIMEOUT:-20m}" go test ./...) \
      || fail "Stint go test ./... failed before Deep Work startup"
  fi
fi

ninfer_pid="$(pgrep -xo ninfer-serve 2>/dev/null || true)"
[ -n "$ninfer_pid" ] || fail "NInfer process is not running"
ninfer_cmd="$(tr '\0' ' ' <"/proc/$ninfer_pid/cmdline" 2>/dev/null || true)"
for flag in '--max-context 262144' '--kv-capacity 262144' '--default-max-tokens 262144'; do
  printf '%s' "$ninfer_cmd" | grep -Fq -- "$flag" || fail "NInfer launch is missing $flag"
done
models="$(curl -fsS -m 5 http://127.0.0.1:8080/v1/models)" || fail "NInfer model endpoint is not healthy"
printf '%s' "$models" | STINT_MODEL_ID="$MODEL_ID" python3 -c 'import json,os,sys; data=json.load(sys.stdin); ids=[item.get("id", "") for item in data.get("data", [])]; sys.exit(0 if os.environ["STINT_MODEL_ID"] in ids else 1)' \
  || fail "NInfer endpoint does not serve model $MODEL_ID"

RPT "git_identity: $(git config --global user.name) <$(git config --global user.email)>"
RPT "node: $(node --version) npm=$(npm --version)"
RPT "hermes: $(hermes --version 2>&1 | head -1)"
RPT "ninfer: pid=$ninfer_pid model=$MODEL_ID"
RPT "runtime requirements passed; next install phase proxy/providers and run the Hermes route smoke"
