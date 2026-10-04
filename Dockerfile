FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY web ./web
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ylyxium-multistream-manager .

FROM lscr.io/linuxserver/ffmpeg:version-9.0-cli
USER root
COPY --from=build /out/ylyxium-multistream-manager /usr/local/bin/ylyxium-multistream-manager
VOLUME ["/data"]
EXPOSE 8090
ENTRYPOINT ["/usr/local/bin/ylyxium-multistream-manager"]
