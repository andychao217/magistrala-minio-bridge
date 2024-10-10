# 使用官方的 Golang 镜像作为基础镜像
FROM --platform=$BUILDPLATFORM golang:1.22.1-alpine AS builder

# 设置 GOPROXY 环境变量
ENV GOPROXY=https://goproxy.cn,https://goproxy.io,https://mirrors.aliyun.com/goproxy/,https://proxy.golang.org,direct

# 定义构建参数
ARG TARGETOS
ARG TARGETARCH
ARG SVC

# 设置工作目录
WORKDIR /app

# 复制当前目录内容到工作目录
COPY . .

# 更新 apk 源并安装 make
RUN echo http://mirrors.aliyun.com/alpine/v3.19/main/ > /etc/apk/repositories && \
    echo http://mirrors.aliyun.com/alpine/v3.19/community/ >> /etc/apk/repositories && \
    apk update && apk upgrade && apk add --no-cache make

# 安装 UPX（如果可用），否则手动下载并安装
RUN if apk add --no-cache upx; then \
        echo "UPX 已通过 apk 安装"; \
    else \
        echo "通过 apk 安装 UPX 失败，尝试手动安装"; \
        wget https://github.com/upx/upx/releases/download/v4.0.4/upx-4.0.4-amd64-linux.tar.xz && \
        tar -xf upx-4.0.4-amd64-linux.tar.xz && \
        mv upx-4.0.4-amd64-linux/upx /usr/local/bin/ && \
        rm -rf upx-4.0.4-amd64-linux upx-4.0.4-amd64-linux.tar.xz; \
    fi

# 构建 Go 应用程序并压缩可执行文件
RUN make GOOS=$TARGETOS GOARCH=$TARGETARCH $SVC && \
    upx /app/build/$SVC && \
    mv /app/build/$SVC /exe

# 最终镜像使用 Alpine 基础镜像
FROM alpine:3.19

# 设置默认时区（可选）
ENV TZ=Asia/Shanghai

# 安装 tzdata
RUN apk add --no-cache tzdata

# 从 builder 阶段复制压缩后的可执行文件
COPY --from=builder /exe /exe

# 从 builder 阶段复制静态文件
COPY --from=builder /app/static /static

# 设置工作目录
WORKDIR /

# 设置时区链接（可选）
RUN cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && echo "Asia/Shanghai" > /etc/timezone

# 暴露程序的端口
EXPOSE 9102

# 启动应用程序
ENTRYPOINT ["/exe"]
