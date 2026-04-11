ARG BDINFO_REPO=https://github.com/mirrorb/BDInfoCLI.git
ARG BDINFO_REF=master
ARG BDINFO_CSPROJ=BDInfo/BDInfo.csproj
ARG GO_VERSION=1.26.1
ARG APP_VERSION=dev

# 构建 WebUI
FROM --platform=$BUILDPLATFORM node:20-alpine AS webui
ARG APP_VERSION=dev
WORKDIR /app
COPY webui/package.json ./
RUN npm install --no-audit --no-fund
COPY webui .
ENV VITE_APP_VERSION=$APP_VERSION
RUN npm run build

# 构建 Go 后端
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG APP_VERSION=dev
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY cmd ./cmd
COPY internal ./internal
COPY --from=webui /app/dist ./webui/dist
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -ldflags="-s -w -X minfo/internal/version.Version=${APP_VERSION}" -o /out/minfo ./cmd/minfo

# 构建 BDInfo (.NET)
FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:9.0-alpine AS bdinfo-build
ARG BDINFO_REPO
ARG BDINFO_REF
ARG BDINFO_CSPROJ
ARG TARGETARCH
RUN apk add --no-cache git ca-certificates
RUN git clone --depth 1 --branch "$BDINFO_REF" "$BDINFO_REPO" /src/bdinfo
WORKDIR /src/bdinfo
RUN set -eux; \
    # 匹配 Alpine (musl) 架构的 RID
    case "$TARGETARCH" in \
        amd64) rid="linux-musl-x64" ;; \
        arm64) rid="linux-musl-arm64" ;; \
        *) echo "unsupported TARGETARCH=$TARGETARCH" >&2; exit 1 ;; \
    esac; \
    dotnet restore "$BDINFO_CSPROJ"; \
    # 编译单文件版 (禁用 Trim 以防命令行反射报错)
    dotnet publish "$BDINFO_CSPROJ" -c Release -r "$rid" --self-contained true \
        -p:PublishSingleFile=true \
        -p:EnableCompressionInSingleFile=true \
        -p:DebugType=None \
        -p:DebugSymbols=false \
        -o /out/bdinfo; \
    # 提取生成的二进制文件
    exe=""; \
    for f in /out/bdinfo/*; do \
        if [ -f "$f" ] && [ -x "$f" ] &&[ "${f##*.}" != "dll" ] && [ "${f##*.}" != "json" ] &&[ "${f##*.}" != "pdb" ]; then \
            exe="$f"; break; \
        fi; \
    done; \
    if [ -n "$exe" ]; then \
        mv "$exe" /out/bdinfo/BDInfo; \
    else \
        echo "BDInfo executable not found" >&2; exit 1; \
    fi; \
    chmod +x /out/bdinfo/BDInfo; \
    find /out/bdinfo -type f \( -name '*.pdb' -o -name '*.xml' -o -name '*.dbg' \) -delete

# 构建 BD/DVD 元数据 helper
FROM --platform=$TARGETPLATFORM alpine:3.19 AS media-helper-build
RUN apk add --no-cache build-base
WORKDIR /src
COPY tools/bdsub_probe.c ./tools/bdsub_probe.c
RUN mkdir -p /out && \
    cc -O2 -Wall -Wextra -std=c11 ./tools/bdsub_probe.c -o /out/bdsub

# 最终运行环境 (Alpine)
FROM alpine:3.19 AS runtime
RUN apk add --no-cache \
    ca-certificates \
    curl \
    ffmpeg \
    mediainfo \
    fontconfig \
    font-noto-cjk \
    kmod \
    libgdiplus \
    findutils \
    util-linux \
    libstdc++ \
    libgcc \
    tzdata \
    bash \
    jq \
    bc \
    file \
    coreutils

RUN set -eux; \
    printf '#!/bin/sh\nexec "$@"\n' > /usr/local/bin/sudo; \
    chmod +x /usr/local/bin/sudo

COPY --from=build /out/minfo /usr/local/bin/minfo
COPY --from=bdinfo-build /out/bdinfo/BDInfo /usr/local/bin/bdinfo
COPY --from=media-helper-build /out/bdsub /usr/local/bin/bdsub

RUN chmod +x /usr/local/bin/minfo /usr/local/bin/bdinfo /usr/local/bin/bdsub

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV PORT=28080
ENV DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1

EXPOSE 28080
ENTRYPOINT ["/usr/local/bin/minfo"]

# 本地调试环境 (Go + Delve + 运行依赖)
FROM golang:${GO_VERSION}-alpine AS debug
RUN apk add --no-cache \
    ca-certificates \
    curl \
    ffmpeg \
    mediainfo \
    fontconfig \
    font-noto-cjk \
    kmod \
    libgdiplus \
    findutils \
    util-linux \
    libstdc++ \
    libgcc \
    tzdata \
    bash \
    jq \
    bc \
    file \
    coreutils

RUN GOBIN=/usr/local/bin go install github.com/go-delve/delve/cmd/dlv@latest

COPY --from=runtime /usr/local/bin/bdinfo /usr/local/bin/bdinfo
COPY --from=runtime /usr/local/bin/bdsub /usr/local/bin/bdsub
COPY --from=runtime /usr/local/bin/sudo /usr/local/bin/sudo

RUN chmod +x /usr/local/bin/dlv /usr/local/bin/bdinfo /usr/local/bin/bdsub /usr/local/bin/sudo

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV PORT=28080
ENV GOCACHE=/tmp/go-build
ENV GOROOT=/usr/local/go
ENV PATH=/usr/local/go/bin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ENV DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1

WORKDIR /opt/minfo
EXPOSE 28080 2345
CMD ["dlv", "dap", "--listen=:2345"]

# 默认最终镜像保持为运行环境，避免 `docker build .` 误落到 debug stage。
FROM runtime AS final
