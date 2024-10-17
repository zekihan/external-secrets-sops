FROM golang:1.23.2@sha256:540d3442f4758da82e787d03930bf6468cf6f8613474135e8946259b9e531ea0 AS builder
WORKDIR /app

COPY go.mod .
COPY go.sum .

ENV CGO_ENABLED=0 GO111MODULE=on GOOS=linux

RUN go mod download

COPY . .

RUN ./scripts/build.sh

# Copy into scratch container
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/tmp/bin/external-secrets-sops ./

ENTRYPOINT ["./external-secrets-sops"]
