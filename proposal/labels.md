# Sandbox Labels

## Goal
Generic key-value labels on a sandbox, used to filter sandboxes on list.

## Data model
- **Source of truth: `sandbox-data`.** Labels live in `metadata["labels"]` of `sandbox-data`, like any other sandbox data.
  Everything else is an expansion derived from it, never edited on its own:
  - **ReplicaSet labels** are the filter index. A paused sandbox has no pod, and list filters ReplicaSets on the K8s side.
  - **Pod labels** are kept for observability only.
- **Expanded with a prefix** `sbx.label/`. `count=2` is stored as `sbx.label/count: "2"`.
  The API always shows keys without the prefix.
  The prefix stops user labels from overwriting system labels (`owner`, `sandbox`, `sbx-id`, `sbx-user`, `sbx-template`, `sbx-pool`).
- **Constraints** (K8s label rules, invalid input returns 400):
  - Key (without prefix) and value: at most 63 chars, `[A-Za-z0-9._-]`, must start and end with an alphanumeric char.
  - Values are always strings. Numbers are written as `"2"`.
  - At most 32 labels per sandbox.

- **Latest and previous configuration.**
  - `sandbox-data` always holds the latest sandbox configuration, with labels in `metadata.labels`.
  - On every user update, its previous value moves to `agent-sandbox.github.io/last-applied-configuration`.
    That annotation always holds exactly one version back.
  - Create has no previous version.
  - An update that changes nothing does not rotate.
  - System changes (pause, resume, process snapshot, pool, scalers) never touch either annotation.
  - The ReplicaSet object itself is not stored.

```yaml
metadata:
  annotations:
    sandbox-data: '{"name":"sbx-...","metadata":{"labels":"count=3,team=a"},...}'                                  # latest
    agent-sandbox.github.io/last-applied-configuration: '{"name":"sbx-...","metadata":{"labels":"count=2,team=a"},...}' # previous
  labels:
    sbx.label/team: "a"
    sbx.label/count: "2"
```

## API
No new SDK method is needed. Labels always travel in `metadata["labels"]`: in the SDK `metadata` argument on create and list, and in the `metadata` header on update.

### 1. Set on create
```python
Sandbox.create(template="sandbox-base", metadata={"labels": "team=a,count=0"})
```
The server validates `labels` and merges template defaults per key.
The result stays in `metadata["labels"]` in canonical form, sorted by key.
The pairs are also written to the ReplicaSet labels.
On a pool hit, they are written in `adaptReplicasetToSandbox` (ReplicaSet) and `updatePoolPodLabels` (pod).

### 2. Update
The e2b SDK has no "update metadata" method, so `create_snapshot` carries a `metadata` header.
The header value is the same metadata structure as in create, encoded as JSON.

```python
    sbx.create_snapshot(headers={"metadata": json.dumps(
        {"labels": "team=a,count=3", "conditions": "count=2,agentid=abc"}
    )})
```
- **Full replace.** Each request sends the complete label set. Keys left out are removed.
  `"labels": ""` clears all labels. Without `labels` in the header, labels are not changed.
- Only the sandbox owner can update; another user gets 404.
- An update with the same labels writes nothing.
- Concurrent updates: the last write wins.
- **Conditions.** Optional `conditions` makes the request conditional:
  ```python
  sbx.create_snapshot(headers={"metadata": json.dumps(
      {"labels": "team=a,count=3", "conditions": "count=2,agentid=abc"})})
  ```
  - K8s label selector syntax, the same as list (`count<3,!locked` works).
  - Matched against the current labels in `sandbox-data`, on the same read the label write is based on.
    A concurrent change in between makes the write conflict and the conditions are checked again.
  - Not met: 412, labels unchanged, no snapshot. Invalid: 400.
  - `conditions` without `labels`: checked only, labels unchanged, then the snapshot.
- **Order.** Labels are applied before the process snapshot is taken. If the labels are invalid, the server returns 400 and does not take the snapshot.

### 3. List / filter
```python
Sandbox.list(query=SandboxQuery(metadata={"labels": "team=a,count<3"}))
```
- `metadata["labels"]` uses **K8s label selector syntax** and is parsed by `labels.Parse`.
  Supported operators are `=`, `!=`, `in (...)`, `notin (...)`, `key`, `!key`, `<` and `>`.
  `<` and `>` need integer values.
- The server adds the prefix to the keys, then ANDs the selector with `sbx-user=<caller>`.
  A caller can never widen the result to other users' sandboxes.
- Other metadata keys keep exact-match semantics.

### 4. Read
`get_info()` and `list()` return `metadata["labels"]` exactly as stored in `sandbox-data`, sorted by key, for example
`"count=2,team=a"`. It is never rebuilt from ReplicaSet labels.
This string has the same format as the create input.

## Lifecycle notes
- An update writes `sandbox-data` first.
- It then re-derives from `sandbox-data` the ReplicaSet labels, `spec.template.metadata.labels` (so a resumed pod gets them) and the running pod.
- Pause/resume: `sandbox-data` and the ReplicaSet labels stay as they are.
- Templates may define default `labels` in their metadata. Sandbox values override template values for the same key.
  Saving templates with invalid labels is rejected.

## Test points
- Invalid key or value, a system key, or more than 32 labels returns 400.
- The selector cannot escape the `sbx-user` scope.
- Labels are applied on both the pool-hit path and the new-ReplicaSet path.
- A full replace removes the keys that are left out.
- Unmet conditions return 412 and change nothing.
- Labels survive pause and resume.
