#!/usr/bin/env bash

set -euo pipefail

# 项目配置（可按需修改）
IMAGE_NAME=${IMAGE_NAME:-"hosts-server"}
IMAGE_REGISTRY=${IMAGE_REGISTRY:-""}      # 例如: docker.io/yourname 或 ghcr.io/yourorg
IMAGE_TAG=${IMAGE_TAG:-"latest"}
PLATFORMS=${PLATFORMS:-"linux/amd64,linux/arm64"}
PUSH=${PUSH:-"false"}      # true: 推送到镜像仓库；false: 仅本地加载
LOAD=${LOAD:-"false"}      # true: buildx --load (仅支持单平台)

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
DOCKERFILE=${DOCKERFILE:-"${ROOT_DIR}/docker/Dockerfile"}
CONTEXT_DIR=${CONTEXT_DIR:-"${ROOT_DIR}"}

if ! command -v docker >/dev/null 2>&1; then
  echo "docker 未安装或不在 PATH" >&2
  exit 1
fi

# 创建 buildx builder（如不存在）
if ! docker buildx inspect multiarch-builder >/dev/null 2>&1; then
  docker buildx create --name multiarch-builder --use >/dev/null
else
  docker buildx use multiarch-builder >/dev/null
fi

set -x

TAGS=()
if [[ -n "${IMAGE_REGISTRY}" ]]; then
  TAGS+=("${IMAGE_REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}")
else
  TAGS+=("${IMAGE_NAME}:${IMAGE_TAG}")
fi

BUILD_ARGS=(
  --platform "${PLATFORMS}"
  -f "${DOCKERFILE}"
  "${CONTEXT_DIR}"
)

for t in "${TAGS[@]}"; do
  BUILD_ARGS+=( -t "$t" )
done

if [[ "${PUSH}" == "true" ]]; then
  BUILD_ARGS+=( --push )
elif [[ "${LOAD}" == "true" ]]; then
  BUILD_ARGS+=( --load )
fi

docker buildx build "${BUILD_ARGS[@]}"

set +x
echo "\n构建完成。镜像标签: ${TAGS[*]}"
if [[ "${PUSH}" == "true" ]]; then
  echo "镜像已推送到远端仓库。"
elif [[ "${LOAD}" == "true" ]]; then
  echo "镜像已加载到本地 Docker。"
else
  echo "镜像位于 buildx 缓存/本地 registry（未 --load）。可改用 PUSH=true 进行推送。"
fi


