FROM golang:1.23-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
COPY web ./web

RUN CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/ylyxium-multistream-manager \
      .

FROM lscr.io/linuxserver/ffmpeg:version-9.0-cli

USER root

COPY --from=build \
  /out/ylyxium-multistream-manager \
  /usr/local/bin/ylyxium-multistream-manager

# Compatibilite avec les anciens scripts qui utilisaient
# /usr/local/bin/multistream-manager.
RUN ln -sf \
  /usr/local/bin/ylyxium-multistream-manager \
  /usr/local/bin/multistream-manager

VOLUME ["/data"]

EXPOSE 8090

ENTRYPOINT ["/usr/local/bin/ylyxium-multistream-manager"]
