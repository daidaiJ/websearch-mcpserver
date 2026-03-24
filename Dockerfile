# 使用官方Golang镜像作为构建阶段
FROM golang:1.25-alpine AS builder

# 设置工作目录
WORKDIR /app

# 复制go模块文件
COPY go.mod go.sum ./
RUN go env -w GOPROXY=https://goproxy.cn,direct
RUN go mod tidy
# 下载依赖（使用-alpine镜像时需要安装git）

# 复制源代码
COPY . .
# 构建二进制文件（静态链接，适用于alpine）
RUN CGO_ENABLED=0 GOOS=linux go build  -o websearch ./cmd/main.go

# 使用alpine作为运行阶段，创建更小的镜像
FROM alpine:latest

# 安装ca-certificates以支持HTTPS请求
RUN apk --no-cache add ca-certificates

# 设置工作目录
WORKDIR /root/

# 从构建阶段复制二进制文件
COPY --from=builder /app/websearch .

# 复制配置文件
COPY --from=builder /app/config.yaml .

# 暴露端口（根据main.go中的conf.Port）
EXPOSE 8338

# 运行应用
CMD ["./websearch"]
