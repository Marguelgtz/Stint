# syntax=docker/dockerfile:1.7

ARG VAST_BASE=vastai/base-image:cuda-12.8.1-cudnn-devel-ubuntu24.04-py310
ARG NINFER_REPOSITORY=https://github.com/sergiuszm/ninfer-4090.git
ARG NINFER_COMMIT=981b685ea2124fdaed023123d2e63fd29d529ab8

FROM ${VAST_BASE} AS build

ARG VAST_BASE
ARG NINFER_REPOSITORY
ARG NINFER_COMMIT
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        cmake \
        gcc-13 \
        g++-13 \
        git \
        gzip \
        libavcodec-dev \
        libavformat-dev \
        libavutil-dev \
        libcurl4-openssl-dev \
        libswscale-dev \
        ninja-build \
        pax-utils \
        pkg-config \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src/ninfer
RUN git init -q \
    && git remote add origin "${NINFER_REPOSITORY}" \
    && git fetch -q --depth 1 origin "${NINFER_COMMIT}" \
    && git checkout -q --detach FETCH_HEAD

RUN CC=/usr/bin/gcc-13 \
    CXX=/usr/bin/g++-13 \
    CUDACXX="$(command -v nvcc)" \
    CUDAHOSTCXX=/usr/bin/g++-13 \
    cmake -S . -B /build -G Ninja \
      -DCMAKE_BUILD_TYPE=Release \
      -DCMAKE_CUDA_ARCHITECTURES=89 \
      -DNINFER_BUILD_APPS=ON \
      -DBUILD_TESTING=OFF \
      -DNINFER_BUILD_BENCHMARKS=OFF \
    && cmake --build /build --parallel 2 --target ninfer ninfer-serve \
    && /build/apps/ninfer-serve --help >/dev/null

# Produce a relocatable runtime directory. CUDA/driver libraries and glibc stay
# provided by the target Vast base image; non-core ELF dependencies are copied
# beside the binaries and resolved with LD_LIBRARY_PATH at runtime.
RUN set -eux; \
    install -d -m 755 /bundle/bin /bundle/lib /out; \
    install -m 755 /build/apps/ninfer /bundle/bin/ninfer; \
    install -m 755 /build/apps/ninfer-serve /bundle/bin/ninfer-serve; \
    { lddtree -l /build/apps/ninfer; lddtree -l /build/apps/ninfer-serve; } \
      | sort -u \
      | while IFS= read -r lib; do \
          case "$lib" in \
            /usr/local/cuda/*|/usr/local/nvidia/*|*/ld-linux-*.so*|*/libc.so.*|*/libm.so.*|*/libdl.so.*|*/librt.so.*|*/libpthread.so.*) continue ;; \
          esac; \
          if [ -f "$lib" ]; then cp -L "$lib" "/bundle/lib/$(basename "$lib")"; fi; \
        done; \
    printf '%s\n' "${NINFER_COMMIT}" > /bundle/commit; \
    printf '{\n  "format": 1,\n  "ninferCommit": "%s",\n  "cudaArch": "89",\n  "platform": "linux-amd64",\n  "baseImage": "%s",\n  "entrypoint": "bin/ninfer-serve"\n}\n' \
      "$NINFER_COMMIT" "$VAST_BASE" > /bundle/manifest.json; \
    short="$(printf '%s' "$NINFER_COMMIT" | cut -c1-8)"; \
    archive="stint-ninfer-${short}-sm89-linux-amd64.tar.gz"; \
    tar --sort=name --mtime='UTC 2026-01-01' --owner=0 --group=0 --numeric-owner -C / -cf - bundle \
      | gzip -n > "/out/${archive}"; \
    (cd /out && sha256sum "$archive" > "$archive.sha256"); \
    du -h "/out/${archive}"; \
    cat "/out/${archive}.sha256"

# This is the proof gate: the bundle must run on the pristine Vast base with no
# apt-get, source checkout, or compilation in the target stage.
FROM ${VAST_BASE} AS smoke
COPY --from=build /bundle /opt/stint-ninfer
ENV LD_LIBRARY_PATH=/opt/stint-ninfer/lib
RUN ! ldd /opt/stint-ninfer/bin/ninfer-serve | grep -q 'not found' \
    && /opt/stint-ninfer/bin/ninfer-serve --help >/dev/null \
    && test "$(cat /opt/stint-ninfer/commit)" = "981b685ea2124fdaed023123d2e63fd29d529ab8"

# Test the actual compressed artifact as Stint would consume it after SSH.
FROM ${VAST_BASE} AS archive-smoke
COPY --from=build /out /tmp/runtime-artifacts
RUN set -eux; \
    archive="$(find /tmp/runtime-artifacts -maxdepth 1 -name '*.tar.gz' -print -quit)"; \
    mkdir -p /opt; \
    tar -C /opt -xzf "$archive"; \
    export LD_LIBRARY_PATH=/opt/bundle/lib; \
    ! ldd /opt/bundle/bin/ninfer-serve | grep -q 'not found'; \
    /opt/bundle/bin/ninfer-serve --help >/dev/null; \
    test "$(cat /opt/bundle/commit)" = "981b685ea2124fdaed023123d2e63fd29d529ab8"

FROM scratch AS bundle-artifact
COPY --from=build /out/ /
