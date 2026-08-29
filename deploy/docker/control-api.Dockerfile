# Reproducible, minimal image for the development control API.
FROM golang:1.26.6-alpine3.23@sha256:e57c41c1d5864341031181b0db34b9a537bb5773eb6428e4e5bdaea0f9135406 AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod ./
COPY adapters ./adapters
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY services ./services
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/control-api ./cmd/control-api

# The binary is static. The runtime contains only the binary and CA roots.
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/control-api /usr/local/bin/control-api
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/control-api"]
