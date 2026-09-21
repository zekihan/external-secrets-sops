FROM --platform=$BUILDPLATFORM busybox:1.38.0@sha256:dc2d74b28e4cf8984fa52af1f39bc7c3d9c73760b41a74d629f5d11b1ab28616 AS builder

ARG TARGETPLATFORM

COPY ./dist ./dist

RUN case ${TARGETPLATFORM} in \
        "linux/amd64") ARCH="amd64" ;; \
        "linux/arm64") ARCH="arm64" ;; \
        *) exit 1 ;; \
    esac && \
    cp ./dist/external-secrets-sops_linux_${ARCH}/external-secrets-sops /usr/bin/external-secrets-sops

FROM --platform=$TARGETPLATFORM zekihan/distroless:v1.0.0@sha256:c5df9133291ff067b2a0a9ce2624fbf54aa4e6d72d0fb1a3a9ada39754a83a98 AS final

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
