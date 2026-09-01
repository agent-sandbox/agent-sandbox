---
icon: lucide/hard-drive
---

# Shared Network Storage for Sandboxes

Sandboxes are ephemeral — a ReplicaSet's Pod can be recreated, paused/resumed,
or scaled to zero at any time, and its container filesystem doesn't survive
that. For workloads that need data to persist, mount an external network volume instead
of relying on local disk.

Agent-Sandbox doesn't hard-code a storage backend. Instead, the
[blueprint](../blueprint.md) can read a piece of **sandbox metadata** and
decide whether (and how) to attach a volume. This guide walks through the
pattern using NFS, then covers two variations you can adapt to your own
infrastructure.

## The pattern

1. A sandbox carries a metadata key, e.g. `shareDataNFS`, whose value is the
   NFS server address.
2. The blueprint checks for that key with `{{ if index .Sandbox.Metadata
   "shareDataNFS" }}`. If it's present, the blueprint adds a `volumeMounts`
   entry and a matching `volumes` entry; if it's absent, the sandbox is
   created exactly as before — no volume at all.
3. The NFS server address itself comes from the metadata value via
   `{{.Sandbox.Metadata.shareDataNFS}}`, so the blueprint doesn't need to
   hard-code a server IP.

```yaml title="sandbox.yaml (blueprint) — inside spec.template.spec.containers[0]"
{{ if index .Sandbox.Metadata "shareDataNFS" }}
          workingDir: /workspace
          volumeMounts:
            - mountPath: /workspace
              name: data-nfs
              subPath: sandboxes-data/{{.Sandbox.Name}}
      volumes:
        - name: data-nfs
          nfs:
            path: /data
            server: {{.Sandbox.Metadata.shareDataNFS}}
{{ end }}
```

Two details worth calling out:

- **`index .Sandbox.Metadata "shareDataNFS"`, not `.Sandbox.Metadata.shareDataNFS`,
  for the condition.** `.Sandbox.Metadata` is a `map[string]string`. Go
  templates panic on `.Field` access into a map for a key that doesn't
  parse as a plain identifier.
- **`subPath: sandboxes-data/{{.Sandbox.Name}}` gives every sandbox its own
  directory on the same NFS export.** All sandboxes share one `nfs.path`
  (`/data`), but each one only ever sees its own
  `sandboxes-data/<sandbox-name>/` subtree mounted at `/workspace` — so
  sandboxes don't read or overwrite each other's files, but a per-user or
  per-agent job can still land its output where the next sandbox created
  for the same purpose will find it.
- **`/workspace`**
    Default `/workspace` is subtree mounted in sandbox. If a workload needs its installed packages to
    survive across restarts — not just user data — install them
    *into* `/workspace` explicitly: e.g. `pip install
    --target=/workspace/...`, a Node project's `node_modules` created with
    `/workspace` as the working directory, and so on. Anything left outside
    `/workspace` has to be reinstalled from scratch on every fresh Pod.

## Example
### Setting the metadata

`shareDataNFS` is just a regular metadata key — set it wherever you'd set
any other sandbox metadata:

**Per-sandbox, at creation time**, via the E2B SDK or REST API. Use this
when different sandboxes should land on different NFS servers or shares:

```python
sandbox = Sandbox.create(
    template="openclaw",
    metadata={"shareDataNFS": "100.100.100.100"},
)
```

**Template-wide, as a default**, in `templates.json`. Every sandbox created
from that template gets the volume without the caller having to ask for it:

```json title="templates.json"
{
  "name": "sandbox-base",
  "image": "ghcr.io/agent-sandbox/sandbox-base:latest",
  "port": 49983,
  "metadata": {
    "shareDataNFS": "100.100.100.100"
  },
  "description": "sandbox base with a shared NFS workspace"
}
```

**Both at once.** The controller merges the two, and metadata from the
create request always wins over the template's default for the same key —
so a template can set a sane default NFS server, and an individual sandbox
can still override it by passing its own `shareDataNFS` value at creation
time.

