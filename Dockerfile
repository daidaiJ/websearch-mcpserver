# Go 交叉编译在宿主机架构上完成（--platform=$BUILDPLATFORM），
# 不要在 QEMU 里跑编译器。运行层安装目标架构的 qpdf。
# TARGETOS/TARGETARCH 由 buildx 注入；本地 docker build 则跟随当前机器。
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o websearch ./cmd/

# ca-certificates 同样在宿主架构安装，再拷进目标镜像
FROM --platform=$BUILDPLATFORM alpine:latest AS certs
RUN apk --no-cache add ca-certificates

# PDFium：与 Release 整包 / npm 平台包共用 third_party/pdfium/fetch.sh 的
# 固定 chromium/8152。docker.yml 在 build 前把 linux-x64 / linux-arm64 落到
# 构建上下文；这里只挑 TARGETARCH 对应的那一份进最终镜像。
FROM --platform=$BUILDPLATFORM alpine:latest AS pdfium
ARG TARGETARCH
COPY third_party/pdfium/ /pdfium/
RUN set -e; \
    case "$TARGETARCH" in \
      amd64) arch=linux-x64 ;; \
      arm64) arch=linux-arm64 ;; \
      *) echo "unsupported TARGETARCH=$TARGETARCH" >&2; exit 1 ;; \
    esac; \
    test -f "/pdfium/$arch/libpdfium.so" || { \
      echo "missing /pdfium/$arch/libpdfium.so; run third_party/pdfium/fetch.sh linux-x64 linux-arm64" >&2; \
      exit 1; \
    }; \
    mkdir -p /out && cp -R "/pdfium/$arch" /out/pdfium

FROM alpine:latest

ARG TARGETARCH

RUN apk --no-cache add qpdf

WORKDIR /app/

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/cert.pem
COPY --from=builder /app/websearch .
COPY --from=builder /app/config.example.yaml ./config.yaml

# 可选的 PDFium 后端随镜像分发；运行时按 /app/third_party/pdfium/<arch>/
# 自动发现，与 Release 整包同一版本与校验和来源。
COPY --from=pdfium /out/pdfium /opt/pdfium
RUN set -e; \
    case "$TARGETARCH" in \
      amd64) arch=linux-x64 ;; \
      arm64) arch=linux-arm64 ;; \
      *) echo "unsupported TARGETARCH=$TARGETARCH" >&2; exit 1 ;; \
    esac; \
    mkdir -p "/app/third_party/pdfium/$arch"; \
    cp -R /opt/pdfium/. "/app/third_party/pdfium/$arch/"; \
    rm -rf /opt/pdfium

EXPOSE 8338

CMD ["./websearch", "start"]
