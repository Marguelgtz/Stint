package main

import (
	"fmt"

	"github.com/Marguelgtz/Stint/internal/core"
	"github.com/Marguelgtz/Stint/internal/provider/vast"
)

const (
	// Vast's official llama.cpp image is derived from the Vast base image, so it
	// keeps the SSH/container lifecycle Stint relies on while shipping a CUDA
	// llama-server prebuilt at /opt/llama.cpp/llama-server.
	llamaVastImage         = "vastai/llama-cpp:b10472-mix-4b653db-cuda-12.9"
	llamaVastMinCUDA       = 12.9
	llamaPrebuiltBinary    = "/opt/llama.cpp/llama-server"
	llamaRuntimeBridgePath = "/workspace/stint/llama.cpp/build/bin/llama-server"
	llamaModelFileName     = "Qwen3.8-27B-Q4_K_M.gguf"
	llamaModelSizeBytes    = int64(18962876416)
	llamaModelSHA256       = "31629f53165ab6a7dad8c9847dcfd1fdf55829dac1e6e748f4a68581b0033d34"
	llamaModelDownloadURL  = "https://huggingface.co/ggml-org/Qwen3.8-27B-GGUF/resolve/main/Qwen3.8-27B-Q4_K_M.gguf"

	// Experimental NInfer bundle deployment. Vast only needs to start its own
	// CUDA base image; the small pinned runtime bundle is fetched after SSH by a
	// self-installing bridge script. The existing NInfer bootstrap starts the
	// Qwen prefetch before validating this bridge, so runtime and model transfer
	// overlap instead of serializing.
	ninferVastImage            = vast.NInferCUDA128Image
	ninferRuntimeBridgePath    = "/workspace/stint/ninfer/build/apps/ninfer-serve"
	ninferRuntimeReleaseTag    = "ninfer-runtime-981b685e"
	ninferRuntimeArchive       = "stint-ninfer-981b685e-sm89-linux-amd64.tar.gz"
	ninferRuntimeInstallRoot   = "/workspace/stint/runtime/ninfer/981b685e"
	ninferRuntimeReleaseBase   = "https://github.com/Marguelgtz/Stint/releases/download/" + ninferRuntimeReleaseTag
	ninferRuntimeBundleURL     = ninferRuntimeReleaseBase + "/" + ninferRuntimeArchive
	ninferRuntimeBundleSHAURL  = ninferRuntimeBundleURL + ".sha256"

	// Some Vast hosts have been observed creating /root/.ssh/authorized_keys
	// with ownership or modes that OpenSSH StrictModes rejects. Keep a tiny
	// background repair loop alive through the provider startup window so late
	// key injection cannot leave an otherwise healthy paid instance unreachable.
	vastSSHPermissionsOnStart = `install -d -m 700 -o root -g root /root/.ssh; chmod 700 /root /root/.ssh 2>/dev/null || true; nohup sh -c 'i=0; while [ "$i" -lt 450 ]; do if [ -e /root/.ssh/authorized_keys ]; then chown root:root /root /root/.ssh /root/.ssh/authorized_keys 2>/dev/null || true; chmod 700 /root /root/.ssh 2>/dev/null || true; chmod 600 /root/.ssh/authorized_keys 2>/dev/null || true; fi; i=$((i+1)); sleep 2; done' >/tmp/stint-ssh-permissions.log 2>&1 &`

	// Keep the rest of the llama lifecycle unchanged while replacing the old
	// per-instance source build with Vast's prebuilt binary. The wrapper is
	// always executable, so a missing/broken /opt binary fails validation rather
	// than silently falling back to a source compile and hiding the A/B test.
	vastLlamaPrebuiltBridgeOnStart = `install -d -m 755 /workspace/stint/llama.cpp/build/bin; printf '%s\n' '#!/bin/sh' 'exec /opt/llama.cpp/llama-server "$@"' > /workspace/stint/llama.cpp/build/bin/llama-server; chmod 755 /workspace/stint/llama.cpp/build/bin/llama-server`
)

