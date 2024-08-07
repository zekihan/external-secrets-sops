FROM golang:1.22.5-alpine AS builder

ARG GITHUB_TOKEN

# Setup
RUN mkdir -p /go/src/github.com/zekihan/external-secrets-sops
WORKDIR /go/src/github.com/zekihan/external-secrets-sops

RUN apk add --no-cache git ca-certificates make

COPY go.mod .

ENV CGO_ENABLED=0 GO111MODULE=on GOOS=linux

RUN go mod download

COPY . .

RUN make build

# Copy into scratch container
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/src/github.com/zekihan/external-secrets-sops/tmp/bin/external-secrets-sops ./

ENTRYPOINT ["./external-secrets-sops"]
