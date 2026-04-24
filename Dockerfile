# syntax=docker/dockerfile:1

# Builder — compiles the Go binary.
FROM golang:1.23-bookworm AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w \
      -X main.version=${VERSION} \
      -X main.commit=${COMMIT} \
      -X main.date=${DATE}" \
    -o /out/preview-argocd-diff ./cmd/preview-argocd-diff


# Runtime — preview-argocd-diff + the CLIs it shells out to.
FROM debian:bookworm-slim AS runtime

ARG TARGETARCH=amd64
ARG KIND_VERSION=v0.23.0
ARG KUBECTL_VERSION=v1.30.0
ARG HELM_VERSION=v3.15.0
ARG ARGOCD_VERSION=v2.12.0

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates curl git tar \
    && rm -rf /var/lib/apt/lists/*

# kind
RUN curl -fsSL -o /usr/local/bin/kind \
        "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-${TARGETARCH}" \
    && chmod +x /usr/local/bin/kind

# kubectl
RUN curl -fsSL -o /usr/local/bin/kubectl \
        "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" \
    && chmod +x /usr/local/bin/kubectl

# helm
RUN curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-${TARGETARCH}.tar.gz" \
        | tar -xz --strip-components=1 -C /usr/local/bin linux-${TARGETARCH}/helm \
    && chmod +x /usr/local/bin/helm

# argocd CLI
RUN curl -fsSL -o /usr/local/bin/argocd \
        "https://github.com/argoproj/argo-cd/releases/download/${ARGOCD_VERSION}/argocd-linux-${TARGETARCH}" \
    && chmod +x /usr/local/bin/argocd

# Version manifest so the Go binary can verify pinned flags match what's
# baked in and fall back to on-demand download only when necessary.
RUN mkdir -p /etc/padp \
    && printf '{\n  "kind": "%s",\n  "kubectl": "%s",\n  "helm": "%s",\n  "argocd": "%s"\n}\n' \
        "${KIND_VERSION}" "${KUBECTL_VERSION}" "${HELM_VERSION}" "${ARGOCD_VERSION}" \
        > /etc/padp/versions.json

COPY --from=builder /out/preview-argocd-diff /usr/local/bin/preview-argocd-diff

ENTRYPOINT ["/usr/local/bin/preview-argocd-diff"]
CMD ["run"]
