FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -o /buildbee-runtime ./cmd/runtime

FROM alpine:3.20
RUN apk add --no-cache ca-certificates docker-cli
COPY --from=build /buildbee-runtime /usr/local/bin/buildbee-runtime
EXPOSE 8090
ENV BUILDBEE_RUNTIME_ADDR=:8090
CMD ["buildbee-runtime"]
