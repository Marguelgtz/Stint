#!/usr/bin/env python3
"""Publish verified on-box Deep Work checkpoints to GitHub.

This helper runs only on the compute instance. It reads durable Deep Work state,
pushes immutable checkpoint branches, creates or updates PRs, inventories and
retrieves review context, queues merge requests, and lets the supervisor execute
deterministic SHA-locked merges. Authentication is read from a root-only token
file; the token is never written to git config or command arguments.
"""
from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import re
import socket
import subprocess
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

SAFE_SESSION = re.compile(r"^[A-Za-z0-9_-]+$")
SAFE_REPOSITORY = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def read_token(path: str) -> str:
    text = Path(path).read_text(encoding="utf-8").strip()
    if not text:
        raise RuntimeError("GitHub token file is empty")
    for raw in text.splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line[7:].strip()
        if "=" in line:
            key, value = line.split("=", 1)
            if key.strip() in {"GITHUB_TOKEN", "GH_TOKEN", "STINT_GITHUB_TOKEN"}:
                return value.strip().strip('"').strip("'")
        elif len(text.splitlines()) == 1:
            return line
    raise RuntimeError("GitHub token file must contain a token or GITHUB_TOKEN=/GH_TOKEN= entry")


def config() -> dict:
    token_file = os.environ.get("STINT_GITHUB_TOKEN_FILE", "").strip()
    repository = os.environ.get("STINT_GITHUB_REPOSITORY", "").strip()
    base = os.environ.get("STINT_GITHUB_BASE", "").strip()
    if not token_file:
        raise RuntimeError("STINT_GITHUB_TOKEN_FILE is required")
    if not os.path.isfile(token_file):
        raise RuntimeError(f"GitHub token file is not readable: {token_file}")
    if not SAFE_REPOSITORY.fullmatch(repository):
        raise RuntimeError("STINT_GITHUB_REPOSITORY must be owner/name")
    if not base or any(ch in base for ch in "\r\n\x00"):
        raise RuntimeError("STINT_GITHUB_BASE is required")
    mode = os.environ.get("STINT_GITHUB_MODE", "engineering").strip().lower()
    if mode not in {"none", "engineering", "maintenance"}:
        raise RuntimeError("STINT_GITHUB_MODE must be none, engineering, or maintenance")
    approval = os.environ.get("STINT_GITHUB_APPROVAL", "internal").strip().lower()
    if approval not in {"internal", "github", "bot"}:
        raise RuntimeError("STINT_GITHUB_APPROVAL must be internal, github, or bot")
    return {
        "token_file": token_file,
        "token": read_token(token_file),
        "repository": repository,
        "base": base,
        "api": os.environ.get("STINT_GITHUB_API_URL", "https://api.github.com").rstrip("/"),
        "git_url": os.environ.get("STINT_GITHUB_GIT_URL", f"https://github.com/{repository}.git"),
        "draft": os.environ.get("STINT_GITHUB_PR_DRAFT", "1") != "0",
        "mode": mode,
        "allowed_authors": [
            author.strip()
            for author in os.environ.get("STINT_GITHUB_ALLOWED_AUTHORS", "").split(",")
            if author.strip()
        ],
        "approval": approval,
        "ledger": os.environ.get("STINT_GITHUB_ACTIONS_LEDGER", "").strip(),
    }


def api_request(cfg: dict, method: str, path: str, payload=None):
    data = None
    headers = {
        "Accept": "application/vnd.github+json",
        "Authorization": f"Bearer {cfg['token']}",
        "User-Agent": "stint-onbox-deep-work",
        "X-GitHub-Api-Version": "2022-11-28",
    }
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(cfg["api"] + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=20) as response:
            body = response.read()
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")[-2000:]
        raise RuntimeError(f"GitHub API {method} {path} returned {exc.code}: {detail}") from exc
    if not body:
        return None
    return json.loads(body.decode("utf-8"))


def graphql_request(cfg: dict, query: str, variables=None):
    """Call GitHub GraphQL without ever putting the token in a URL or body."""
    data = json.dumps({"query": query, "variables": variables or {}}).encode("utf-8")
    headers = {
        "Accept": "application/vnd.github+json",
        "Authorization": f"Bearer {cfg['token']}",
        "User-Agent": "stint-onbox-deep-work",
        "Content-Type": "application/json",
    }
    req = urllib.request.Request(cfg["api"] + "/graphql", data=data, headers=headers, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=20) as response:
            body = response.read()
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")[-2000:]
        raise RuntimeError(f"GitHub GraphQL returned {exc.code}: {detail}") from exc
    payload = json.loads(body.decode("utf-8")) if body else {}
    if payload.get("errors"):
        raise RuntimeError("GitHub GraphQL error: " + json.dumps(payload["errors"])[-2000:])
    return payload.get("data") or {}


