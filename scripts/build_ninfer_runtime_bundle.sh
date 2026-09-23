#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_repository="https://github.com/sergiuszm/ninfer-4090.git"
source_commit="81b68a20a9a0d9ab47d7e5838887c6d636ab76e0"
image_ref="vastai/base-image@sha256:bf6bb047dbc1105c89a5ac41b9a32205a2f2e022cb24d632d055c5b14a86f7ec"
output_dir="${1:-$repo_root/dist/ninfer-runtime}"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/stint-ninfer-build.XXXXXX")"
trap 'rm -rf "$work_dir"' EXIT
mkdir -p "$output_dir" "$work_dir/out"

if find "$output_dir" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
  echo "output directory must be empty: $output_dir" >&2
  exit 1
fi

python3 - "$repo_root/cmd/stint/runtime.go" <<'PY'
import re
import sys

source = open(sys.argv[1], encoding="utf-8").read()
match = re.search(r'ninferSourceCommit\s*=\s*"([0-9a-f]{40})"', source)
if not match or match.group(1) != "81b68a20a9a0d9ab47d7e5838887c6d636ab76e0":
    raise SystemExit("Stint's source pin and bundle builder's candidate SHA disagree")
PY

git -C "$work_dir" init -q source
git -C "$work_dir/source" remote add origin "$source_repository"
git -C "$work_dir/source" fetch -q --depth 1 origin "$source_commit"
git -C "$work_dir/source" checkout -q --detach FETCH_HEAD
test "$(git -C "$work_dir/source" rev-parse HEAD)" = "$source_commit"

docker pull "$image_ref"
docker run --rm --pull=never \
  --entrypoint /bin/bash \
  --mount "type=bind,src=$work_dir,dst=/work" \
  "$image_ref" --noprofile --norc -euo pipefail -c '
set -x
src=/work/source
build=/work/build
test "$(git -C "$src" rev-parse HEAD)" = "'"$source_commit"'"
CC=/usr/bin/gcc-13 \
CXX=/usr/bin/g++-13 \
CUDACXX="$(command -v nvcc)" \
CUDAHOSTCXX=/usr/bin/g++-13 \
cmake -S "$src" -B "$build" -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_CUDA_ARCHITECTURES=89 \
  -DNINFER_BUILD_APPS=ON \
  -DBUILD_TESTING=OFF \
  -DNINFER_BUILD_BENCHMARKS=OFF
cmake --build "$build" --parallel 4 --target ninfer ninfer-serve
test -x "$build/apps/ninfer-serve"
"$build/apps/ninfer-serve" --help >/dev/null
cp "$build/apps/ninfer-serve" /work/out/ninfer-serve
'

python3 "$repo_root/scripts/ninfer_runtime_bundle.py" package \
  --binary "$work_dir/out/ninfer-serve" \
  --output-dir "$output_dir"

for archive in "$output_dir"/*.tar.gz; do
  [ -e "$archive" ] || { echo "runtime archive was not created" >&2; exit 1; }
  prefix="${archive%.tar.gz}"
  checksum="$archive.sha256"
  manifest="${prefix}.manifest.json"
  python3 "$repo_root/scripts/ninfer_runtime_bundle.py" verify \
    --archive "$archive" --manifest "$manifest" --checksum "$checksum"
  clean_extract="$work_dir/clean-extract"
  mkdir -m 700 "$clean_extract"
  docker run --rm --pull=never --entrypoint /bin/bash \
    --mount "type=bind,src=$repo_root,dst=/repo,readonly" \
    --mount "type=bind,src=$output_dir,dst=/bundle,readonly" \
    --mount "type=bind,src=$clean_extract,dst=/extract" \
    "$image_ref" --noprofile --norc -euo pipefail -c '
python3 /repo/scripts/ninfer_runtime_bundle.py extract \
  --archive "/bundle/'"$(basename "$archive")"'" \
  --manifest "/bundle/'"$(basename "$manifest")"'" \
  --checksum "/bundle/'"$(basename "$checksum")"'" \
  --destination /extract
test -x /extract/ninfer-serve
"/extract/ninfer-serve" --help >/dev/null
if ldd /extract/ninfer-serve | grep -q "not found"; then
  ldd /extract/ninfer-serve >&2
  exit 1
fi
'
done
