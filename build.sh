#!/bin/sh
set -e

V=$(git describe --tags --always --dirty)
LDFLAGS="-X github.com/agent-sandbox/agent-sandbox/pkg/config.Version=$V"
IMAGE=ghcr.io/agent-sandbox/agent-sandbox:$V
IMAGE_LATEST=ghcr.io/agent-sandbox/agent-sandbox:latest

echo "start building..."
#build go app
go env -w CGO_ENABLED=0
go env -w GOARCH=amd64
go env -w GOOS=linux
go env -w GOPROXY=https://goproxy.cn,direct
go build -ldflags "$LDFLAGS" -o agent-sandbox
echo "=> build agent-sandbox success..."

echo "=> image: $IMAGE"
docker build -t $IMAGE -t $IMAGE_LATEST .
docker push $IMAGE
docker push $IMAGE_LATEST
echo "=> build image success..."
