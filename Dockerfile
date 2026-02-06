ARG MONO_TAG=5.12.0.226
ARG RUNTIME_MONO_TAG=latest

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

FROM debian:bookworm-slim AS bdinfo-src
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 https://github.com/zoffline/BDInfoCLI-ng.git /tmp/bdinfo
RUN curl -fsSL -o /tmp/nuget.exe https://dist.nuget.org/win-x86-commandline/latest/nuget.exe

FROM mono:${MONO_TAG} AS bdinfo-build
COPY --from=bdinfo-src /etc/ssl/certs /etc/ssl/certs
COPY --from=bdinfo-src /tmp/bdinfo /tmp/bdinfo
COPY --from=bdinfo-src /tmp/nuget.exe /tmp/nuget.exe
WORKDIR /tmp/bdinfo
RUN mono /tmp/nuget.exe restore BDInfo.sln
RUN if command -v msbuild >/dev/null 2>&1; then msbuild BDInfo.sln /p:Configuration=Release; else xbuild /p:Configuration=Release BDInfo.sln; fi
RUN set -eux; \
    bdinfo_exe="$(find /tmp/bdinfo -type f -name 'BDInfo.exe' -path '*bin/Release*' | head -n 1)"; \
    if [ -z "$bdinfo_exe" ]; then echo "BDInfo.exe not found"; exit 1; fi; \
    bdinfo_dir="$(dirname "$bdinfo_exe")"; \
    mkdir -p /opt/bdinfo; \
    cp -r "$bdinfo_dir"/. /opt/bdinfo/

FROM mono:${RUNTIME_MONO_TAG}
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    ffmpeg \
    mediainfo \
    libgdiplus \
    util-linux \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/minfo /usr/local/bin/minfo
COPY --from=bdinfo-build /opt/bdinfo /opt/bdinfo
COPY bdinfo.sh /usr/local/bin/bdinfo
RUN chmod +x /usr/local/bin/bdinfo
ENV BDINFO_BIN=/usr/local/bin/bdinfo
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/minfo"]
