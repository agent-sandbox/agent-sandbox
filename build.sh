#!/bin/sh
set -e
while [[ $# -gt 0 ]]; do
    case $1 in
        --ns|--namespace)
            NS="$2"
            shift 2
            ;;
        --image)
            IMAGE="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

if [ -z "$IMAGE" ]; then
  echo "Unknown option: $1"
  exit 1
#  serviceVersion=$(date +%Y%m%d%H%M%S)
#  IMAGE="ghcr.io/agent-sandbox/agent-sandbox:dev-0.1"
fi


echo "start building..."
#build go app
go env -w CGO_ENABLED=0
go env -w GOARCH=amd64
go env -w GOOS=linux
go env -w GOPROXY=https://goproxy.cn,direct
go build -o agent-sandbox
echo "=> build agent-sandbox success..."
if [ -z "$NS" ]; then
  NS=agent-sandbox
fi


echo "=> namespace: $NS"
echo "=> image: $IMAGE"
docker build -t $IMAGE .
docker push $IMAGE
echo "=> build image success..."