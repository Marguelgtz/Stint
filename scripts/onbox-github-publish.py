#!/usr/bin/env python3
"""Publish verified on-box Deep Work checkpoints to GitHub.

This helper runs only on the compute instance. It reads durable Deep Work state,
pushes immutable checkpoint branches, creates a stacked draft PR chain, and writes
publication.json beside deep.json. Authentication is read from a root-only token
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
    return {
        "token_file": token_file,
        "token": read_token(token_file),
        "repository": repository,
        "base": base,
        "api": os.environ.get("STINT_GITHUB_API_URL", "https://api.github.com").rstrip("/"),
        "git_url": os.environ.get("STINT_GITHUB_GIT_URL", f"https://github.com/{repository}.git"),
        "draft": os.environ.get("STINT_GITHUB_PR_DRAFT", "1") != "0",
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


def find_checkpoint_commit(worktree: str, session: str, task_id: str) -> str:
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
    return {
        "number": pr.get("number"),
        "url": pr.get("html_url", ""),
    }


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
        commit = find_checkpoint_commit(worktree, session, task_id)
        branch = f"stint/deep-{session}-{index:02d}-{slug(task_id)}"
        entry = existing.get(task_id)
        if entry is not None:
            if entry.get("commit") != commit or entry.get("branch") != branch or entry.get("base") != previous_branch:
                raise RuntimeError(f"published checkpoint identity changed for {task_id}")
        else:
            push_commit(cfg, worktree, commit, branch)
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
    args = parser.parse_args()
    try:
        if args.command == "preflight":
            preflight(args.repo_path)
        else:
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
    except Exception as exc:
        print(f"GITHUB_PUBLISH_FAIL {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
