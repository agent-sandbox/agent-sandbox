"""
User cases for sandbox labels (see proposal/labels.md): how an agent platform uses labels in practice.

  1. Tag on create        - every sandbox records who it belongs to and what it runs.
  2. Find and reconnect   - a restarted agent finds its sandbox by labels instead of creating a new one.
  3. Checkpoints          - an agent records its progress on each snapshot.
  4. Dashboard queries    - operators slice sandboxes with label selectors.
  5. Cleanup              - finished sandboxes are found by labels and killed.

Labels are maintained by the client: every update sends the full set (see patch_labels).
All sandboxes carry run=<RUN_ID>, so a run only sees its own sandboxes, and are killed at the end.
"""

"""
All Case output:

2026-11-05 17:37:16.817978 labels user cases, run=r15c625fd

== Case 1: tag sandboxes on create ==
  alice's sandbox 7ea94d43299849c988dd9846c2e20cfd metadata: {'labels': 'agent-id=coder,org-name=org-1,run=r15c625fd,state=running,user-name=alice', 'name': 'sbx-testuser-a-1793871436827-7ea94d43299849c988dd'}

== Case 2: find my sandbox and reconnect ==
  lookup user-name=alice,agent-id=coder -> ['7ea94d43299849c988dd9846c2e20cfd']
  command in the reconnected sandbox: reconnected
  lookup user-name=dave,agent-id=coder -> [], dave creates a new sandbox

== Case 3: checkpoints with a step counter ==
  checkpoint step=1 -> ok
  checkpoint step=2 -> ok
  checkpoint step=3 -> ok
  snapshot if step>2 -> ok

== Case 4: dashboard queries ==
  all sandboxes of org-1       org-name=org-1               -> 2
  coder agents in any org      agent-id=coder               -> 2
  everything except org-2      org-name!=org-2              -> 2
  alice or bob                 user-name in (alice,bob)     -> 2
  sandboxes past step 2        step>2                       -> 1

== Case 5: cleanup by labels ==
  state=done -> ['33ebc04b05134f00b98230c77fab241d'], killing them
  left: state=done -> 0, state=running -> 2

Process finished with exit code 0
"""

import json
import re
import uuid
from datetime import datetime
from typing import Dict, List, Optional

import config
from e2b.exceptions import AuthenticationException, NotFoundException
from e2b.sandbox.sandbox_api import SandboxQuery
from e2b_code_interpreter import Sandbox

config.init_config()

TEMPLATE = "sandbox-base"
TIMEOUT = 10 * 60

RUN_ID = "r" + uuid.uuid4().hex[:8]
ORG = "org-1"


# ---------------------------------------------------------------------------
# Entry
# ---------------------------------------------------------------------------

def main():
    """Play all user cases in order, then kill every sandbox created by this run."""
    print(f"{datetime.now()} labels user cases, run={RUN_ID}")
    try:
        alice = case_tag_on_create()
        case_find_and_reconnect()
        case_checkpoints(alice)
        case_dashboard()
        case_cleanup()
    finally:
        for sbx in created:
            try:
                sbx.kill()
            except Exception:
                pass  # already killed by the cleanup case


# ---------------------------------------------------------------------------
# Case 1: tag on create
# ---------------------------------------------------------------------------

def case_tag_on_create() -> Sandbox:
    """A platform creates one sandbox per agent session and tags it with owner, org and agent.

    Returns alice's sandbox, which later cases keep using.
    """
    step("Case 1: tag sandboxes on create")
    alice = create(**{"user-name": "alice", "org-name": ORG, "agent-id": "coder", "state": "running"})
    create(**{"user-name": "bob", "org-name": ORG, "agent-id": "reviewer", "state": "running"})
    create(**{"user-name": "carol", "org-name": "org-2", "agent-id": "coder", "state": "running"})
    say(f"alice's sandbox {alice.sandbox_id} metadata: {alice.get_info().metadata}")
    return alice


# ---------------------------------------------------------------------------
# Case 2: find and reconnect
# ---------------------------------------------------------------------------

def case_find_and_reconnect():
    """An agent process restarted and lost the sandbox id: it looks its sandbox up by labels and reconnects.

    Only when nothing matches does it create a new sandbox (get-or-create).
    """
    step("Case 2: find my sandbox and reconnect")
    found = find("user-name=alice,agent-id=coder")
    say(f"lookup user-name=alice,agent-id=coder -> {ids(found)}")
    sbx = Sandbox.connect(found[0].sandbox_id)
    say(f"command in the reconnected sandbox: {sbx.commands.run('echo reconnected').stdout.strip()}")

    say(f"lookup user-name=dave,agent-id=coder -> {ids(find('user-name=dave,agent-id=coder'))}, "
        f"dave creates a new sandbox")


# ---------------------------------------------------------------------------
# Case 3: checkpoints
# ---------------------------------------------------------------------------

