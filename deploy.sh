#!/bin/sh
set -e
# 解析命令行参数
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

cp deployment.yaml deploy-tmp.yaml

escape_sed() {
    printf '%s\n' "$1" | sed 's/[&/\]/\\&/g'
}

ESCAPED_NS=$(escape_sed "$NS")
ESCAPED_IMAGE=$(escape_sed "$IMAGE")

if [[ "$OSTYPE" == "darwin"* ]]; then
    # macOS
    sed -i "" "s/\${NS}/$ESCAPED_NS/g" deploy-tmp.yaml
    sed -i "" "s/\${IMAGE}/$ESCAPED_IMAGE/g" deploy-tmp.yaml
else
    # Linux
    sed -i "s/\${NS}/$ESCAPED_NS/g" deploy-tmp.yaml
    sed -i "s/\${IMAGE}/$ESCAPED_IMAGE/g" deploy-tmp.yaml
fi

#cat deploy-tmp.yaml
#kubectl apply -f deploy-tmp.yaml
