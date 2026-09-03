#!/usr/bin/env bash
# P2 box provisioning for Deep Work --worker hermes (Vanta CP1).
# Run OVER SSH with the Stint key, on the compute box:
#
#   ssh -i <stint-key> -p <port> -o BatchMode=yes root@<host> 'bash -s' < scripts/provision-box.sh
#
# Idempotent: only installs what is missing. Prints a REPORT block (grep it).
# Requires: root, outbound network (npm/PyPI/pip reachable) — P0 confirmed for
# the Vast NInfer offer class used for the first run.
set -u
RPT() { echo "REPORT $*"; }
FAILS=0
chk() { # chk <name> <cmd...>
  local n="$1"; shift
  if out=$("$@" 2>&1); then RPT "OK $n :: $out"
  else RPT "MISS $n ($out)"; FAILS=$((FAILS+1)); fi
}

RPT "os: $(. /etc/os-release 2>/dev/null; echo "$PRETTY_NAME") user=$(id -un) net=$(curl -s -m5 -o /dev/null -w '%{http_code}' https://registry.npmjs.org/ || echo fail)"
chk git     command -v git
chk py      command -v python3
chk node    command -v node
chk hermes  command -v hermes
chk npx     command -v npx
chk timeout command -v timeout
RPT "model_endpoint: $(curl -s -m5 http://127.0.0.1:8080/v1/models | head -c 300)"
RPT "py_crypto: $(python3 -c 'import cryptography; print("cryptography", cryptography.__version__)' 2>&1 | head -1)"
command -v node >/dev/null 2>&1 && RPT "node_ed25519: $(node -e 'require("crypto").generateKeyPairSync("ed25519");console.log("node-ed25519 OK")' 2>&1 | head -1)"

# --- install node (only if missing; box has outbound net) ---
if ! command -v node >/dev/null 2>&1; then
  RPT "installing node (nodesource 22.x)..."
  (curl -fsSL https://deb.nodesource.com/setup_22.x | bash - >/tmp/nodesource.log 2>&1 && \
   apt-get install -y nodejs >>/tmp/nodesource.log 2>&1) || RPT "node_install_failed: $(tail -3 /tmp/nodesource.log)"
  chk node command -v node
fi
command -v node >/dev/null 2>&1 && RPT "node_ver: $(node --version)"

# --- install hermes (only if missing) ---
if ! command -v hermes >/dev/null 2>&1; then
  RPT "installing hermes..."
  (curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash >/tmp/hermes_install.log 2>&1) || RPT "hermes_install_failed: $(tail -3 /tmp/hermes_install.log)"
  command -v hermes >/dev/null 2>&1 || PATH="$PATH:$HOME/.local/bin"
  export PATH="$PATH:$HOME/.local/bin"
  chk hermes command -v hermes
fi
command -v hermes >/dev/null 2>&1 && RPT "hermes_ver: $(hermes --version 2>&1 | head -1)"

RPT "done fails=$FAILS"