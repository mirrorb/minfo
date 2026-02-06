FROM node:20-bookworm-slim AS webui
WORKDIR /app
COPY webui/package.json ./
RUN npm install
COPY webui .
RUN npm run build

FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY --from=webui /app/dist ./webui/dist
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/minfo

FROM debian:bookworm-slim AS mono-build
ARG MONO_REF=main
ARG LIBGDIPLUS_REF=master
RUN apt-get update && apt-get install -y --no-install-recommends \
    autoconf \
    automake \
    bison \
    build-essential \
    ca-certificates \
    cmake \
    curl \
    gettext \
    git \
    libbsd-dev \
    libcairo2-dev \
    libcurl4-openssl-dev \
    libexif-dev \
    libffi-dev \
    libfontconfig1-dev \
    libfreetype6-dev \
    libgif-dev \
    libglib2.0-dev \
    libjpeg-dev \
    libkrb5-dev \
    liblzma-dev \
    libpng-dev \
    libssl-dev \
    libtiff-dev \
    libtool \
    libx11-dev \
    libxext-dev \
    libxml2-dev \
    libxrender-dev \
    ninja-build \
    pkg-config \
    python3 \
    zlib1g-dev \
    && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 --branch "$MONO_REF" https://github.com/mono/mono.git /tmp/mono
WORKDIR /tmp/mono
RUN ./autogen.sh --prefix=/opt/mono --disable-nls
RUN make get-monolite-latest
RUN make -j"$(nproc)"
RUN make install
RUN git clone --depth 1 --branch "$LIBGDIPLUS_REF" https://github.com/mono/libgdiplus.git /tmp/libgdiplus
WORKDIR /tmp/libgdiplus
RUN ./autogen.sh --prefix=/opt/mono
RUN make -j"$(nproc)"
RUN make install

FROM mono-build AS bdinfo-build
ENV PATH="/opt/mono/bin:$PATH"
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    git \
    curl \
    && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 https://github.com/zoffline/BDInfoCLI-ng.git /tmp/bdinfo
WORKDIR /tmp/bdinfo
RUN curl -fsSL -o /tmp/nuget.exe https://dist.nuget.org/win-x86-commandline/latest/nuget.exe
RUN mono /tmp/nuget.exe restore BDInfo.sln
RUN if command -v msbuild >/dev/null 2>&1; then msbuild BDInfo.sln /p:Configuration=Release; else xbuild /p:Configuration=Release BDInfo.sln; fi
RUN set -eux; \
    bdinfo_exe="$(find /tmp/bdinfo -type f -name 'BDInfo.exe' -path '*bin/Release*' | head -n 1)"; \
    if [ -z "$bdinfo_exe" ]; then echo "BDInfo.exe not found"; exit 1; fi; \
    bdinfo_dir="$(dirname "$bdinfo_exe")"; \
    mkdir -p /opt/bdinfo; \
    cp -r "$bdinfo_dir"/. /opt/bdinfo/

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    ffmpeg \
    mediainfo \
    libbsd0 \
    libcairo2 \
    libcurl4 \
    libexif12 \
    libffi8 \
    libfontconfig1 \
    libfreetype6 \
    libgif7 \
    libglib2.0-0 \
    libjpeg62-turbo \
    libkrb5-3 \
    liblzma5 \
    libpng16-16 \
    libssl3 \
    libtiff6 \
    libx11-6 \
    libxext6 \
    libxml2 \
    libxrender1 \
    util-linux \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/minfo /usr/local/bin/minfo
COPY --from=mono-build /opt/mono /opt/mono
COPY --from=bdinfo-build /opt/bdinfo /opt/bdinfo
COPY bdinfo.sh /usr/local/bin/bdinfo
RUN chmod +x /usr/local/bin/bdinfo
ENV PATH="/opt/mono/bin:$PATH"
ENV LD_LIBRARY_PATH="/opt/mono/lib:$LD_LIBRARY_PATH"
ENV BDINFO_BIN=/usr/local/bin/bdinfo
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/minfo"]
