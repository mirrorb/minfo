ARG BDINFO_REPO=https://github.com/dotnetcorecorner/BDInfo.git
ARG BDINFO_REF=master
ARG BDINFO_CSPROJ=
ARG RUNTIME_BASE=debian:bookworm-slim
ARG DOTNET_SDK_TAG=9.0-bookworm-slim

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

FROM mcr.microsoft.com/dotnet/sdk:${DOTNET_SDK_TAG} AS bdinfo-build
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
    csproj="$BDINFO_CSPROJ"; \
    if [ -z "$csproj" ]; then \
        csproj="$(grep -rl --include='*.csproj' '<OutputType>Exe</OutputType>' . | grep -i bdinfo | head -n 1 || true)"; \
    fi; \
    if [ -z "$csproj" ]; then \
        csproj="$(find . -maxdepth 5 -name '*BDInfo*CLI*.csproj' ! -name '*Test*' | head -n 1)"; \
    fi; \
    if [ -z "$csproj" ]; then \
        csproj="$(find . -maxdepth 5 -name '*BDInfo*.csproj' ! -name '*Test*' | head -n 1)"; \
    fi; \
    if [ -z "$csproj" ]; then \
        echo "BDInfo csproj not found" >&2; \
        exit 1; \
    fi; \
    dotnet restore "$csproj"; \
    dotnet publish "$csproj" -c Release -r "$rid" --self-contained true \
        -p:PublishSingleFile=true \
        -p:PublishTrimmed=true \
        -p:IncludeNativeLibrariesForSelfExtract=true \
        -o /out/bdinfo; \
    exe="$(find /out/bdinfo -maxdepth 1 -type f -perm /111 | head -n 1)"; \
    if [ -z "$exe" ]; then \
        echo "BDInfo executable not found in publish output" >&2; \
        ls -la /out/bdinfo; \
        exit 1; \
    fi; \
    if [ "$(basename "$exe")" != "BDInfo" ]; then \
        mv "$exe" /out/bdinfo/BDInfo; \
    fi; \
    chmod +x /out/bdinfo/BDInfo

FROM ${RUNTIME_BASE}
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    ffmpeg \
    mediainfo \
    libgdiplus \
    util-linux \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/minfo /usr/local/bin/minfo
COPY --from=bdinfo-build /out/bdinfo /opt/bdinfo
COPY bdinfo.sh /usr/local/bin/bdinfo
RUN set -eux; \
    chmod +x /usr/local/bin/bdinfo; \
    if [ -f /opt/bdinfo/BDInfo ]; then chmod +x /opt/bdinfo/BDInfo; fi
ENV BDINFO_BIN=/usr/local/bin/bdinfo
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/minfo"]
