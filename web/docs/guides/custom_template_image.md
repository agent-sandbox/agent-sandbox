---
icon: lucide/container
---

# Building a Custom Template Image

A [template](../templates.md) is mostly just a container image plus a bit of
metadata (`port`, `resources`, `pool`, ...). You are not limited to the
images this repo ships — any image you can build with `docker build` can
become a template, as long as it satisfies one requirement.

## The only hard requirement: `envd`

`envd` is the small agent binary that the E2B protocol talks to inside every
sandbox. It's what makes `commands.run(...)`, file upload/download, the
in-browser terminal, and process snapshotting (used by Pause/Resume) work —
and the Agent-Sandbox controller
itself calls into it directly (on its fixed port, `49983`) to snapshot a
sandbox's running processes before a pause. Without `envd` running, a
sandbox isn't an Agent-Sandbox sandbox — it's just a container.

If your workload doesn't need `run_code` (see below), `envd` is genuinely
all you need. [`templates/sandbox-base`](https://github.com/agent-sandbox/agent-sandbox/tree/main/templates/sandbox-base)
is the minimal example — trimmed down, it's:

```dockerfile title="Dockerfile"
FROM python:3.12-slim-trixie

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl sudo tini \
    && rm -rf /var/lib/apt/lists/*

COPY envd-0.2.10 /usr/bin/envd
RUN chmod +x /usr/bin/envd

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 49983
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
CMD []
```

`entrypoint.sh` just starts `envd` on `49983` and then either waits on it
(no user command) or execs whatever command the sandbox was created with,
forwarding termination signals so it shuts down cleanly. `envd` itself is a
prebuilt binary — don't compile it yourself, copy the version-pinned binary
straight out of any of the `templates/*` directories in this repo (e.g.
`templates/sandbox-base/envd-0.2.10`) into your own image.

[`templates/sandbox-base-node`](https://github.com/agent-sandbox/agent-sandbox/tree/main/templates/sandbox-base-node)
is the same pattern on `node:22-trixie-slim` instead of `python:3.12-slim-trixie`
— identical `entrypoint.sh`, different base image. That's the whole
customization surface for this class of template: **swap the base image
and whatever you `apt`/`pip`/`npm` install on top of it; keep `envd` and
`entrypoint.sh` as-is.** File I/O, shell commands, and the terminal all work
immediately.

## Adding `run_code` support

`run_code` is a separate, higher-level capability — It's served by its own small HTTP API that a Jupyter kernel sits
behind, running on its own port alongside `envd`.

[`templates/sandbox-base-code`](https://github.com/agent-sandbox/agent-sandbox/tree/main/templates/sandbox-base-code)
adds exactly that on top of the minimal pattern above:

- **`code-server.py`** — a small FastAPI app that proxies to a Jupyter
  kernel over its websocket API. It exposes `POST /execute`, plus
  `POST /contexts`, `GET /contexts`, `POST /contexts/{id}/restart`, and
  `DELETE /contexts/{id}` for managing execution contexts (a context is
  what keeps variables alive between two `run_code` calls), and
  `GET /health`.
- **`requirements.txt`** — `jupyter-server`, `ipykernel`, `fastapi`,
  `uvicorn`, and the couple of HTTP/websocket libraries `code-server.py`
  needs to talk to Jupyter.
- **`entrypoint.sh`** additionally starts a Jupyter server (bound to
  `127.0.0.1` only — it's never reachable directly) and then, once no user
  command is given, runs `code-server.py` under `uvicorn` as the foreground
  process on `CODE_INTERPRETER_PORT` (`49999` by default). `envd` keeps
  running the same as before, on its own port.

```dockerfile title="Dockerfile — additions on top of sandbox-base"
COPY code-server.py /opt/code-interpreter/server.py
COPY requirements.txt /opt/code-interpreter/requirements.txt
RUN pip install -r /opt/code-interpreter/requirements.txt

EXPOSE 49983
EXPOSE 49999
```

The `code-server.py` implementation of the `run_code` protocol. Added to the image and started from
`entrypoint.sh`, running next to `envd`.

## The `port` field and readiness

A template's `port` field (see [Templates → Template Fields](../templates.md#template-fields))
becomes `.Sandbox.Port` in the [blueprint](../blueprint.md), which sets the
container's `containerPort` and the `startupProbe` the platform waits on
before it considers the sandbox ready. Set it to whichever port is your
image's "main" one:

- No extra service beyond `envd` → set `port` to `49983` (envd's own port).
- `run_code` support → set `port` to the code-interpreter API's port
  (`49999`, as `sandbox-base-code` does).

Any *other* port your image listens on is still reachable at runtime
regardless of what `port` is set to — E2B's per-port routing
(`https://{port}-{sandboxID}.your-domain.com`, see
[E2B SDK Workarounds](e2b_workarounds.md) for the non-wildcard-domain
fallback) reaches any port, not just the one in the template. `port` only
controls what the platform waits on during creation, not what's reachable
afterward.

## Registering the image as a template

Build and push the image, then point a template entry at it:

```bash
docker build -t your-registry/my-custom-sandbox:0.1.0 .
docker push your-registry/my-custom-sandbox:0.1.0
```

```json title="templates.json"
{
  "name": "my-custom-sandbox",
  "image": "your-registry/my-custom-sandbox:0.1.0",
  "port": 49983,
  "description": "Custom sandbox image with <whatever you added>"
}
```

See [Templates](../templates.md) for the full field reference (resources,
pool warmup, dynamic templates, metadata), and
[Blueprint → Hot Reload](../blueprint.md#hot-reload) — updating
`templates.json` via the API or UI takes effect immediately, no restart
needed.

## Bring your own image

None of this is specific to Python or to code execution — the same
`envd`-plus-optional-extra-service shape works for a browser image, a
desktop/VNC image, a specific language toolchain, or an image wrapping your
own internal tooling. If you want ideas for what other kinds of sandbox
images people build, the [kubernetes-sigs/agent-sandbox example
gallery](https://agent-sandbox.sigs.k8s.io/docs/use-cases/examples/) and [e2b-cookbook](https://github.com/e2b-dev/e2b-cookbook/tree/main/examples) is a
good source of inspiration e.g. [claude-code interpreter](https://github.com/e2b-dev/e2b-cookbook/blob/main/examples/claude-code-interpreter-python/claude_code_interpreter.ipynb) — different project, same underlying idea of
"a sandbox is just a container with an agent-facing API inside it."

## See also

- [Templates](../templates.md) — template fields, dynamic templates, pool
  configuration
- [Blueprint](../blueprint.md) — how a template's image becomes a running
  ReplicaSet
- [Shared Network Storage](network_storage.md) — mounting external storage
  into a template's containers
