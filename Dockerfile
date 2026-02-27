ARG BDINFO_REPO=https://github.com/dotnetcorecorner/BDInfo.git
ARG BDINFO_REF=master
ARG BDINFO_CSPROJ=BDInfo.Core/BDInfo/BDInfo.csproj

# 构建 WebUI
FROM --platform=$BUILDPLATFORM node:20-alpine AS webui
WORKDIR /app
COPY webui/package.json ./
RUN npm install --no-audit --no-fund
COPY webui .
RUN npm run build

# 构建 Go 后端
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY --from=webui /app/dist ./webui/dist
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/minfo

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
        if[ -f "$f" ] && [ -x "$f" ] &&[ "${f##*.}" != "dll" ] && [ "${f##*.}" != "json" ] &&[ "${f##*.}" != "pdb" ]; then \
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

# 最终运行环境 (Alpine)
FROM alpine:3.19
RUN apk add --no-cache \
    ca-certificates \
    ffmpeg \
    mediainfo \
    libgdiplus \
    findutils \
    util-linux \
    libstdc++ \
    libgcc \
    tzdata \
    bash

COPY --from=build /out/minfo /usr/local/bin/minfo
COPY --from=bdinfo-build /out/bdinfo/BDInfo /opt/bdinfo/BDInfo
COPY bdinfo.sh /usr/local/bin/bdinfo

RUN chmod +x /usr/local/bin/bdinfo /usr/local/bin/minfo /opt/bdinfo/BDInfo

ENV BDINFO_BIN=/usr/local/bin/bdinfo
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV PORT=8080
# 开启全球化不变模式以缩减 .NET 运行体积
ENV DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1

EXPOSE 8080
ENTRYPOINT["/usr/local/bin/minfo"]