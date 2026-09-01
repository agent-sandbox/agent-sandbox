---
name: ai-deploy
description: >
  Deploy a generated project (one process or several) to agent-sandboxes
  with scale-to-zero: idle sandboxes pause and auto-resume (process
  restarted) on the next HTTP request. Use when asked to deploy, host, or
  preview generated code through the sandbox platform.
---

# AI Deploy — Continuous Deployment to Sandboxes

Ship a project you just generated into agent-sandboxes via the e2b SDK,
get a public URL, and let the platform pause/resume it on demand. No
wrapper script — write SDK calls inline, adapted to the project at hand.
Most steps below are optional; skip what this project doesn't need.

A project may be one process or several (API, worker, UI, ...). Each
deploys to its **own sandbox**, on whichever template matches its runtime
(`sandbox-base` for stdlib Python, `sandbox-base-node` for Node, ...),
with its own `sandbox_id`. If one service calls another, use the
platform's internal cluster address, not the public gateway.

## Setup

```bash
pip install e2b==2.21.1 e2b-code-interpreter==2.4.1 python-dotenv
```

`.env`:

```bash
E2B_API_KEY=<key>
E2B_DOMAIN=<domain>
E2B_API_URL=http://agent-sandbox.<domain>/e2b/v1
```

## Writing the project

- Multiple services → separate directories, separate sandboxes.
- Servers must bind `0.0.0.0`.
- Address the persistent working directory (`/workspace`) explicitly in
  every file write and command.
- Served under a sub-path (`/sandboxes/router/{id}/{port}/`), so any
  browser-facing assets need relative paths: `vite build --base ./`, CRA
  `"homepage": "."`, plain `./app.js`-style references.
- Prefer stdlib-only implementations when possible — no install step,
  nothing to redo on resume. Dependencies are fine too — see step 6.
- Printed URLs should still be `https://` regardless of `E2B_API_URL`'s
  own scheme — see `public_base_url()` below.

## The pipeline

Every deploy — first time or redeploy — walks the same ten steps. Several
are optional; skip the ones this project doesn't need.

```
1. generate project  →  2. build  →  3. push to git
  →  4. create/reuse sandbox  →  5. get the code onto it
  →  6. install deps  →  7. start  →  8. snapshot
  →  9. save deploy state  →  10. push to git
```

This is meant to be **re-run** on every code change: it reuses the
service's existing sandbox (same `sandbox_id`, same public URL) instead
of creating a new one, unless the recorded sandbox is gone.

