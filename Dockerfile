FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
COPY static ./static
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/minfo

FROM debian:bookworm-slim
ARG TARGETARCH=amd64
ARG BDINFO_VERSION=0.8.0.1b
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    unzip \
    ffmpeg \
    mediainfo \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/minfo /usr/local/bin/minfo
RUN case "$TARGETARCH" in \
      amd64) bdinfo_arch="x64" ;; \
      arm64) bdinfo_arch="arm64" ;; \
      *) echo "unsupported TARGETARCH: $TARGETARCH" >&2; exit 1 ;; \
    esac \
    && curl -fsSL -o /tmp/bdinfo.zip "https://github.com/UniqProject/BDInfo/releases/download/v${BDINFO_VERSION}/BDInfo_v${BDINFO_VERSION}-linux-${bdinfo_arch}.zip" \
    && mkdir -p /opt/bdinfo \
    && unzip -q /tmp/bdinfo.zip -d /opt/bdinfo \
    && find /opt/bdinfo -maxdepth 4 -type f \( -iname 'BDInfo*' -o -iname 'bdinfo*' \) -exec chmod +x {} + \
    && rm -f /tmp/bdinfo.zip
COPY bdinfo.sh /usr/local/bin/bdinfo
RUN chmod +x /usr/local/bin/bdinfo
ENV BDINFO_BIN=/usr/local/bin/bdinfo
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/minfo"]
