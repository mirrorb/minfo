ARG BDINFO_REPO=https://github.com/dotnetcorecorner/BDInfo.git
ARG BDINFO_REF=master
ARG BDINFO_CSPROJ=BDInfo.Core/BDInfo/BDInfo.csproj

# ==========================================
# Stage 1: 构建 WebUI (Node.js)
# ==========================================
FROM --platform=$BUILDPLATFORM node:20-alpine AS webui
WORKDIR /app
COPY webui/package.json ./
RUN npm install --no-audit --no-fund
COPY webui .
RUN npm run build

# ==========================================
# Stage 2: 构建 Go 后端
# ==========================================
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY --from=webui /app/dist ./webui/dist
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0
# 禁用 CGO 并编译
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/minfo

# ==========================================
# Stage 3: 构建 BDInfo (.NET)
# ==========================================
FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:9.0-alpine AS bdinfo-build
ARG BDINFO_REPO
ARG BDINFO_REF
ARG BDINFO_CSPROJ
ARG TARGETARCH
RUN apk add --no-cache git ca-certificates
RUN git clone --depth 1 --branch "$BDINFO_REF" "$BDINFO_REPO" /src/bdinfo
WORKDIR /src/bdinfo
RUN set -eux; \
    # 注意这里：Alpine 必须使用 musl 对应的 RID
    case "$TARGETARCH" in \
        amd64) rid="linux-musl-x64" ;; \
        arm64) rid="linux-musl-arm64" ;; \
        *) echo "unsupported TARGETARCH=$TARGETARCH" >&2; exit 1 ;; \
    esac; \
    dotnet restore "$BDINFO_CSPROJ"; \
    # 开启 Trimming (裁剪) 极致压缩体积
    dotnet publish "$BDINFO_CSPROJ" -c Release -r "$rid" --self-contained true \
        -p:PublishSingleFile=true \
        -p:PublishTrimmed=true \
        -p:EnableCompressionInSingleFile=true \
        -p:DebugType=None \
        -p:DebugSymbols=false \
        -o /out/bdinfo; \
    # 兼容不同 find 版本的安全提取逻辑
    exe=""; \
    for f in /out/bdinfo/*; do \
        if [ -f "$f" ] &&[ -x "$f" ] && [ "${f##*.}" != "dll" ] &&[ "${f##*.}" != "json" ] && [ "${f##*.}" != "pdb" ]; then \
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

# ==========================================
# Stage 4: 最终运行环境 (极简 Alpine)
# ==========================================
FROM alpine:3.19

# 安装运行时必需依赖
# - findutils: 解决 bdinfo.sh 中 find -printf 的兼容问题
# - util-linux: 提供完整的 mount 命令
# - libstdc++ & libgcc: .NET Self-contained 程序的基础依赖
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

# 复制产物
COPY --from=build /out/minfo /usr/local/bin/minfo
COPY --from=bdinfo-build /out/bdinfo/BDInfo /opt/bdinfo/BDInfo
COPY bdinfo.sh /usr/local/bin/bdinfo

# 赋予执行权限
RUN chmod +x /usr/local/bin/bdinfo /usr/local/bin/minfo /opt/bdinfo/BDInfo

# 环境变量设置
ENV BDINFO_BIN=/usr/local/bin/bdinfo
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV PORT=8080

# 开启全球化不变模式，免去安装庞大的 ICU 依赖包，进一步缩减体积
ENV DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/minfo"]