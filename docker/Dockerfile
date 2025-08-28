# syntax=docker/dockerfile:1.7

###############################################
# Builder
###############################################
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

WORKDIR /src

# 更快的 apk 源与基础依赖
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk add --no-cache ca-certificates tzdata git build-base

# 复制 go.mod/go.sum 以缓存依赖
COPY go.mod go.sum ./
ENV GOTOOLCHAIN=auto
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# 复制源码
COPY . .

# 编译
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags "-s -w" -o /out/hosts-server ./

###############################################
# Runner
###############################################
FROM gcr.io/distroless/static:nonroot AS runner

WORKDIR /app
COPY --from=builder /out/hosts-server /app/hosts-server
COPY domain.txt /app/domain.txt

# 默认监听端口
EXPOSE 8585

# 默认以服务模式启动；可通过运行时覆盖参数
ENTRYPOINT ["/app/hosts-server"]
CMD ["-server", "true", "-port", "8585", "-domain", "domain.txt", "-output", "hosts"]