### Whole sandbox blueprint
```yaml
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: {{.Sandbox.Name}}
  namespace: {{.Namespace}}
  annotations:
    sandbox-data:  |
      {{.RawData}}
  labels:
    owner: agent-sandbox
    sandbox: {{.Sandbox.Name}}
    sbx-id: {{.Sandbox.ID}}
    sbx-user: {{.Sandbox.User}}
    sbx-template: {{.Sandbox.Template}}
    sbx-pool: {{.Sandbox.IsPool}}
spec:
  replicas: 1
  selector:
    matchLabels:
      sandbox: {{.Sandbox.Name}}
  template:
    metadata:
      labels:
        sandbox: {{.Sandbox.Name}}
        owner: agent-sandbox
        sbx-id: {{.Sandbox.ID}}
        sbx-user: {{.Sandbox.User}}
        sbx-template: {{.Sandbox.Template}}
        sbx-pool: {{.Sandbox.IsPool}}
    spec:
      containers:
        - name: sandbox
          image: {{.Sandbox.Image}}
  {{if .Sandbox.Cmd}}
          command: [{{.Sandbox.Cmd}}]
  {{end}}
  {{if .Sandbox.Args}}
          args:
      {{range .Sandbox.Args }}
            - {{ . }}
      {{end }}
  {{end}}
          imagePullPolicy: IfNotPresent
          ports:
            - containerPort: {{.Sandbox.Port}}
          env:
            - name: INSTANCE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
  {{if .Sandbox.EnvVars}}
      {{range $name, $value := .Sandbox.EnvVars }}
            - name: {{$name}}
              value: {{printf "%q" $value}}
      {{end }}
  {{end}}
          resources:
            requests:
              cpu: {{.Sandbox.CPU}}
              memory: {{.Sandbox.Memory}}
            limits:
              cpu: {{.Sandbox.CPULimit}}
              memory: {{.Sandbox.MemoryLimit}}
{{if not .Sandbox.TemplateObj.NoStartupProbe}}
          startupProbe:
            failureThreshold: 600
            tcpSocket:
              port: {{.Sandbox.Port}}
            periodSeconds: 1
            successThreshold: 1
            timeoutSeconds: 3
{{end}}
{{ if index .Sandbox.Metadata "shareDataNFS" }}
          workingDir: /workspace
          volumeMounts:
            - mountPath: /workspace
              name: data-nfs
              subPath: sandboxes-data/{{.Sandbox.Name}}
      volumes:
        - name: data-nfs
          nfs:
            path: /data
            server: {{.Sandbox.Metadata.shareDataNFS}}
{{ end }}
```

### PV&PVC instead of an inline NFS volume

The inline `nfs:` volume above talks to the NFS server directly, with no
Kubernetes storage object in between. If you'd rather go through a
`PersistentVolumeClaim` — to get quota enforcement, a `StorageClass`, or
just to keep the server address out of the blueprint — pre-create a
`PersistentVolume`/`PersistentVolumeClaim` pair once, and have the
blueprint mount the claim instead:

```yaml title="one-time setup"
apiVersion: v1
kind: PersistentVolume
metadata:
  name: data-nfs-pv
spec:
  capacity:
    storage: 100Gi
  volumeMode: Filesystem
  accessModes:
    - ReadWriteMany
  persistentVolumeReclaimPolicy: Retain
  nfs:
    path: /data
    server: 100.100.100.100
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: data-nfs-pvc
  namespace: {{.Namespace}}
spec:
  storageClassName: ""
  accessModes:
    - ReadWriteMany
  resources:
    requests:
      storage: 10Gi
  volumeName: data-nfs-pv
```

```yaml title="sandbox.yaml (blueprint)"
{{ if index .Sandbox.Metadata "shareDataNFS" }}
          workingDir: /workspace
          volumeMounts:
            - mountPath: /workspace
              name: data-nfs
              subPath: sandboxes-data/{{.Sandbox.Name}}
      volumes:
        - name: data-nfs
          persistentVolumeClaim:
              claimName: data-nfs-pvc
{{ end }}
```

Note that in this variant the metadata value no longer selects *which*
server to use — the PV already pins that — so `shareDataNFS` acts purely as
an on/off switch for whichever sandboxes should get the volume. If you need
different sandboxes to land on genuinely different servers or shares, use
the inline `nfs:` form above, or provision one PV/PVC pair per share and
branch on the metadata value (e.g. `{{ if eq (index .Sandbox.Metadata
"shareDataNFS") "fast-tier" }}`).

## Not just NFS

The mechanism is generic, Swap the `nfs:`
block for any other Kubernetes volume type — a CSI driver, a `PVC` (see the
pre section), even a `hostPath` — and the same
`{{ if index .Sandbox.Metadata "shareDataNFS" }}` / `{{ end }}` gate still
works unchanged:

```yaml title="sandbox.yaml (blueprint) — any CSI-backed volume"
{{ if index .Sandbox.Metadata "shareDataNFS" }}
          workingDir: /workspace
          volumeMounts:
            - mountPath: /workspace
              name: data-vol
      volumes:
        - name: data-vol
          csi:
            driver: your.csi.driver
            volumeAttributes:
              server: {{.Sandbox.Metadata.shareDataNFS}}
{{ end }}
```

The metadata key's name (`shareDataNFS`) and what its value means (an NFS
server address) are just this repo's example — rename the key and repurpose
the value for whatever your storage backend needs. What matters is the
`if`/`end` gate itself: it's the one thing every variation in this guide
has in common, and it's how you make a piece of storage configuration
opt-in per template or per sandbox instead of baked into every sandbox
unconditionally.

## See also
- [Blueprint](../blueprint.md) — how `sandbox.yaml` is rendered, and how
  hot reload works
- [Templates](../templates.md) — the `metadata` field on a template
  definition