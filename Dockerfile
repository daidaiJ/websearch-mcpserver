# 使用官方Golang镜像作为构建阶段
# 注意：版本需与 go.mod 的 go 指令保持一致（当前 go 1.26）
FROM golang:1.26-alpine AS builder

# 版本号由 CI 注入（与 release.yml 二进制构建的 -X main.version 一致）
ARG VERSION=dev

WORKDIR /app

# 复制go模块文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 构建二进制文件（静态链接；./cmd/ 整包编译，包含 setup_other.go 等平台文件）
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o websearch ./cmd/

# 使用alpine作为运行阶段，创建更小的镜像
FROM alpine:latest

# 安装ca-certificates以支持HTTPS请求
RUN apk --no-cache add ca-certificates

# 设置工作目录
WORKDIR /app/

# 从构建阶段复制二进制文件
COPY --from=builder /app/websearch .

# 复制示例配置作为默认配置（config.yaml 被 gitignore，CI 中不存在；
# 程序首次 start 也会自动生成，这里显式放置便于容器内直接修改）
COPY --from=builder /app/config.example.yaml ./config.yaml

# 暴露端口（根据main.go中的conf.Port）
EXPOSE 8338

# 运行应用
CMD ["./websearch", "start"]
