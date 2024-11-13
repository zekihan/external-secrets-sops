FROM golang:1.23.3@sha256:3694e364a0357a6e885569d3bdd097f7b36839bc4fb0df2af33d411b37e27e59 AS builder
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