def ledger_path(state_dir: str | None, cfg: dict) -> Path | None:
    raw = cfg.get("ledger", "")
    if not raw:
        if not state_dir:
            return None
        raw = str(Path(state_dir) / "github-actions.jsonl")
    path = Path(raw)
    path.parent.mkdir(parents=True, exist_ok=True)
    return path


def append_ledger(cfg: dict, operation: str, *, session: str = "", task: str = "", pr: dict | None = None,
                  result: str, reason: str, head_sha: str = "", base: str = "", state_dir: str | None = None) -> None:
    path = ledger_path(state_dir, cfg)
    if path is None:
        return
    entry = {
        "timestamp": utc_now(),
        "session": session,
        "task": task,
        "operation": operation,
        "pr": (pr or {}).get("number"),
        "headSha": head_sha or (pr or {}).get("head", {}).get("sha", ""),
        "base": base or (pr or {}).get("base", {}).get("ref", ""),
        "result": result,
        "reason": reason,
    }
    with open(path, "a", encoding="utf-8") as stream:
        stream.write(json.dumps(entry, sort_keys=True) + "\n")
    os.chmod(path, 0o600)


def git(repo: str, *args: str, env=None) -> str:
    proc = subprocess.run(
        ["git", "-C", repo, *args],
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        env=env,
        timeout=60,
    )
    if proc.returncode != 0:
        detail = (proc.stderr or proc.stdout).strip()
        raise RuntimeError(f"git {args[0]} failed: {detail}")
    return proc.stdout.strip()


def normalize_github_repo(remote: str) -> str:
    remote = remote.strip()
    patterns = (
        r"^git@github\.com:([^/]+/[^/]+?)(?:\.git)?$",
        r"^ssh://git@github\.com/([^/]+/[^/]+?)(?:\.git)?$",
        r"^https?://github\.com/([^/]+/[^/]+?)(?:\.git)?/?$",
    )
    for pattern in patterns:
        match = re.match(pattern, remote)
        if match:
            return match.group(1)
    return ""


def preflight(repo_path: str) -> None:
    cfg = config()
    local_head = git(repo_path, "rev-parse", "HEAD")
    if not re.fullmatch(r"[0-9a-f]{40}", local_head):
        raise RuntimeError("repository HEAD is not a full commit SHA")
    origin = git(repo_path, "remote", "get-url", "origin")
    origin_repo = normalize_github_repo(origin)
    if origin_repo != cfg["repository"]:
        raise RuntimeError(
            f"origin mismatch: repository has {origin!r}, expected GitHub repository {cfg['repository']}"
        )
    repo_info = api_request(cfg, "GET", f"/repos/{cfg['repository']}")
    permissions = (repo_info or {}).get("permissions") or {}
    if permissions.get("push") is False:
        raise RuntimeError("GitHub credential does not have repository push permission")
    branch_path = urllib.parse.quote(cfg["base"], safe="")
    api_request(cfg, "GET", f"/repos/{cfg['repository']}/branches/{branch_path}")
    print(f"GITHUB_PREFLIGHT_OK repository={cfg['repository']} base={cfg['base']}")


