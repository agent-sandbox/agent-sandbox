V=0.1.0

echo "-----------building sandbox-base with version $V----------"
docker build  -t ghcr.io/agent-sandbox/sandbox-base:$V sandbox-base
docker push ghcr.io/agent-sandbox/sandbox-base:$V

echo "----------building sandbox-base-node with version $V----------"
docker build  -t ghcr.io/agent-sandbox/sandbox-base-node:$V sandbox-base-node
docker push ghcr.io/agent-sandbox/sandbox-base-node:$V

echo "----------building sandbox-base-code with version $V----------"
docker build -t ghcr.io/agent-sandbox/sandbox-base-code:$V sandbox-base-code
docker push ghcr.io/agent-sandbox/sandbox-base-code:$V