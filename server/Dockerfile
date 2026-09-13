FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /buildbee-server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /buildbee-server /usr/local/bin/buildbee-server
EXPOSE 8080
ENV BUILDBEE_ADDR=:8080
USER nobody
CMD ["buildbee-server"]
