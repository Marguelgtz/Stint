# syntax=docker/dockerfile:1.7

ARG VAST_BASE=vastai/base-image:cuda-12.8.1-cudnn-devel-ubuntu24.04-py310
ARG NINFER_REPOSITORY=https://github.com/sergiuszm/ninfer-4090.git
ARG NINFER_COMMIT=981b685ea2124fdaed023123d2e63fd29d529ab8

FROM ${VAST_BASE} AS build

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
        libavcodec-dev \
        libavformat-dev \
        libavutil-dev \
        libcurl4-openssl-dev \
        libswscale-dev \
        ninja-build \
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

FROM ${VAST_BASE}

ARG NINFER_COMMIT
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        libavcodec60 \
        libavformat60 \
        libavutil58 \
        libcurl4t64 \
        libswscale7 \
    && rm -rf /var/lib/apt/lists/*

RUN install -d -m 755 /opt/ninfer/bin
COPY --from=build /build/apps/ninfer /opt/ninfer/bin/ninfer
COPY --from=build /build/apps/ninfer-serve /opt/ninfer/bin/ninfer-serve
RUN printf '%s\n' "${NINFER_COMMIT}" > /opt/ninfer/commit \
    && ldd /opt/ninfer/bin/ninfer-serve > /tmp/ninfer-ldd.txt \
    && ! grep -q 'not found' /tmp/ninfer-ldd.txt \
    && /opt/ninfer/bin/ninfer-serve --help >/dev/null \
    && rm -f /tmp/ninfer-ldd.txt

ENV PATH="/opt/ninfer/bin:${PATH}"

LABEL org.opencontainers.image.source="https://github.com/Marguelgtz/Stint" \
      org.opencontainers.image.description="Vast-compatible prebuilt NInfer runtime for Stint" \
      org.opencontainers.image.licenses="Apache-2.0" \
      io.stint.ninfer.commit="${NINFER_COMMIT}"

WORKDIR /workspace
