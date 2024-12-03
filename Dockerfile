FROM golang:1.23.3@sha256:b4aabba13d38c069fa6d6ee3656e1fcb11c18181ecfd13728bfced414f6422ca AS builder
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
