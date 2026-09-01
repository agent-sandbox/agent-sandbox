---
icon: lucide/settings
---

# Environment Variables

Agent-Sandbox is configured via environment variables using [envconfig](https://github.com/kelseyhightower/envconfig). This page documents all available configuration options.

---

## Example Configuration

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-sandbox
  namespace: agent-sandbox
spec:
  template:
    spec:
      containers:
      - name: agent-sandbox
        env:
        - name: SYSTEM_TOKEN
          value: "sys-your-admin-token"
        - name: API_TOKENS_RAW
          value: "user1-abc123,user2-def456"
```

---

## Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_ADDR` | `0.0.0.0:10000` | Server listen address |
| `LOG_LEVEL` | `2` | klog verbosity level |
| `LEADER_NAME` | `agent-sandbox-leader` | Lease name for leader election |
| `SYSTEM_TOKEN` | `sys-2492a85b10ed4cb083b2c76b181eac96` | System admin token |
| `API_TOKENS_RAW` | `testuser-aef134ef-7aa1-945e-9399-7df9a4ad0c3f` | Comma-separated user tokens |
| `RATE_LIMIT_ENABLED` | `true` | Enable per-user rate limiting |
| `RATE_LIMIT_MAX_CONCURRENCY` | `10` | Max concurrent requests per user |
| `RATE_LIMIT_MAX_SANDBOX` | `100` | Max sandboxes per user |
| `RATE_LIMIT_USERS_RAW` | *(empty)* | JSON per-user rate limit overrides |
| `CONFIGMAP_NAME` | `agent-sandbox` | ConfigMap name for config storage |
| `SANDBOX_DEFAULT_IMAGE` | `ghcr.io/agent-infra/sandbox:latest` | Default sandbox image |
| `SANDBOX_DEFAULT_TEMPLATE` | `aio` | Default template name |
| `SANDBOX_BLUEPRINT_FILE` | `config/sandbox.yaml` | Bootstrap fallback path for the sandbox Blueprint |
| `SANDBOX_TEMPLATES_CONFIG_FILE` | `config/templates.json` | Bootstrap fallback path for the Template catalog |
| `ENV_NAME` | `dev` | Environment name label |
| `POOL_ENABLE` | `false` | Enable the Sandbox Pool syncer |
| `PAUSE_RESUME` | `false` | Enable Pause/Resume lifecycle |
| `TELEMETRY_ENABLED` | `false` | Enable lifecycle-event telemetry |
| `TELEMETRY_OTLP_ENDPOINT` | `victorialogs:9428` | OTLP backend endpoint |
| `TELEMETRY_OTLP_URL_PATH` | `/insert/opentelemetry/v1/logs` | OTLP URL path |
| `TELEMETRY_OTLP_INSECURE` | `true` | Use plain HTTP for OTLP |
