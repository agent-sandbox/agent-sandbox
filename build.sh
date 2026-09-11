#!/bin/sh
set -e

V=0.8.4-202609041043
IMAGE=ghcr.io/agent-sandbox/agent-sandbox:$V
IMAGE_LATEST=ghcr.io/agent-sandbox/agent-sandbox:latest

echo "start building..."
#build go app
go env -w CGO_ENABLED=0
go env -w GOARCH=amd64
go env -w GOOS=linux
go build -ldflags "-X github.com/agent-sandbox/agent-sandbox/pkg/config.Version=$V" -o agent-sandbox
echo "=> build agent-sandbox success..."

#build ui
cd ui
npm install
npm run build

#build images
cd ..
echo "=> image: $IMAGE"
docker build -t $IMAGE -t $IMAGE_LATEST .
docker push $IMAGE
docker push $IMAGE_LATEST
echo "=> build image success..."