func prepareVastSearchForRuntime(profile core.Profile, options vast.SearchOptions, runtimeRequest string) (core.Profile, vast.SearchOptions) {
	// The official llama.cpp image is CUDA 12.9. The NInfer bundle is built and
	// smoke-tested against Vast's CUDA 12.8.1 Ubuntu 24.04 base. Reject
	// incompatible hosts before rental.
	options.MinCUDAMaxGood = llamaVastMinCUDA
	if runtimeRequest == runtimeNInfer {
		profile.GPU.PreferredModels = []string{"RTX 4090"}
		options.MinCUDAMaxGood = vast.NInferMinCUDAVersion
	}
	return profile, options
}

func vastImageForRuntime(runtime string) string {
	if runtime == runtimeNInfer {
		return ninferVastImage
	}
	return llamaVastImage
}

func vastOnStartForRuntime(runtime string) string {
	// Runtime/model preparation still happens only after Stint has proved SSH
	// responsiveness. The NInfer hook writes a self-installing bridge but does
	// not download the runtime bundle while Vast is in provider loading.
	switch runtime {
	case runtimeLlamaCpp:
		return vastSSHPermissionsOnStart + " " + vastLlamaPrebuiltBridgeOnStart
	case runtimeNInfer:
		return vastSSHPermissionsOnStart + "\n" + vastNInferBundleBridgeOnStart()
	default:
		return vastSSHPermissionsOnStart
	}
}

func vastNInferBundleBridgeOnStart() string {
	return fmt.Sprintf(`install -d -m 755 /workspace/stint/ninfer/build/apps /workspace/stint/ninfer
cat > %s <<'STINT_NINFER_WRAPPER'
#!/bin/sh
set -eu
runtime=%q
real="$runtime/bin/ninfer-serve"
commit=%q
archive_url=%q
sha_url=%q

if [ ! -x "$real" ] || [ "$(cat "$runtime/commit" 2>/dev/null || true)" != "$commit" ]; then
  parent="$(dirname "$runtime")"
  tmp="${runtime}.tmp.$$"
  rm -rf "$tmp"
  mkdir -p "$tmp" "$parent"
  python3 - "$archive_url" "$sha_url" "$tmp" <<'PY'
import hashlib
import pathlib
import shutil
import sys
import tarfile
import time
import urllib.request

archive_url, sha_url, tmp_arg = sys.argv[1:]
tmp = pathlib.Path(tmp_arg)
archive = tmp / "runtime.tar.gz"


def fetch(url, dest=None):
    last = None
    for attempt in range(1, 4):
        try:
            with urllib.request.urlopen(url, timeout=60) as response:
                if dest is None:
                    return response.read()
                with open(dest, "wb") as out:
                    shutil.copyfileobj(response, out, length=1024 * 1024)
                return None
        except Exception as exc:
            last = exc
            if attempt == 3:
                raise
            time.sleep(attempt * 2)
    raise last

expected = fetch(sha_url).decode("utf-8").strip().split()[0].lower()
if len(expected) != 64:
    raise RuntimeError(f"invalid NInfer bundle SHA256 sidecar: {expected!r}")
fetch(archive_url, archive)
actual = hashlib.sha256(archive.read_bytes()).hexdigest()
if actual != expected:
    raise RuntimeError(f"NInfer bundle checksum mismatch: got {actual}, want {expected}")

root = tmp.resolve()
with tarfile.open(archive, "r:gz") as tf:
    for member in tf.getmembers():
        target = (root / member.name).resolve()
        if target != root and root not in target.parents:
            raise RuntimeError(f"unsafe path in NInfer bundle: {member.name}")
        if member.issym() or member.islnk():
            raise RuntimeError(f"links are not allowed in NInfer bundle: {member.name}")
    tf.extractall(root)
archive.unlink()
PY
  test -x "$tmp/bundle/bin/ninfer-serve"
  test "$(cat "$tmp/bundle/commit")" = "$commit"
  rm -rf "$runtime"
  mv "$tmp/bundle" "$runtime"
  rm -rf "$tmp"
fi

export LD_LIBRARY_PATH="$runtime/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
exec "$real" "$@"
STINT_NINFER_WRAPPER
chmod 755 %s
printf '%%s\n' %s > /workspace/stint/ninfer/.stint-commit`,
		ninferRuntimeBridgePath,
		ninferRuntimeInstallRoot,
		ninferSourceCommit,
		ninferRuntimeBundleURL,
		ninferRuntimeBundleSHAURL,
		ninferRuntimeBridgePath,
		ninferSourceCommit,
	)
}
