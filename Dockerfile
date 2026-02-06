FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
COPY static ./static
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/minfo

FROM debian:bookworm-slim AS bluray
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    git \
    && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 https://github.com/Aniverse/bluray.git /tmp/bluray

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    ffmpeg \
    mediainfo \
    mono-runtime \
    libmono-system-windows-forms4.0-cil \
    libgdiplus \
    util-linux \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/minfo /usr/local/bin/minfo
COPY --from=bluray /tmp/bluray/tools/BDinfoCli.0.7.3 /opt/bdinfo/BDinfoCli.0.7.3
COPY --from=bluray /tmp/bluray/tools/bdinfocli.exe /opt/bdinfo/bdinfocli.exe
COPY --from=bluray /tmp/bluray/README.md /opt/bdinfo/README.bluray.md
COPY bdinfo.sh /usr/local/bin/bdinfo
RUN chmod +x /usr/local/bin/bdinfo
ENV BDINFO_BIN=/usr/local/bin/bdinfo
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/minfo"]
