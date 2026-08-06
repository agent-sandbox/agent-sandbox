---
icon: lucide/play
---

# Quick Start

This guide walks you through deploying Agent-Sandbox and creating your first sandbox via E2B SDK and REST API.

## Prerequisites

- Kubernetes cluster (version 1.28 or higher)
- `kubectl` configured to access your cluster
- (Optional) `e2b-code-interpreter` Python SDK: `pip install e2b-code-interpreter`

## 1. Deploy Agent-Sandbox

Apply the installation manifest:

```bash
kubectl create namespace agent-sandbox
kubectl apply -n agent-sandbox -f install.yaml
```

!!! Tip

    Download `install.yaml`: https://github.com/agent-sandbox/agent-sandbox/blob/main/install.yaml


[install.yaml](https://github.com/agent-sandbox/agent-sandbox/blob/main/install.yaml) contains the full deployment configuration and `Agent-Sandbox` controller component.

## 2. Expose the Service

Create an Ingress:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: agent-sandbox
  namespace: agent-sandbox
spec:
  ingressClassName: ingress-nginx
  rules:
  - host: agent-sandbox.your-host.com
    http:
      paths:
      - backend:
          service:
            name: agent-sandbox
            port:
              number: 80
        path: /
```

Or port-forward for local testing:

```bash
kubectl port-forward -n agent-sandbox svc/agent-sandbox 8080:80
```

## 3. Verify Deployment

```bash
curl https://agent-sandbox.your-host.com/healthz
```

Expected response:

```json
{"status":"ok","version":"xxx"}
```

## 4. Authentication

Get your API key. The default system token is:

```
sys-2492a85b10ed4cb083b2c76b181eac96
```

All API requests require the `Authorization` header:

```bash
Authorization: Bearer <your-api-key>
```

---

## Create Sandbox via E2B SDK

The E2B SDK is the recommended way for Python applications.

### Install the SDK

```bash
pip install e2b-code-interpreter
```

### Configure Environment

```python
import os

# Point SDK to your Agent-Sandbox instance
os.environ['E2B_API_URL'] = 'https://agent-sandbox.your-host.com/e2b/v1'
os.environ['E2B_API_KEY'] = 'sys-2492a85b10ed4cb083b2c76b181eac96'
os.environ['E2B_DOMAIN'] = 'agent-sandbox.your-host.com'
```

#### For local development
!!! Warning

    For local or dev no HTTPS environment, the SDK's default API URL won't work. Use the following configuration to connect to a local instance without HTTPS:

```python
def local():
    os.environ['E2B_API_URL'] = 'http://example.domain.com/e2b/v1'
    os.environ['E2B_DOMAIN'] = 'example.domain.com'
    os.environ['E2B_API_KEY'] = 'testuser-aef134ef-7aa1-945e-9399-7df9a4ad0c3f'

    # use path to forward to sandbox, instead of subdomain
    def __connection_config_get_host(_, sandbox_id: str, sandbox_domain: str, port: int) -> str:
        return f"{sandbox_domain}/sandboxes/router/{sandbox_id}/{port}"

     # use http instead of https for sandbox url
    def __get_sandbox_url(self, sandbox_id: str, sandbox_domain: str) -> str:
        return f"http://{self.get_host(sandbox_id, sandbox_domain, self.envd_port)}"

    from e2b import ConnectionConfig
    ConnectionConfig.get_host = __connection_config_get_host
    ConnectionConfig.get_sandbox_url = __get_sandbox_url

    # patch the API key pattern to compatible old format,
    # new API key format is must start with "e2b_",(e2b version >= 2.25.0)
    from e2b import api
    import re
    api._API_KEY_PATTERN = re.compile(r"\A.{30,}\Z")
```



### Create and Use a Sandbox

```python
from e2b_code_interpreter import Sandbox

# Create a sandbox
sandbox = Sandbox.create(template='sandbox-base-code', timeout=300)

print(f"Sandbox ID: {sandbox.sandbox_id}")

# Execute code
execution = sandbox.run_code("print('Hello from Agent-Sandbox!')")
print(execution.logs)

# Run a shell command
result = sandbox.commands.run("ls -la")
print(result.stdout)

# Clean up
sandbox.kill()
```

### Connect to an Existing Sandbox

```python
from e2b_code_interpreter import Sandbox

# Reconnect by ID
sandbox = Sandbox.connect("existing-sandbox-id")
print(f"Connected to: {sandbox.sandbox_id}")
```

---

## Create Sandbox via REST API

For non-Python environments, use the REST API directly.

### Native REST API

```bash
curl -X POST http://localhost:8080/api/v1/sandbox \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sys-2492a85b10ed4cb083b2c76b181eac96" \
  -d '{"name":"my-sandbox"}'
```

Response:

```json
{
  "code": "0",
  "data": "Sandbox my-sandbox created successfully"
}
```

### E2B-Compatible API

```bash
curl -X POST http://localhost:8080/e2b/v1/sandboxes \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sys-2492a85b10ed4cb083b2c76b181eac96" \
  -d '{"templateID":"code-interpreter","timeout":300}'
```

Response:

```json
{
  "sandboxID": "e94466d4e94466d4",
  "templateID": "code-interpreter",
  "clientID": "client-id-x",
  "envdVersion": "0.1.1",
  "envdAccessToken": "e94466d4e94466d4",
  "trafficAccessToken": "e94466d4e94466d4",
  "domain": "localhost:8080",
  "metadata": {"name": "sandbox-abc123"},
  "cpuCount": 2000,
  "memoryMB": 4096,
  "diskSizeMB": 5120,
  "startedAt": "2026-01-27T10:00:00Z",
  "endAt": "2026-01-27T10:05:00Z",
  "state": "running"
}
```

### List Sandboxes

```bash
curl http://localhost:8080/api/v1/sandbox \
  -H "Authorization: Bearer sys-2492a85b10ed4cb083b2c76b181eac96"
```

### Delete Sandbox

```bash
curl -X DELETE http://localhost:8080/api/v1/sandbox/my-sandbox \
  -H "Authorization: Bearer sys-2492a85b10ed4cb083b2c76b181eac96"
```

---
