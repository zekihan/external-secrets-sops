FROM --platform=$BUILDPLATFORM busybox:1.38.0@sha256:a46257e06c82ad85242d0d38077d0a52d9896389ca60a1a59d3463db586e5fa4 AS builder

ARG TARGETPLATFORM

COPY ./dist ./dist

RUN case ${TARGETPLATFORM} in \
        "linux/amd64") ARCH="amd64" ;; \
        "linux/arm64") ARCH="arm64" ;; \
        *) exit 1 ;; \
    esac && \
    cp ./dist/external-secrets-sops_linux_${ARCH}/external-secrets-sops /usr/bin/external-secrets-sops

FROM --platform=$TARGETPLATFORM ghcr.io/zekihan/distroless:v1.0.0@sha256:f2ec17ff44138983a5e892ab05a4eb91ea58e33d3bb263830713823ef13531c6 AS final

COPY --from=builder /usr/bin/external-secrets-sops /usr/bin/external-secrets-sops

USER 25000:25000

ENTRYPOINT ["external-secrets-sops"]

LABEL org.opencontainers.image.authors="zekihan@noqer.com" \
    org.opencontainers.image.url="https://noqer.com/" \
    org.opencontainers.image.documentation="https://docs.noqer.com/" \
    org.opencontainers.image.source="https://github.com/zekihan/external-secrets-sops" \
    org.opencontainers.image.vendor="Zekihan AZMAN" \
    org.opencontainers.image.licenses="AGPL-3.0" \
    org.opencontainers.image.title="External Secrets Sops" \
    org.opencontainers.image.description="External Secrets Sops is a tool that helps you to manage your secrets in Kubernetes with Sops."