def atomic_json(path: Path, payload: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    with open(tmp, "w", encoding="utf-8") as stream:
        json.dump(payload, stream, indent=2, sort_keys=True)
        stream.write("\n")
    os.chmod(tmp, 0o600)
    os.replace(tmp, path)


def slug(value: str) -> str:
    out = re.sub(r"[^A-Za-z0-9._-]+", "-", value).strip("-._").lower()
    return (out or "task")[:40]


def find_checkpoint_commit(worktree: str, session: str, task: dict) -> str:
    task_id = str(task.get("id", ""))
    recorded = str(task.get("checkpointCommit", "")).strip()
    if recorded:
        if not re.fullmatch(r"[0-9a-f]{40}", recorded):
            raise RuntimeError(f"invalid recorded checkpoint commit for {task_id}: {recorded!r}")
        git(worktree, "cat-file", "-e", recorded + "^{commit}")
        git(worktree, "merge-base", "--is-ancestor", recorded, "HEAD")
        return recorded

    # Compatibility for sessions started before checkpointCommit was added.
    # New sessions never infer the published revision from a subject line.
    subject = f"deep: {session} {task_id} verified"
    out = git(worktree, "log", "--format=%H%x00%s", "-2000")
    for line in out.splitlines():
        commit, sep, commit_subject = line.partition("\x00")
        if sep and commit_subject == subject:
            if not re.fullmatch(r"[0-9a-f]{40}", commit):
                raise RuntimeError(f"invalid checkpoint commit for {task_id}: {commit!r}")
            return commit
    raise RuntimeError(f"verified task {task_id} has no coordinator checkpoint commit")


def askpass_env(cfg: dict):
    fd, path = tempfile.mkstemp(prefix="stint-github-askpass-", text=True)
    try:
        os.write(fd, b'#!/bin/sh\ncase "$1" in\n  *Username*) printf "%s\\n" x-access-token ;;\n  *) cat "$STINT_GITHUB_TOKEN_FILE" ;;\nesac\n')
    finally:
        os.close(fd)
    os.chmod(path, 0o700)
    env = os.environ.copy()
    env.update({
        "GIT_ASKPASS": path,
        "GIT_TERMINAL_PROMPT": "0",
        "STINT_GITHUB_TOKEN_FILE": cfg["token_file"],
    })
    return env, path


def push_commit(cfg: dict, worktree: str, commit: str, branch: str) -> None:
    env, askpass = askpass_env(cfg)
    try:
        git(worktree, "push", "--porcelain", cfg["git_url"], f"{commit}:refs/heads/{branch}", env=env)
    finally:
        try:
            os.remove(askpass)
        except FileNotFoundError:
            pass


def existing_pr(cfg: dict, branch: str):
    owner = cfg["repository"].split("/", 1)[0]
    query = urllib.parse.urlencode({"state": "all", "head": f"{owner}:{branch}", "per_page": 10})
    prs = api_request(cfg, "GET", f"/repos/{cfg['repository']}/pulls?{query}") or []
    for pr in prs:
        if pr.get("head", {}).get("ref") == branch:
            return pr
    return None


def ensure_pr(cfg: dict, *, session: str, branch: str, base: str, title: str, body: str):
    pr = existing_pr(cfg, branch)
    if pr is None:
        pr = api_request(cfg, "POST", f"/repos/{cfg['repository']}/pulls", {
            "title": title,
            "head": branch,
            "base": base,
            "body": body,
            "draft": cfg["draft"],
        })
    else:
        # Keep publication idempotent while allowing a resumed worker to
        # refresh its objective/evidence. Never retarget an existing PR.
        if pr.get("base", {}).get("ref") != base:
            raise RuntimeError(
                f"existing PR #{pr.get('number')} base {pr.get('base', {}).get('ref')!r} differs from {base!r}"
            )
        if pr.get("state") == "open":
            api_request(cfg, "PATCH", f"/repos/{cfg['repository']}/pulls/{pr['number']}", {
                "title": title,
                "body": body,
            })
    return {
        "number": pr.get("number"),
        "url": pr.get("html_url", ""),
    }


def fetch_pull_request(cfg: dict, number: int) -> dict:
    return api_request(cfg, "GET", f"/repos/{cfg['repository']}/pulls/{int(number)}") or {}


def pull_request_inventory(cfg: dict) -> list[dict]:
    """Return compact open-PR summaries; detailed context is fetched on demand."""
    prs = []
    for page in range(1, 11):
        query = urllib.parse.urlencode({"state": "open", "per_page": 100, "page": page, "sort": "updated", "direction": "desc"})
        batch = api_request(cfg, "GET", f"/repos/{cfg['repository']}/pulls?{query}") or []
        prs.extend(batch)
        if len(batch) < 100:
            break
    result = []
    for pr in prs:
        result.append({
            "number": pr.get("number"),
            "title": pr.get("title", ""),
            "url": pr.get("html_url", ""),
            "draft": bool(pr.get("draft")),
            "author": (pr.get("user") or {}).get("login", ""),
            "head": (pr.get("head") or {}).get("ref", ""),
            "headSha": (pr.get("head") or {}).get("sha", ""),
            "base": (pr.get("base") or {}).get("ref", ""),
            "mergeable": pr.get("mergeable"),
            "mergeableState": pr.get("mergeable_state", ""),
            "updatedAt": pr.get("updated_at", ""),
        })
    return result


def review_threads(cfg: dict, pr: dict) -> list[dict]:
    """Fetch unresolved review threads through GraphQL when available."""
    owner, name = cfg["repository"].split("/", 1)
    node_id = pr.get("node_id")
    variables = {"owner": owner, "name": name, "number": int(pr.get("number", 0))}
    query = """
      query($owner:String!, $name:String!, $number:Int!) {
        repository(owner:$owner, name:$name) {
          pullRequest(number:$number) {
            reviewThreads(first:100) {
              nodes { isResolved isOutdated comments(first:20) { nodes { body author { login } } } }
            }
          }
        }
      }
    """
    data = graphql_request(cfg, query, variables)
    pull = ((data.get("repository") or {}).get("pullRequest") or {})
    return pull.get("reviewThreads", {}).get("nodes", []) or []


def pull_request_context(cfg: dict, number: int) -> dict:
    pr = fetch_pull_request(cfg, number)
    if not pr:
        raise RuntimeError(f"pull request #{number} was not found")
    comments = api_request(cfg, "GET", f"/repos/{cfg['repository']}/issues/{int(number)}/comments?per_page=100") or []
    reviews = api_request(cfg, "GET", f"/repos/{cfg['repository']}/pulls/{int(number)}/reviews?per_page=100") or []
    review_comments = api_request(cfg, "GET", f"/repos/{cfg['repository']}/pulls/{int(number)}/comments?per_page=100") or []
    files = api_request(cfg, "GET", f"/repos/{cfg['repository']}/pulls/{int(number)}/files?per_page=100") or []
    commits = api_request(cfg, "GET", f"/repos/{cfg['repository']}/pulls/{int(number)}/commits?per_page=100") or []
    checks = api_request(cfg, "GET", f"/repos/{cfg['repository']}/commits/{urllib.parse.quote(pr.get('head', {}).get('sha', ''), safe='')}/check-runs?per_page=100") or {}
    threads_error = ""
    try:
        threads = review_threads(cfg, pr)
    except Exception as exc:
        threads = []
        threads_error = str(exc)
    return {
        "pull": pr,
        "comments": comments,
        "reviews": reviews,
        "reviewComments": review_comments,
        "files": files,
        "commits": commits,
        "checkRuns": checks.get("check_runs", []),
        "reviewThreads": threads,
        "reviewThreadsError": threads_error,
    }


def reply_to_comment(cfg: dict, number: int, body: str, comment_id: int | None = None) -> dict:
    if not body.strip():
        raise RuntimeError("comment reply is empty")
    if comment_id is not None:
        # Review-comment replies use the dedicated endpoint and preserve the
        # thread relationship. Issue comments remain available through the
        # normal comments endpoint when no review comment id is supplied.
        return api_request(cfg, "POST", f"/repos/{cfg['repository']}/pulls/{int(number)}/comments/{int(comment_id)}/replies", {"body": body}) or {}
    return api_request(cfg, "POST", f"/repos/{cfg['repository']}/issues/{int(number)}/comments", {"body": body}) or {}


def _parent_pr_satisfied(cfg: dict, base_ref: str) -> bool:
    if base_ref == cfg["base"]:
        return True
    parent = existing_pr(cfg, base_ref)
    return bool(parent and parent.get("state") == "closed" and parent.get("merged_at"))


def _review_state_blocked(reviews: list[dict]) -> bool:
    latest: dict[str, str] = {}
    for review in reviews:
        user = (review.get("user") or {}).get("login", "")
        state = str(review.get("state", "")).upper()
        if user and state:
            latest[user] = state
    return any(state == "CHANGES_REQUESTED" for state in latest.values())


def _checks_blocked(check_runs: list[dict], statuses: dict) -> list[str]:
    reasons = []
    accepted = {"success", "neutral", "skipped"}
    for check in check_runs:
        status = str(check.get("status", "")).lower()
        conclusion = str(check.get("conclusion", "")).lower()
        if status != "completed":
            reasons.append(f"check {check.get('name', '')} is {status or 'unknown'}")
        elif conclusion not in accepted:
            reasons.append(f"check {check.get('name', '')} conclusion is {conclusion or 'unknown'}")
    state = str((statuses or {}).get("state", "")).lower()
    if state in {"pending", "error", "failure"}:
        reasons.append(f"commit status is {state}")
    return reasons


def merge_gate(cfg: dict, number: int, approval: dict | None = None, expected_head: str = "") -> tuple[bool, list[str], dict]:
    """Evaluate all deterministic merge gates and return (allowed, reasons, context)."""
    approval = approval or {}
    context = pull_request_context(cfg, int(number))
    pr = context["pull"]
    reasons: list[str] = []
    if pr.get("state") != "open":
        reasons.append("pull request is not open")
    if pr.get("draft"):
        reasons.append("pull request is a draft")
    author = (pr.get("user") or {}).get("login", "")
    allowed_authors = cfg.get("allowed_authors") or []
    if not allowed_authors or author not in allowed_authors:
        reasons.append(f"author {author or '<unknown>'} is not in allowed-authors")
    head = pr.get("head") or {}
    head_ref = str(head.get("ref", ""))
    head_sha = str(head.get("sha", ""))
    if head_ref.startswith("stint/deep-"):
        reasons.append("Deep Work branch is excluded from maintenance merges")
    for commit in context.get("commits", []):
        message = str((commit.get("commit") or {}).get("message", "")).lower()
        author_email = str(((commit.get("commit") or {}).get("author") or {}).get("email", "")).lower()
        if message.startswith("deep:") or author_email.endswith("@stint.local"):
            reasons.append("PR ancestry contains a Deep Work-generated commit")
            break
    if not re.fullmatch(r"[0-9a-f]{40}", head_sha):
        reasons.append("head SHA is missing or invalid")
    base_ref = str((pr.get("base") or {}).get("ref", ""))
    if not base_ref or not _parent_pr_satisfied(cfg, base_ref):
        reasons.append(f"base/dependency {base_ref or '<unknown>'} is not satisfied")
    if pr.get("mergeable") is not True or str(pr.get("mergeable_state", "")).lower() != "clean":
        reasons.append("GitHub does not report MERGEABLE and CLEAN")
    expected = expected_head or str(approval.get("headSha", ""))
    if not expected:
        reasons.append("approval has no reviewed head SHA")
    elif expected != head_sha:
        reasons.append("head SHA changed since review")
    if _review_state_blocked(context["reviews"]):
        reasons.append("blocking CHANGES_REQUESTED review remains")
    threads = context.get("reviewThreads", [])
    if context.get("reviewThreadsError"):
        reasons.append("review thread state unavailable: " + context["reviewThreadsError"])
    if any(not thread.get("isResolved") and not thread.get("isOutdated") for thread in threads):
        reasons.append("unresolved review thread remains")
    statuses = {}
    if re.fullmatch(r"[0-9a-f]{40}", head_sha):
        try:
            statuses = api_request(
                cfg, "GET", f"/repos/{cfg['repository']}/commits/{urllib.parse.quote(head_sha, safe='')}/status"
            ) or {}
        except Exception as exc:
            reasons.append(f"commit status unavailable: {exc}")
    reasons.extend(_checks_blocked(context.get("checkRuns", []), statuses))
    if str(approval.get("decision", "")).lower() != "approved":
        reasons.append("xhigh internal approval decision is missing")
    if cfg.get("approval") == "github":
        if not any(str(review.get("state", "")).upper() == "APPROVED" for review in context["reviews"]):
            reasons.append("GitHub approval policy has no approving review")
    elif cfg.get("approval") == "bot":
        reasons.append("bot approval policy is documented but not enabled")
    if not str(approval.get("evidence", "")).strip():
        reasons.append("approval evidence is missing")
    files = [str(item.get("filename", "")) for item in context.get("files", [])]
    reviewed_files = approval.get("filesReviewed") or []
    if reviewed_files and sorted(files) != sorted(str(item) for item in reviewed_files):
        reasons.append("approval filesReviewed does not match the actual diff")
    elif not reviewed_files and files:
        reasons.append("approval does not explain the changed files")
    generated = [name for name in files if name.endswith((".pyc", ".o", ".tmp")) or name.startswith(("bin/", "dist/"))]
    if generated and not set(generated).issubset(set(approval.get("generatedFiles") or [])):
        reasons.append("generated artifacts are present without an explicit explanation")
    return not reasons, reasons, context


def merge_pull_request(state_dir: str, number: int, approval_path: str) -> dict:
    cfg = config()
    if cfg.get("mode") != "maintenance":
        raise RuntimeError("merge operation requires STINT_GITHUB_MODE=maintenance")
    approval = json.loads(Path(approval_path).read_text(encoding="utf-8"))
    state_path = Path(state_dir) / "deep.json"
    state = json.loads(state_path.read_text(encoding="utf-8")) if state_path.is_file() else {}
    if state.get("githubLedger"):
        cfg["ledger"] = str(state["githubLedger"])
    session = state.get("sessionId", "")
    existing = fetch_pull_request(cfg, number)
    if existing.get("state") == "closed" and existing.get("merged_at"):
        append_ledger(cfg, "merge", session=session, task=str(approval.get("task", "")), pr=existing,
                      result="already-merged", reason="idempotent retry", state_dir=state_dir)
        return {"number": number, "result": "already-merged", "headSha": (existing.get("head") or {}).get("sha", "")}
    allowed, reasons, context = merge_gate(cfg, number, approval)
    pr = context.get("pull", {})
    if not allowed:
        reason = "; ".join(reasons)
        append_ledger(cfg, "merge", session=session, task=str(approval.get("task", "")), pr=pr,
                      result="rejected", reason=reason, state_dir=state_dir)
        return {"number": number, "result": "rejected", "reasons": reasons, "headSha": (pr.get("head") or {}).get("sha", "")}
    head_sha = (pr.get("head") or {}).get("sha", "")
    # The SHA is supplied to GitHub's merge endpoint as an optimistic lock.
    result = api_request(cfg, "PUT", f"/repos/{cfg['repository']}/pulls/{int(number)}/merge", {
        "sha": head_sha,
        "merge_method": "merge",
    }) or {}
    merged = bool(result.get("merged"))
    append_ledger(cfg, "merge", session=session, task=str(approval.get("task", "")), pr=pr,
                  result="merged" if merged else "not-merged", reason=result.get("message", "merge gate passed"),
                  head_sha=head_sha, base=(pr.get("base") or {}).get("ref", ""), state_dir=state_dir)
    if not merged:
        raise RuntimeError(f"GitHub did not merge PR #{number}: {result.get('message', 'unknown result')}")
    return {"number": number, "result": "merged", "headSha": head_sha, "message": result.get("message", "")}


def record_action(state_dir: str, operation: str, result: str, reason: str, task: str = "", number: int | None = None,
                  head_sha: str = "", base: str = "") -> dict:
    cfg = config()
    state_path = Path(state_dir) / "deep.json"
    state = json.loads(state_path.read_text(encoding="utf-8")) if state_path.is_file() else {}
    if state.get("githubLedger"):
        cfg["ledger"] = str(state["githubLedger"])
    pr = {"number": number} if number is not None else None
    append_ledger(cfg, operation, session=state.get("sessionId", ""), task=task, pr=pr,
                  result=result, reason=reason, head_sha=head_sha, base=base, state_dir=state_dir)
    return {"operation": operation, "result": result, "task": task, "pr": number}


def request_merge(state_dir: str, number: int, approval_path: str) -> dict:
    """Persist a merge request for the supervisor's deterministic gatekeeper."""
    approval = json.loads(Path(approval_path).read_text(encoding="utf-8"))
    if not isinstance(approval, dict):
        raise RuntimeError("merge approval must be a JSON object")
    approval.setdefault("pr", number)
    approval.setdefault("requestedAt", utc_now())
    requests = Path(state_dir) / "merge-requests"
    requests.mkdir(parents=True, exist_ok=True)
    path = requests / f"{int(number)}.json"
    atomic_json(path, approval)
    cfg = config()
    state_path = Path(state_dir) / "deep.json"
    state = json.loads(state_path.read_text(encoding="utf-8")) if state_path.is_file() else {}
    if state.get("githubLedger"):
        cfg["ledger"] = str(state["githubLedger"])
    append_ledger(cfg, "merge-request", session=state.get("sessionId", ""), task=str(approval.get("task", "")),
                  pr={"number": number}, head_sha=str(approval.get("headSha", "")),
                  result="queued", reason="worker submitted merge request", state_dir=state_dir)
    return {"number": number, "request": str(path), "result": "queued"}


def initial_publication(cfg: dict, state: dict) -> dict:
    return {
        "version": 1,
        "session": state.get("sessionId", ""),
        "repository": cfg["repository"],
        "base": cfg["base"],
        "origin": os.environ.get("STINT_ONBOX_ORIGIN", "unknown"),
        "instanceId": os.environ.get("STINT_ONBOX_INSTANCE_ID", ""),
        "hostname": socket.gethostname(),
        "checkpoints": [],
        "handoff": None,
        "lastError": "",
        "updatedAt": utc_now(),
    }


def load_publication(path: Path, cfg: dict, state: dict) -> dict:
    if path.is_file():
        payload = json.loads(path.read_text(encoding="utf-8"))
        if payload.get("session") != state.get("sessionId"):
            raise RuntimeError("publication.json session does not match deep.json")
        if payload.get("repository") != cfg["repository"] or payload.get("base") != cfg["base"]:
            raise RuntimeError("publication.json GitHub repository/base differs from launch configuration")
        return payload
    return initial_publication(cfg, state)


def record_error(path: Path, cfg: dict, state: dict, exc: Exception) -> None:
    try:
        payload = load_publication(path, cfg, state)
    except Exception:
        payload = initial_publication(cfg, state)
    payload["lastError"] = str(exc)[-2000:]
    payload["updatedAt"] = utc_now()
    atomic_json(path, payload)


def sync(state_dir: str) -> None:
    cfg = config()
    root = Path(state_dir)
    state_path = root / "deep.json"
    if not state_path.is_file():
        raise RuntimeError(f"deep state is missing: {state_path}")
    state = json.loads(state_path.read_text(encoding="utf-8"))
    if state.get("githubLedger"):
        cfg["ledger"] = str(state["githubLedger"])
    session = state.get("sessionId", "")
    if not SAFE_SESSION.fullmatch(session):
        raise RuntimeError("invalid Deep Work session id")
    worktree = state.get("worktreePath", "")
    if not worktree or not os.path.isdir(worktree):
        raise RuntimeError("Deep Work worktree is unavailable")
    publication_path = root / "publication.json"
    publication = load_publication(publication_path, cfg, state)
    existing = {entry.get("taskId"): entry for entry in publication.get("checkpoints", [])}
    previous_branch = cfg["base"]
    checkpoints = []

    for index, task in enumerate(state.get("tasks", []), start=1):
        task_id = str(task.get("id", ""))
        if task.get("status") != "verified":
            continue
        commit = find_checkpoint_commit(worktree, session, task)
        branch = f"stint/deep-{session}-{index:02d}-{slug(task_id)}"
        entry = existing.get(task_id)
        if entry is not None:
            if entry.get("commit") != commit or entry.get("branch") != branch or entry.get("base") != previous_branch:
                raise RuntimeError(f"published checkpoint identity changed for {task_id}")
        else:
            push_commit(cfg, worktree, commit, branch)
            append_ledger(cfg, "push", session=session, task=task_id,
                          head_sha=commit, base=previous_branch, result="pushed",
                          reason="verified checkpoint", state_dir=str(root))
            objective = str(task.get("objective", "")).strip()
            pr = ensure_pr(
                cfg,
                session=session,
                branch=branch,
                base=previous_branch,
                title=f"deep: {session} {task_id}",
                body=(
                    f"GPU-owned Deep Work checkpoint for `{task_id}`.\n\n"
                    f"Session: `{session}`\n\n"
                    f"Verified commit: `{commit}`\n\n"
                    f"Objective: {objective}\n\n"
                    "This PR was pushed and opened by the detached on-box supervisor after coordinator verification."
                ),
            )
            append_ledger(cfg, "pull-request", session=session, task=task_id,
                          pr={"number": pr["number"]}, head_sha=commit, base=previous_branch,
                          result="created-or-updated", reason="verified checkpoint publication", state_dir=str(root))
            entry = {
                "taskId": task_id,
                "commit": commit,
                "branch": branch,
                "base": previous_branch,
                "prNumber": pr["number"],
                "prUrl": pr["url"],
                "publishedAt": utc_now(),
            }
            publication.setdefault("checkpoints", []).append(entry)
            publication["lastError"] = ""
            publication["updatedAt"] = utc_now()
            atomic_json(publication_path, publication)
        checkpoints.append(entry)
        previous_branch = branch

    publication["checkpoints"] = checkpoints

    if state.get("phase") == "landed":
        head = git(worktree, "rev-parse", "HEAD")
        handoff_branch = f"stint/deep-{session}-handoff"
        handoff = publication.get("handoff")
        if handoff is None or handoff.get("commit") != head:
            push_commit(cfg, worktree, head, handoff_branch)
            append_ledger(cfg, "push", session=session, task="CLOSE",
                          head_sha=head, base=previous_branch, result="pushed",
                          reason="final handoff", state_dir=str(root))
            pr = ensure_pr(
                cfg,
                session=session,
                branch=handoff_branch,
                base=previous_branch,
                title=f"deep: {session} handoff",
                body=(
                    f"Final GPU-owned Deep Work handoff for session `{session}`.\n\n"
                    f"Handoff commit: `{head}`\n\n"
                    "This final stack layer contains the generated Deep Work handoff and any landing-only evidence."
                ),
            )
            append_ledger(cfg, "pull-request", session=session, task="CLOSE",
                          pr={"number": pr["number"]}, head_sha=head, base=previous_branch,
                          result="created-or-updated", reason="final handoff publication", state_dir=str(root))
            handoff = {
                "commit": head,
                "branch": handoff_branch,
                "base": previous_branch,
                "prNumber": pr["number"],
                "prUrl": pr["url"],
                "publishedAt": utc_now(),
            }
            publication["handoff"] = handoff

    publication["lastError"] = ""
    publication["updatedAt"] = utc_now()
    atomic_json(publication_path, publication)
    urls = [entry.get("prUrl", "") for entry in publication.get("checkpoints", []) if entry.get("prUrl")]
    handoff = publication.get("handoff") or {}
    if handoff.get("prUrl"):
        urls.append(handoff["prUrl"])
    print(f"GITHUB_PUBLISH_OK session={session} prs={len(urls)}")


def main() -> int:
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="command", required=True)
    pre = sub.add_parser("preflight")
    pre.add_argument("repo_path")
    syn = sub.add_parser("sync")
    syn.add_argument("state_dir")
    inv = sub.add_parser("inventory")
    inv.add_argument("--output", default="", help="write compact inventory JSON to this path")
    ctx = sub.add_parser("context")
    ctx.add_argument("pr", type=int)
    ctx.add_argument("--output", default="")
    reply = sub.add_parser("reply")
    reply.add_argument("pr", type=int)
    reply.add_argument("body")
    reply.add_argument("--comment-id", type=int, default=None)
    reply.add_argument("--state-dir", default="")
    gate = sub.add_parser("gate")
    gate.add_argument("pr", type=int)
    gate.add_argument("--approval", required=True)
    gate.add_argument("--output", default="")
    gate.add_argument("--state-dir", default="")
    merge = sub.add_parser("merge")
    merge.add_argument("state_dir")
    merge.add_argument("pr", type=int)
    merge.add_argument("--approval", required=True)
    rec = sub.add_parser("record")
    rec.add_argument("state_dir")
    rec.add_argument("operation")
    rec.add_argument("result")
    rec.add_argument("reason")
    rec.add_argument("--task", default="")
    rec.add_argument("--pr", type=int, default=None)
    rec.add_argument("--head-sha", default="")
    rec.add_argument("--base", default="")
    req = sub.add_parser("request-merge")
    req.add_argument("state_dir")
    req.add_argument("pr", type=int)
    req.add_argument("--approval", required=True)
    args = parser.parse_args()
    try:
        if args.command == "preflight":
            preflight(args.repo_path)
        elif args.command == "sync":
            try:
                sync(args.state_dir)
            except Exception as exc:
                try:
                    cfg = config()
                    state_path = Path(args.state_dir) / "deep.json"
                    state = json.loads(state_path.read_text(encoding="utf-8")) if state_path.is_file() else {}
                    if state:
                        record_error(Path(args.state_dir) / "publication.json", cfg, state, exc)
                except Exception:
                    pass
                raise
        elif args.command == "inventory":
            cfg = config()
            payload = {"repository": cfg["repository"], "base": cfg["base"], "mode": cfg["mode"], "allowedAuthors": cfg["allowed_authors"], "updatedAt": utc_now(), "pulls": pull_request_inventory(cfg)}
            encoded = json.dumps(payload, indent=2, sort_keys=True) + "\n"
            if args.output:
                atomic_json(Path(args.output), payload)
            else:
                print(encoded, end="")
        elif args.command == "context":
            cfg = config()
            payload = pull_request_context(cfg, args.pr)
            if args.output:
                atomic_json(Path(args.output), payload)
            else:
                print(json.dumps(payload, indent=2, sort_keys=True))
        elif args.command == "reply":
            cfg = config()
            try:
                result = reply_to_comment(cfg, args.pr, args.body, args.comment_id)
            except Exception as exc:
                append_ledger(cfg, "comment-reply", pr={"number": args.pr}, result="failed", reason=str(exc), state_dir=args.state_dir or None)
                raise
            append_ledger(cfg, "comment-reply", pr={"number": args.pr}, result="posted", reason="worker review response", state_dir=args.state_dir or None)
            print(json.dumps({"number": args.pr, "comment": result.get("id")}, sort_keys=True))
        elif args.command == "gate":
            cfg = config()
            approval = json.loads(Path(args.approval).read_text(encoding="utf-8"))
            allowed, reasons, context = merge_gate(cfg, args.pr, approval)
            append_ledger(cfg, "merge-gate", task=str(approval.get("task", "")),
                          pr=context.get("pull", {}), result="passed" if allowed else "rejected",
                          reason="; ".join(reasons) if reasons else "all gates passed",
                          state_dir=args.state_dir or None)
            payload = {"number": args.pr, "allowed": allowed, "reasons": reasons, "headSha": (context.get("pull", {}).get("head") or {}).get("sha", "")}
            if args.output:
                atomic_json(Path(args.output), payload)
            else:
                print(json.dumps(payload, indent=2, sort_keys=True))
            if not allowed:
                return 2
        elif args.command == "merge":
            result = merge_pull_request(args.state_dir, args.pr, args.approval)
            print(json.dumps(result, indent=2, sort_keys=True))
        elif args.command == "record":
            print(json.dumps(record_action(args.state_dir, args.operation, args.result, args.reason,
                                           args.task, args.pr, args.head_sha, args.base), sort_keys=True))
        elif args.command == "request-merge":
            print(json.dumps(request_merge(args.state_dir, args.pr, args.approval), sort_keys=True))
    except Exception as exc:
        print(f"GITHUB_PUBLISH_FAIL {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
