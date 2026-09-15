# BuildBee Server: API, WebSocket and the embedded web UI in one binary.
FROM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
ENV GOTOOLCHAIN=local CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /web/dist ./internal/webui/dist
RUN go build -trimpath -o /out/buildbee-server ./cmd/buildbee-server

FROM alpine:3.24
RUN apk add --no-cache ca-certificates && mkdir -p /data/blobs && chown -R nobody /data
COPY --from=build /out/buildbee-server /usr/local/bin/buildbee-server
EXPOSE 8080
ENV BUILDBEE_ADDR=:8080 BUILDBEE_BLOB_DIR=/data/blobs
VOLUME /data
USER nobody
CMD ["buildbee-server"]
