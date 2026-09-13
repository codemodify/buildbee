# Multi-stage: Vite web → Go Server with embedded UI.
# One Server process hosts /v1, /healthz, and the SPA.
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.22-alpine AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ .
COPY --from=web /web/dist ./internal/webui/dist
ENV GOTOOLCHAIN=local
RUN CGO_ENABLED=0 go build -o /buildbee-server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /buildbee-server /usr/local/bin/buildbee-server
COPY --from=web /web/dist /var/buildbee/web
EXPOSE 8080
ENV BUILDBEE_ADDR=:8080
ENV BUILDBEE_WEB_DIR=/var/buildbee/web
USER nobody
CMD ["buildbee-server"]
