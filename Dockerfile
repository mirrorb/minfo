ARG RUNTIME_MONO_TAG=latest
ARG BDINFO_IMAGE=zoffline/bdinfocli-ng
ARG BDINFO_DIR=/opt/bdinfo

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

FROM ${BDINFO_IMAGE} AS bdinfo-prebuilt
ARG BDINFO_DIR
RUN set -eux; \
    if [ -d "$BDINFO_DIR" ]; then \
        mkdir -p /opt/bdinfo; \
        cp -r "$BDINFO_DIR"/. /opt/bdinfo/; \
    else \
        if ! command -v find >/dev/null 2>&1; then \
            echo "find not available; set BDINFO_DIR to the BDInfo directory inside the image" >&2; \
            exit 1; \
        fi; \
        bdinfo_exe="$(find / -type f -name 'BDInfo.exe' 2>/dev/null | head -n 1)"; \
        if [ -z "$bdinfo_exe" ]; then echo "BDInfo.exe not found; set BDINFO_DIR" >&2; exit 1; fi; \
        bdinfo_dir="$(dirname "$bdinfo_exe")"; \
        bdinfo_root="$bdinfo_dir"; \
        while [ "$bdinfo_root" != "/" ]; do \
            if [ -f "$bdinfo_root/BDInfo.sln" ] || [ -d "$bdinfo_root/packages" ]; then \
                break; \
            fi; \
            bdinfo_root="$(dirname "$bdinfo_root")"; \
        done; \
        if [ "$bdinfo_root" = "/" ]; then \
            bdinfo_root="$bdinfo_dir"; \
        fi; \
        mkdir -p /opt/bdinfo; \
        cp -r "$bdinfo_root"/. /opt/bdinfo/; \
    fi

FROM mono:${RUNTIME_MONO_TAG}
RUN set -eux; \
    if [ -f /etc/apt/sources.list.d/mono-official-stable.list ]; then rm -f /etc/apt/sources.list.d/mono-official-stable.list; fi; \
    if grep -qE 'jessie|stretch|buster' /etc/os-release; then \
        echo 'Acquire::Check-Valid-Until "false";' > /etc/apt/apt.conf.d/99no-check-valid-until; \
        sed -i 's|http://deb.debian.org/debian|http://archive.debian.org/debian|g' /etc/apt/sources.list; \
        sed -i 's|http://security.debian.org/debian-security|http://archive.debian.org/debian-security|g' /etc/apt/sources.list; \
        sed -i 's|http://deb.debian.org/debian-security|http://archive.debian.org/debian-security|g' /etc/apt/sources.list; \
        sed -i '/-updates/d' /etc/apt/sources.list; \
    fi; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        ffmpeg \
        mediainfo \
        libgdiplus \
        util-linux; \
    rm -rf /var/lib/apt/lists/*
COPY --from=build /out/minfo /usr/local/bin/minfo
COPY --from=bdinfo-prebuilt /opt/bdinfo /opt/bdinfo
COPY bdinfo.sh /usr/local/bin/bdinfo
RUN chmod +x /usr/local/bin/bdinfo
ENV BDINFO_BIN=/usr/local/bin/bdinfo
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/minfo"]
