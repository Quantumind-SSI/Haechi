# Reproducible, minimal image for the development control API.
FROM golang:1.27.2-alpine3.23@sha256:9e45f0eb4a63ed37ad6e604950407558b7c738e34e015ae77a5d4ad369cde079 AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates
# go.sum is required now that the composition root pulls in a database driver.
# Dependencies are downloaded and verified before the source is copied, so the
# layer is cached and a source change does not refetch the module graph.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

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
