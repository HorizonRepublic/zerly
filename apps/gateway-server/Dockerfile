# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/gateway ./cmd/gateway

FROM scratch

COPY --from=builder /out/gateway /gateway
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

EXPOSE 8080 9090

ENTRYPOINT ["/gateway"]