Reuse depends on a state file (`<project>/.ai-deploy-state.json` —
anchor it to the project, not the caller's cwd) holding each service's
`sandbox_id` and last start command. **Commit it with the project**
(don't gitignore it) — it's what lets a redeploy from any session or
machine continue the same live sandboxes instead of forking new ones.

### 1. Generate the project

Write the source files into the project's own directory.

### 2. Build — optional

Only if the project has a build step, e.g. `npm run build` / `vite build`
→ `dist/`. Upload the *output* directory, not the source tree. Skip
entirely for a plain stdlib script or server.

### 3. Push to git — optional

If the project has a remote, commit and push the generated files now —
this is what the git-based upload in step 5 clones/pulls from. Skip it
if uploading via the files API instead.

### 4. Create or reuse the sandbox

```python
import json
import os
from pathlib import Path
from urllib.parse import urlsplit, urlunsplit
from dotenv import load_dotenv
from e2b import CommandExitException, SandboxNotFoundException
from e2b_code_interpreter import Sandbox

load_dotenv()
idle_timeout = 600  # seconds
WORKDIR = "/workspace"    # sandbox inner persistent mount; always address it explicitly

# Absolute path — a relative one breaks "reuse the existing sandbox"
# silently if this script ever runs from a different cwd.
PROJECT_DIR = Path("/path/to/the/project").resolve()  # the project you just wrote
STATE_FILE = PROJECT_DIR / ".ai-deploy-state.json"  # sandbox_id + start_cmd per service


def public_base_url():
    # Force https regardless of E2B_API_URL's own scheme.
    parts = urlsplit(os.environ["E2B_API_URL"])
    return urlunsplit(("https", parts.netloc, "", "", ""))


def load_state():
    return json.loads(STATE_FILE.read_text()) if STATE_FILE.exists() else {}


def save_state(state):
    STATE_FILE.write_text(json.dumps(state, indent=2))


def get_or_create_sandbox(state, key, template, idle_timeout, envs=None):
    """Reuse the recorded sandbox if it still exists, else create one.
    Returns (sandbox, service_state_entry, created)."""
    entry = state.setdefault(key, {})
    sandbox_id = entry.get("sandbox_id")
    if sandbox_id:
        try:
            return Sandbox.connect(sandbox_id), entry, False  # reused
        except SandboxNotFoundException:
            pass  # deleted/expired — fall through and create a new one

    sbx = Sandbox.create(
        template=template,
        timeout=-1,  # no hard lifetime; idle timeout owns reclamation
        metadata={"idleTimeout": str(idle_timeout)},
        lifecycle={"on_timeout": "pause", "auto_resume": True},
        envs=envs
    )
    entry["sandbox_id"] = sbx.sandbox_id
    return sbx, entry, True  # freshly created
```

### 5. Get the code onto the sandbox: files vs. git

Pick per service, based on size.

**Files API** — simplest, no remote needed. Good for a handful of files:

```python
def upload_dir(sbx, local_dir, workdir, skip=()):
    for local in sorted(local_dir.rglob("*")):
        if not local.is_file():
            continue
        rel = local.relative_to(local_dir).as_posix()
        if rel in skip:
            continue
        with open(local, "rb") as file:
            sbx.files.write(f"{workdir}/{rel}", file)
```

**Git clone/pull** — better for many files (large source tree, a built
frontend). Requires the project already pushed to a remote (step 3):

```python
def sync_via_git(sbx, workdir, repo_url, branch=None, username=None, password=None):
    if sbx.files.exists(f"{workdir}/.git"):
        sbx.git.pull(workdir, branch=branch, username=username, password=password)
    else:
        sbx.git.clone(repo_url, path=workdir, branch=branch,
                       username=username, password=password)
```

Never commit `node_modules`/`venv` to carry dependencies this way —
install them inside the sandbox instead (step 6).

### 6. Install dependencies — optional

Skip for a stdlib-only service. Otherwise, run the install command
against the working directory like any other command:

```python
def install_deps(sbx, workdir, cmd, timeout=300):
    sbx.commands.run(cmd, cwd=workdir, timeout=timeout)

# install_deps(sbx, WORKDIR, "pip install -r requirements.txt")
# install_deps(sbx, WORKDIR, "npm install --omit=dev")
```

### 7. Start the service

Kill any previous run of this service — `files.write()` alone doesn't
make a running process pick up new code — start the fresh one with
output redirected to a log file, then verify it's actually serving. 
The sandbox has no `ps` command; use `sbx.commands.list()` to see what's running instead:

```python
def restart_service(sbx, workdir, match, start_cmd, port, log_file="service.log"):
    for proc in sbx.commands.list():
        if proc.cwd == workdir and any(match in arg for arg in proc.args):
            sbx.commands.kill(proc.pid)

    sbx.commands.run(f"{start_cmd} > {log_file} 2>&1", background=True, timeout=0, cwd=workdir)
    try:
        sbx.commands.run(f"curl -sf --retry 30 --retry-delay 1 --retry-all-errors "
                          f"-o /dev/null http://localhost:{port}/")
    except CommandExitException:
        log = sbx.files.read(f"{workdir}/{log_file}")
        raise RuntimeError(f"{match} failed to start on port {port}, log:\n{log}") from None
```

### 8. Snapshot — optional, only if the start command changed

Retake only when the *command line* changes (new entry file, port, or
flags) — a resumed sandbox re-runs the recorded command from scratch and
picks up whatever's currently on disk, so a code-only redeploy doesn't
need one:

```python
def maybe_snapshot(sbx, entry, start_cmd, created):
    if created or entry.get("start_cmd") != start_cmd:
        sbx.create_snapshot()
    entry["start_cmd"] = start_cmd
```

### 9. Save deploy state

Persist `sandbox_id`/`start_cmd` every run, not just the first deploy —
step 8's comparison depends on it staying current.

### 10. Push to git — optional

If step 3 pushed, push again now including the updated
`.ai-deploy-state.json` — a checkout from any machine then has both the
latest code and the sandbox IDs it's already running on.

### Putting it together

`PROJECT_DIR` is the project root and anchors the one shared
`STATE_FILE`. For a single service, it's also the service's
source directory:

```python
state = load_state()
base = public_base_url()
py_deps = f"{WORKDIR}/pylibs"
sbx, entry, created = get_or_create_sandbox(state, "service", "sandbox-base", idle_timeout, envs={"PYTHONPATH": py_deps})
upload_dir(sbx, PROJECT_DIR, WORKDIR)                       # step 5 (files variant)
# install_deps(sbx, WORKDIR, "pip install -r requirements.txt")  # step 6, if needed
start_cmd = "python3 server.py"
restart_service(sbx, WORKDIR, match="server.py", start_cmd=start_cmd, port=8000)  # step 7
maybe_snapshot(sbx, entry, start_cmd, created)              # step 8
save_state(state)                                           # step 9

url = f"{base}/sandboxes/router/{sbx.sandbox_id}/8000/"
print("sandbox:", sbx.sandbox_id)
print("url:", url)
```

If one service calls another, deploy the dependency first and feed its
**internal** address —
`http://agent-sandbox/sandboxes/router/{sandbox_id}/{port}/`, not the
public `https://` URL — into however the caller reads its config (env
var, config file, whatever fits). 

## Sample

```
sample-app/
├── .ai-deploy-state.json   # shared state — "backend"/"ui" keys
├── api/
│   └── server.py           # stdlib JSON API, :8000
└── ui/
    ├── server.js            # static file server + /api/* proxy, :3000
    ├── index.html
    ├── logo.svg
    └── config.json          # {"apiBase": ...} — written by the deploy script
```

`sample-app/` deploys **two** services, showing the "multiple services"
and "one calls another" patterns together: a stdlib Python API
(`api/server.py`, :8000) and a small site (`ui/`, :3000, Tailwind via CDN,
no build step) that fetches `/api/hello`. `ui/server.js` serves the
static files and proxies `/api/*` to the backend's internal address —
read from `config.json`, written by the deploy script once the backend
sandbox exists — so the browser only ever sees same-origin requests.
Both services use the files-API upload (small, no git needed) with no
build or install step.

Deploy with `PROJECT_DIR = Path("./sample-app").resolve()` — both
services share `sample-app/.ai-deploy-state.json` under the
`"backend"`/`"ui"` keys — uploading from `PROJECT_DIR / "api"` and
`PROJECT_DIR / "ui"` per the "Multiple services" pattern above.

### .ai-deploy-state.json
```json
{
  "backend": {
    "sandbox_id": "e48864184f2f42e898ef52246d0f1050",
    "start_cmd": "python3 server.py"
  },
  "ui": {
    "sandbox_id": "44a8b436893b4fdc988929967889eadb",
    "start_cmd": "node server.js"
  }
}
```


## E2B SDK reference

- [`Sandbox`](https://e2b.dev/docs/sdk-reference/python-sdk/v2.28.2/sandbox_sync#sandbox) — create/connect/pause/snapshot
- [`commands`](https://e2b.dev/docs/sdk-reference/python-sdk/v2.28.2/sandbox_sync#commands) — run/list/kill processes
- [`git`](https://e2b.dev/docs/sdk-reference/python-sdk/v2.28.2/sandbox_sync#git) — clone/pull/push inside the sandbox (step 5, git variant)
- [`filesystem`](https://e2b.dev/docs/sdk-reference/python-sdk/v2.28.2/sandbox_sync#filesystem) — read/write/exists (step 5, files variant; log retrieval in step 7)
