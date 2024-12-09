FROM golang:1.23.4@sha256:574185e5c6b9d09873f455a7c205ea0514bfd99738c5dc7750196403a44ed4b7 AS builder
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
