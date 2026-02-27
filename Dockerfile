ARG BDINFO_REPO=https://github.com/dotnetcorecorner/BDInfo.git
ARG BDINFO_REF=master
ARG BDINFO_CSPROJ=BDInfo.Core/BDInfo/BDInfo.csproj
ARG RUNTIME_BASE=debian:bookworm-slim
ARG DOTNET_SDK_TAG=9.0-bookworm-slim

FROM --platform=$BUILDPLATFORM node:20-bookworm-slim AS webui
WORKDIR /app
COPY webui/package.json ./
RUN --mount=type=cache,target=/root/.npm npm install --no-audit --no-fund
COPY webui .
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY --from=webui /app/dist ./webui/dist
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/minfo

FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:${DOTNET_SDK_TAG} AS bdinfo-build
ARG BDINFO_REPO
ARG BDINFO_REF
ARG BDINFO_CSPROJ
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    git \
    && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 --branch "$BDINFO_REF" "$BDINFO_REPO" /src/bdinfo
WORKDIR /src/bdinfo
RUN set -eux; \
    case "$TARGETARCH" in \
        amd64) rid="linux-x64" ;; \
        arm64) rid="linux-arm64" ;; \
        *) echo "unsupported TARGETARCH=$TARGETARCH" >&2; exit 1 ;; \
    esac; \
    if [ ! -f "$BDINFO_CSPROJ" ]; then \
        echo "BDInfo csproj not found: $BDINFO_CSPROJ" >&2; \
        exit 1; \
    fi; \
    dotnet restore "$BDINFO_CSPROJ"; \
    dotnet publish "$BDINFO_CSPROJ" -c Release -r "$rid" --self-contained true \
        -p:PublishSingleFile=true \
        -p:IncludeNativeLibrariesForSelfExtract=true \
        -p:EnableCompressionInSingleFile=true \
        -p:DebugType=None \
        -p:DebugSymbols=false \
        -o /out/bdinfo; \
    exe="$(find /out/bdinfo -maxdepth 1 -type f -perm /111 | head -n 1)"; \
    if [ -z "$exe" ]; then \
        echo "BDInfo executable not found in publish output" >&2; \
        ls -la /out/bdinfo; \
        exit 1; \
    fi; \
    exe_name="$(basename "$exe")"; \
    if [ "$exe_name" != "BDInfo" ]; then \
        mv "$exe" /out/bdinfo/BDInfo; \
    fi; \
    find /out/bdinfo -type f \( -name '*.pdb' -o -name '*.xml' -o -name '*.dbg' \) -delete; \
    chmod +x /out/bdinfo/BDInfo

FROM ${RUNTIME_BASE}
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    ffmpeg \
    mediainfo \
    libgdiplus \
    mount \
    && rm -rf /var/lib/apt/lists/* /var/cache/apt/archives/* \
    /usr/share/doc \
    /usr/share/info \
    /usr/share/man \
    /usr/share/locale/*
COPY --from=build /out/minfo /usr/local/bin/minfo
COPY --from=bdinfo-build /out/bdinfo/BDInfo /opt/bdinfo/BDInfo
COPY bdinfo.sh /usr/local/bin/bdinfo
RUN set -eux; \
    chmod +x /usr/local/bin/bdinfo; \
    if [ -f /opt/bdinfo/BDInfo ]; then chmod +x /opt/bdinfo/BDInfo; fi
ENV BDINFO_BIN=/usr/local/bin/bdinfo
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/minfo"]
