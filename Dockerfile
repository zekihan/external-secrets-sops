FROM --platform=$BUILDPLATFORM busybox:1.37.0@sha256:2919d0172f7524b2d8df9e50066a682669e6d170ac0f6a49676d54358fe970b5 AS builder

ARG TARGETPLATFORM

COPY dist/* /usr/bin/

RUN case ${TARGETPLATFORM} in \
        "linux/amd64")    ARCH="amd64"    ;; \
        "linux/arm64")    ARCH="arm64"    ;; \
        "linux/riscv64")  ARCH="riscv64"  ;; \
        "linux/ppc64le")  ARCH="ppc64le"  ;; \
        "linux/s390x")    ARCH="s390x"    ;; \
        "linux/386")      ARCH="386"      ;; \
        "linux/arm/v7")   ARCH="arm_v7"   ;; \
        "linux/arm/v6")   ARCH="arm_v6"   ;; \
    esac && \
    mv /usr/bin/external-secrets-sops_linux_${ARCH} /usr/bin/external-secrets-sops

FROM scratch AS final

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
