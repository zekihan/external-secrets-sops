FROM --platform=$BUILDPLATFORM busybox:1.37.0@sha256:b3255e7dfbcd10cb367af0d409747d511aeb66dfac98cf30e97e87e4207dd76f AS builder

ARG TARGETPLATFORM

COPY ./dist ./dist

RUN case ${TARGETPLATFORM} in \
        "linux/amd64")    ARCH="amd64"    ;; \
        "linux/arm64")    ARCH="arm64"    ;; \
        "linux/riscv64")  ARCH="riscv64"  ;; \
        "linux/ppc64le")  ARCH="ppc64le"  ;; \
        "linux/s390x")    ARCH="s390x"    ;; \
        "linux/386")      ARCH="386"      ;; \
        "linux/arm/v7")   ARCH="armv7"   ;; \
        "linux/arm/v6")   ARCH="armv6"   ;; \
    esac && \
    mv ./dist/external-secrets-sops_linux_${ARCH}/external-secrets-sops /usr/bin/external-secrets-sops

FROM --platform=$BUILDPLATFORM zekihan/distroless:v1.0.0@sha256:51f31cfb7765f5f242df6e959e6742d9b0d9cb435387eb4ffb4faf0a83c53d53 AS final

COPY --from=builder /usr/bin/external-secrets-sops /usr/bin/external-secrets-sops

ENTRYPOINT ["external-secrets-sops"]

LABEL org.opencontainers.image.authors="zekihan@noqer.com" \
    org.opencontainers.image.url="https://noqer.com/" \
    org.opencontainers.image.documentation="https://docs.noqer.com/" \
    org.opencontainers.image.source="https://github.com/zekihan/external-secrets-sops" \
    org.opencontainers.image.vendor="Zekihan AZMAN" \
    org.opencontainers.image.licenses="AGPL-3.0" \
    org.opencontainers.image.title="External Secrets Sops" \
    org.opencontainers.image.description="External Secrets Sops is a tool that helps you to manage your secrets in Kubernetes with Sops."