def case_checkpoints(sbx: Sandbox):
    """An agent snapshots after each task step and records the step in the labels.

    Each checkpoint requires the previous step (step=N-1), so a duplicate or stale writer,
    e.g. a retried request of an old process, cannot move the progress back.
    """
    step("Case 3: checkpoints with a step counter")
    sid = sbx.sandbox_id
    patch_labels(sid, {"step": "0"})
    for n in range(1, 4):
        sbx.commands.run(f"echo step {n} > /tmp/progress")
        code = patch_labels(sid, {"step": str(n)}, conditions=f"step={n - 1}")
        say(f"checkpoint step={n} -> {'ok' if code is None else f'HTTP {code}'}")

    # Snapshot only once enough progress is made: a condition with no label change.
    code = snapshot(sid, {"conditions": "step>2"})
    say(f"snapshot if step>2 -> {'ok' if code is None else f'HTTP {code}'}")


# ---------------------------------------------------------------------------
# Case 4: dashboard queries
# ---------------------------------------------------------------------------

def case_dashboard():
    """Operators slice the sandboxes with label selectors, the same syntax as kubectl -l."""
    step("Case 4: dashboard queries")
    queries = {
        f"all sandboxes of {ORG}": f"org-name={ORG}",
        "coder agents in any org": "agent-id=coder",
        "everything except org-2": "org-name!=org-2",
        "alice or bob": "user-name in (alice,bob)",
        "sandboxes past step 2": "step>2",
    }
    for title, selector in queries.items():
        say(f"{title:28} {selector:28} -> {len(find(selector))}")


# ---------------------------------------------------------------------------
# Case 5: cleanup
# ---------------------------------------------------------------------------

def case_cleanup():
    """Finished sessions are marked state=done; a janitor finds everything with state=done and kills it."""
    step("Case 5: cleanup by labels")
    for item in find("user-name=bob"):
        patch_labels(item.sandbox_id, {"state": "done"})
    done = find("state=done")
    say(f"state=done -> {ids(done)}, killing them")
    for item in done:
        Sandbox.kill(item.sandbox_id)
    say(f"left: state=done -> {len(find('state=done'))}, state=running -> {len(find('state=running'))}")


# ---------------------------------------------------------------------------
# Helpers: labels
# ---------------------------------------------------------------------------

def parse(raw: Optional[str]) -> Dict[str, str]:
    """Parse a stored labels string "k1=v1,k2=v2" into a dict."""
    return dict(pair.split("=", 1) for pair in raw.split(",")) if raw else {}


def fmt(labels: Dict[str, str]) -> str:
    """Render labels as "k1=v1,k2=v2" sorted by key, the form the server stores."""
    return ",".join(f"{k}={labels[k]}" for k in sorted(labels))


def read_labels_by_id(sandbox_id: str) -> Dict[str, str]:
    """Current labels of a sandbox by id."""
    return parse(Sandbox.get_info(sandbox_id).metadata.get("labels"))


def patch_labels(sandbox_id: str, changes: Dict[str, Optional[str]], conditions: str = "") -> Optional[int]:
    """Client-side patch: read the labels, apply changes (None removes a key) and send the full set.

    Pass conditions to make it a compare-and-set. Returns None on success, else the HTTP status.
    """
    labels = read_labels_by_id(sandbox_id)
    for k, v in changes.items():
        if v is None:
            labels.pop(k, None)
        else:
            labels[k] = v
    header = {"labels": fmt(labels)}
    if conditions:
        header["conditions"] = conditions
    return snapshot(sandbox_id, header)


# ---------------------------------------------------------------------------
# Helpers: SDK calls
# ---------------------------------------------------------------------------

created: List[Sandbox] = []  # every sandbox of this run, killed at the end of main


def create(**labels: str) -> Sandbox:
    """Create a sandbox with the given labels plus run=<RUN_ID>, and track it for cleanup."""
    sbx = Sandbox.create(template=TEMPLATE, timeout=TIMEOUT,
                         metadata={"labels": fmt({"run": RUN_ID, **labels})})
    created.append(sbx)
    return sbx


def find(selector: str) -> list:
    """Sandboxes of this run matching a label selector, across all pages."""
    paginator = Sandbox.list(query=SandboxQuery(metadata={"labels": f"run={RUN_ID},{selector}"}))
    items = []
    while paginator.has_next:
        items.extend(paginator.next_items())
    return items


def snapshot(sandbox_id: str, header: dict) -> Optional[int]:
    """create_snapshot with a JSON "metadata" header. Returns None on success, else the HTTP status."""
    try:
        Sandbox.create_snapshot(sandbox_id, headers={"metadata": json.dumps(header)})
        return None
    except Exception as e:
        return status_of(e)


def status_of(e: Exception) -> int:
    """HTTP status of an SDK error. The SDK formats API errors as "<status>: <message>".

    An error without a status is not an API answer, so it is raised instead of being mistaken for success.
    """
    if isinstance(e, NotFoundException):
        return 404
    if isinstance(e, AuthenticationException):
        return 401
    m = re.match(r"\s*(\d{3}):", str(e))
    if not m:
        raise e
    return int(m.group(1))


def ids(items: list) -> List[str]:
    """Sorted sandbox ids of list results."""
    return sorted(i.sandbox_id for i in items)


# ---------------------------------------------------------------------------
# Helpers: output
# ---------------------------------------------------------------------------

def step(title: str):
    """Start a case: print its title."""
    print(f"\n== {title} ==")


def say(message: str):
    """Print what the user case is doing."""
    print(f"  {message}")


if __name__ == "__main__":
    main()
