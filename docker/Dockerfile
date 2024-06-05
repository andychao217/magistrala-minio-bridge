# 使用官方的Golang镜像作为基础镜像  
# Copyright (c) Abstract Machines
# SPDX-License-Identifier: Apache-2.0

# 使用官方的Golang镜像作为基础镜像
FROM --platform=$BUILDPLATFORM golang:1.22.1-alpine AS builder

# 设置GOPROXY环境变量
ENV GOPROXY=https://goproxy.cn,https://goproxy.io,https://mirrors.aliyun.com/goproxy/,https://proxy.golang.org,direct

ARG TARGETOS
ARG TARGETARCH
ARG SVC

WORKDIR /app

COPY . .

RUN echo http://mirrors.aliyun.com/alpine/v3.19/main/ > /etc/apk/repositories && \
    echo http://mirrors.aliyun.com/alpine/v3.19/community/ >> /etc/apk/repositories && \
    apk update && apk upgrade && apk add make

# 构建Go应用程序
RUN make GOOS=$TARGETOS GOARCH=$TARGETARCH $SVC && mv build/$SVC /exe

# 使用scratch作为基础镜像
FROM scratch

# 从builder阶段复制可执行文件
COPY --from=builder /exe /exe

# 从builder阶段复制静态文件
COPY --from=builder /app/static /static

# 设置工作目录
WORKDIR /

# 暴露程序的端口
EXPOSE 9102

# 启动应用程序
ENTRYPOINT ["/exe"]
