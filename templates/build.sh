V=0.1.1

echo "-----------building sandbox-base with version $V----------"
docker build  -t ghcr.io/agent-sandbox/sandbox-base:$V sandbox-base
docker push ghcr.io/agent-sandbox/sandbox-base:$V
docker build  -t ghcr.io/agent-sandbox/sandbox-base:latest sandbox-base
docker push ghcr.io/agent-sandbox/sandbox-base:latest

echo "----------building sandbox-base-node with version $V----------"
docker build  -t ghcr.io/agent-sandbox/sandbox-base-node:$V sandbox-base-node
docker push ghcr.io/agent-sandbox/sandbox-base-node:$V
docker build  -t ghcr.io/agent-sandbox/sandbox-base-node:latest sandbox-base-node
docker push ghcr.io/agent-sandbox/sandbox-base-node:latest

echo "----------building sandbox-base-code with version $V----------"
docker build -t ghcr.io/agent-sandbox/sandbox-base-code:$V sandbox-base-code
docker push ghcr.io/agent-sandbox/sandbox-base-code:$V
docker build -t ghcr.io/agent-sandbox/sandbox-base-code:latest sandbox-base-code
docker push ghcr.io/agent-sandbox/sandbox-base-code:latest