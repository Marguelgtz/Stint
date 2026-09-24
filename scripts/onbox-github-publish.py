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
SAFE_AUTHOR = re.compile(r"^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$")


class PermanentPublicationError(RuntimeError):
    """A policy or immutable identity conflict that retries cannot fix."""


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
    api = os.environ.get("STINT_GITHUB_API_URL", "https://api.github.com").rstrip("/")
    parsed_api = urllib.parse.urlsplit(api)
    if parsed_api.scheme != "https" or parsed_api.netloc != "api.github.com" or parsed_api.path or parsed_api.query or parsed_api.fragment:
        raise RuntimeError("STINT_GITHUB_API_URL must be https://api.github.com; refusing to send the token elsewhere")
    git_url = os.environ.get("STINT_GITHUB_GIT_URL", f"https://github.com/{repository}.git")
    if normalize_github_repo(git_url) != repository:
        raise RuntimeError("STINT_GITHUB_GIT_URL must point to STINT_GITHUB_REPOSITORY")
    mode = os.environ.get("STINT_GITHUB_MODE", "engineering").strip().lower()
    if mode not in {"none", "engineering", "maintenance"}:
        raise RuntimeError("STINT_GITHUB_MODE must be none, engineering, or maintenance")
    approval = os.environ.get("STINT_GITHUB_APPROVAL", "internal").strip().lower()
    if approval not in {"internal", "github", "bot"}:
        raise RuntimeError("STINT_GITHUB_APPROVAL must be internal, github, or bot")
    allowed_authors = [author.strip() for author in os.environ.get("STINT_GITHUB_ALLOWED_AUTHORS", "").split(",") if author.strip()]
    if any(not SAFE_AUTHOR.fullmatch(author) for author in allowed_authors):
        raise RuntimeError("STINT_GITHUB_ALLOWED_AUTHORS contains an invalid GitHub login")
    if len({author.lower() for author in allowed_authors}) != len(allowed_authors):
        raise RuntimeError("STINT_GITHUB_ALLOWED_AUTHORS contains duplicates")
    return {
        "token_file": token_file,
        "token": read_token(token_file),
        "repository": repository,
        "base": base,
        "api": api,
        "git_url": git_url,
        "draft": os.environ.get("STINT_GITHUB_PR_DRAFT", "1") != "0",
        "mode": mode,
        "allowed_authors": allowed_authors,
        "approval": approval,
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


def api_paginated(cfg: dict, path: str, *, per_page: int = 100, max_pages: int = 100) -> list:
    """Fetch a full REST collection or fail closed when its size is unbounded."""
    split = urllib.parse.urlsplit(path)
    original = dict(urllib.parse.parse_qsl(split.query, keep_blank_values=True))
    result = []
    for page in range(1, max_pages + 1):
        params = {**original, "per_page": per_page, "page": page}
        page_path = urllib.parse.urlunsplit(("", "", split.path, urllib.parse.urlencode(params), ""))
        batch = api_request(cfg, "GET", page_path)
        if not isinstance(batch, list):
            raise RuntimeError(f"GitHub collection {split.path} returned a non-list response")
        result.extend(batch)
        if len(batch) < per_page:
            return result
    # One look-ahead distinguishes an exactly-full final page from truncation.
    params = {**original, "per_page": per_page, "page": max_pages + 1}
    page_path = urllib.parse.urlunsplit(("", "", split.path, urllib.parse.urlencode(params), ""))
    batch = api_request(cfg, "GET", page_path)
    if not isinstance(batch, list):
        raise RuntimeError(f"GitHub collection {split.path} returned a non-list response")
    if batch:
        raise RuntimeError(f"GitHub collection {split.path} exceeds the {max_pages * per_page}-item completeness limit")
    return result


def assert_session_github_policy(cfg: dict, state: dict) -> None:
    saved = state.get("github")
    if not isinstance(saved, dict):
        raise PermanentPublicationError("deep.json has no persisted GitHub policy")
    saved_authors = sorted(str(author).strip().lower() for author in saved.get("allowedAuthors", []))
    configured_authors = sorted(str(author).strip().lower() for author in cfg.get("allowed_authors", []))
    expected = (
        str(saved.get("mode", "")).strip().lower(),
        str(saved.get("repository", "")).strip(),
        str(saved.get("base", "")).strip(),
        saved_authors,
        str(saved.get("approval", "")).strip().lower() or "internal",
    )
    actual = (cfg["mode"], cfg["repository"], cfg["base"], configured_authors, cfg["approval"])
    if expected != actual:
        raise PermanentPublicationError("publisher GitHub configuration differs from persisted mission policy (mode, repository, base, allowed authors, approval)")
    if cfg["mode"] != "engineering":
        raise PermanentPublicationError(f"on-box checkpoint publisher does not implement GitHub mode {cfg['mode']!r}")


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
    if cfg["mode"] != "engineering":
        raise RuntimeError(f"on-box checkpoint publisher does not implement GitHub mode {cfg['mode']!r}")
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


def push_commit(cfg: dict, worktree: str, commit: str, branch: str, session: str) -> None:
    if branch == cfg["base"] or branch in {"main", "master", "develop", "default"}:
        raise RuntimeError("publisher cannot push the configured base or a default branch")
    match = re.fullmatch(r"stint/deep-([A-Za-z0-9_-]+)-(?:[0-9]{2,}-[a-z0-9._-]{1,40}|handoff(?:-[0-9a-f]{12})?)", branch)
    if not match:
        raise RuntimeError("publisher push is restricted to generated session checkpoint/handoff branches")
    if match.group(1) != session:
        raise RuntimeError("publisher push branch does not belong to the durable session")
    if normalize_github_repo(cfg["git_url"]) != cfg["repository"]:
        raise RuntimeError("publisher push URL differs from the persisted repository policy")
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise RuntimeError("publisher only pushes a full immutable commit SHA")
    git(worktree, "cat-file", "-e", commit + "^{commit}")
    git(worktree, "merge-base", "--is-ancestor", commit, "HEAD")
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
    query = urllib.parse.urlencode({"state": "all", "head": f"{owner}:{branch}"})
    prs = api_paginated(cfg, f"/repos/{cfg['repository']}/pulls?{query}")
    for pr in prs:
        if pr.get("head", {}).get("ref") == branch:
            return pr
    return None


def ensure_pr(cfg: dict, *, session: str, branch: str, base: str, title: str, body: str, expected_head: str):
    pr = existing_pr(cfg, branch)
    if pr is None:
        pr = api_request(cfg, "POST", f"/repos/{cfg['repository']}/pulls", {
            "title": title,
            "head": branch,
            "base": base,
            "body": body,
            "draft": cfg["draft"],
        })
    if not isinstance(pr, dict):
        raise RuntimeError(f"GitHub did not return a PR record for {branch}")
    actual_base = str((pr.get("base") or {}).get("ref", ""))
    actual_branch = str((pr.get("head") or {}).get("ref", ""))
    actual_head = str((pr.get("head") or {}).get("sha", ""))
    actual_repository = str(((pr.get("head") or {}).get("repo") or {}).get("full_name", ""))
    if actual_base != base:
        raise PermanentPublicationError(f"existing PR #{pr.get('number')} base {actual_base!r} differs from {base!r}")
    if actual_repository != cfg["repository"] or actual_branch != branch or actual_head != expected_head:
        raise PermanentPublicationError(f"existing PR #{pr.get('number')} head identity differs from checkpoint {branch}@{expected_head}")
    return {
        "number": pr.get("number"),
        "url": pr.get("html_url", ""),
    }


def validate_published_pr(cfg: dict, entry: dict, *, branch: str, base: str, commit: str) -> None:
    try:
        number = int(entry.get("prNumber", 0))
    except (TypeError, ValueError):
        number = 0
    if number <= 0:
        raise RuntimeError(f"publication record for {branch} has no valid PR number")
    pr = api_request(cfg, "GET", f"/repos/{cfg['repository']}/pulls/{number}") or {}
    head = pr.get("head") or {}
    actual_base = str((pr.get("base") or {}).get("ref", ""))
    actual_repository = str((head.get("repo") or {}).get("full_name", ""))
    if actual_repository != cfg["repository"] or str(head.get("ref", "")) != branch or str(head.get("sha", "")) != commit or actual_base != base:
        raise PermanentPublicationError(f"published PR #{number} no longer matches durable identity {branch}@{commit} -> {base}")
    if entry.get("prUrl") and entry["prUrl"] != pr.get("html_url", ""):
        raise PermanentPublicationError(f"published PR URL for {branch} differs from GitHub's record")


def exact_landing_commit(state: dict, worktree: str) -> str:
    if state.get("phase") != "landed":
        raise PermanentPublicationError("final handoff publication requires landed durable state")
    if state.get("landingVerifyDone") is not True or not str(state.get("landingHandoff", "")).strip():
        raise PermanentPublicationError("landed state has no completed final verification/handoff record")
    commit = str(state.get("landingCommit", ""))
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise PermanentPublicationError("landed state has no valid durable landingCommit SHA")
    head = git(worktree, "rev-parse", "HEAD")
    if head != commit:
        raise PermanentPublicationError(f"worktree HEAD {head} differs from durable landingCommit {commit}")
    git(worktree, "cat-file", "-e", commit + "^{commit}")
    if git(worktree, "status", "--porcelain"):
        raise PermanentPublicationError("landed worktree has uncommitted changes")
    handoff_path = str(state.get("handoffPath", ""))
    if not handoff_path or not os.path.isfile(handoff_path):
        raise PermanentPublicationError("durable handoff file is missing")
    if Path(handoff_path).read_text(encoding="utf-8") != state["landingHandoff"]:
        raise PermanentPublicationError("durable handoff file differs from persisted landingHandoff")
    return commit


def initial_publication(cfg: dict, state: dict) -> dict:
    return {
        "version": 2,
        "session": state.get("sessionId", ""),
        "repository": cfg["repository"],
        "base": cfg["base"],
        "mode": cfg["mode"],
        "allowedAuthors": sorted(cfg["allowed_authors"], key=str.lower),
        "approval": cfg["approval"],
        "origin": os.environ.get("STINT_ONBOX_ORIGIN", "unknown"),
        "instanceId": os.environ.get("STINT_ONBOX_INSTANCE_ID", ""),
        "hostname": socket.gethostname(),
        "checkpoints": [],
        "handoff": None,
        "handoffHistory": [],
        "handoffDrifts": [],
        "lastError": "",
        "updatedAt": utc_now(),
    }


def load_publication(path: Path, cfg: dict, state: dict) -> dict:
    if path.is_file():
        payload = json.loads(path.read_text(encoding="utf-8"))
        if payload.get("version") != 2:
            raise PermanentPublicationError("publication.json lacks the persisted GitHub policy schema; refusing an implicit authority upgrade")
        if payload.get("session") != state.get("sessionId"):
            raise PermanentPublicationError("publication.json session does not match deep.json")
        if payload.get("repository") != cfg["repository"] or payload.get("base") != cfg["base"]:
            raise PermanentPublicationError("publication.json GitHub repository/base differs from launch configuration")
        if payload.get("mode") != cfg["mode"] or payload.get("approval", "internal") != cfg["approval"]:
            raise PermanentPublicationError("publication.json GitHub mode/approval differs from launch configuration")
        saved_authors = sorted(payload.get("allowedAuthors", []), key=str.lower)
        configured_authors = sorted(cfg["allowed_authors"], key=str.lower)
        if saved_authors != configured_authors:
            raise PermanentPublicationError("publication.json allowed authors differ from launch configuration")
        return payload
    return initial_publication(cfg, state)


def record_error(path: Path, cfg: dict, state: dict, exc: Exception) -> None:
    if path.is_file():
        try:
            payload = load_publication(path, cfg, state)
        except Exception:
            # A policy or identity mismatch must not be rewritten with the
            # current environment: that would destroy the evidence of drift.
            atomic_json(path.with_name("publication-error.json"), {
                "session": state.get("sessionId", ""),
                "error": str(exc)[-2000:],
                "updatedAt": utc_now(),
            })
            return
    else:
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
    assert_session_github_policy(cfg, state)
    session = state.get("sessionId", "")
    if not SAFE_SESSION.fullmatch(session):
        raise RuntimeError("invalid Deep Work session id")
    worktree = state.get("worktreePath", "")
    if not worktree or not os.path.isdir(worktree):
        raise RuntimeError("Deep Work worktree is unavailable")
    publication_path = root / "publication.json"
    publication = load_publication(publication_path, cfg, state)
    existing = {entry.get("taskId"): entry for entry in publication.get("checkpoints", [])}
    task_ids = [str(task.get("id", "")) for task in state.get("tasks", [])]
    if any(not task_id for task_id in task_ids) or len(set(task_ids)) != len(task_ids):
        raise RuntimeError("durable task identity is empty or duplicated")
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
                raise PermanentPublicationError(f"published checkpoint identity changed for {task_id}")
            validate_published_pr(cfg, entry, branch=branch, base=previous_branch, commit=commit)
        else:
            push_commit(cfg, worktree, commit, branch, session)
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
                expected_head=commit,
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
        head = exact_landing_commit(state, worktree)
        handoff = publication.get("handoff")
        handoff_branch = f"stint/deep-{session}-handoff"
        versioned_handoff_branch = f"stint/deep-{session}-handoff-{head[:12]}"
        if handoff is not None and handoff.get("commit") == head and handoff.get("branch") == versioned_handoff_branch:
            # A resumed landing keeps its versioned final identity on every
            # subsequent heartbeat/publication sync.
            handoff_branch = versioned_handoff_branch
        if handoff is not None and (
            handoff.get("commit") != head
            or handoff.get("branch") != handoff_branch
            or handoff.get("base") != previous_branch
        ):
            handoff_branch = versioned_handoff_branch
            history = publication.setdefault("handoffHistory", [])
            old_key = (handoff.get("branch"), handoff.get("commit"), handoff.get("prNumber"))
            old_record = next((entry for entry in history if (entry.get("branch"), entry.get("commit"), entry.get("prNumber")) == old_key), None)
            if old_record is None:
                old_record = dict(handoff)
                old_record.update({"status": "superseding", "supersededAt": utc_now()})
                history.append(old_record)
            new_identity = {"commit": head, "branch": handoff_branch, "base": previous_branch}
            if not any(
                drift.get("previous", {}).get("branch") == handoff.get("branch")
                and drift.get("previous", {}).get("commit") == handoff.get("commit")
                and drift.get("next") == new_identity
                for drift in publication.setdefault("handoffDrifts", [])
            ):
                publication["handoffDrifts"].append({
                    "previous": {"commit": handoff.get("commit", ""), "branch": handoff.get("branch", ""), "base": handoff.get("base", "")},
                    "next": new_identity,
                    "detectedAt": utc_now(),
                })
            old_record["supersededBy"] = new_identity
            publication["updatedAt"] = utc_now()
            atomic_json(publication_path, publication)
            handoff = None
        if handoff is not None:
            if handoff.get("commit") != head or handoff.get("branch") != handoff_branch or handoff.get("base") != previous_branch:
                raise PermanentPublicationError("published handoff identity differs from durable landing state")
            validate_published_pr(cfg, handoff, branch=handoff_branch, base=previous_branch, commit=head)
        else:
            push_commit(cfg, worktree, head, handoff_branch, session)
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
                expected_head=head,
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

        # Close superseded draft handoffs only after the replacement PR exists.
        # A pending close is retried on later syncs without losing the old identity.
        for old_record in publication.get("handoffHistory", []):
            if old_record.get("status") not in {"superseding", "close_pending"}:
                continue
            old_record["status"] = "close_pending"
            try:
                supersede_pr(cfg, old_record, handoff)
            except Exception:
                old_record["lastCloseError"] = utc_now()
                publication["updatedAt"] = utc_now()
                atomic_json(publication_path, publication)
                raise
            old_record["status"] = "superseded"
            old_record["supersededAt"] = old_record.get("supersededAt") or utc_now()
            old_record.pop("lastCloseError", None)
            publication["updatedAt"] = utc_now()
            atomic_json(publication_path, publication)

    publication["lastError"] = ""
    publication["updatedAt"] = utc_now()
    atomic_json(publication_path, publication)
    urls = [entry.get("prUrl", "") for entry in publication.get("checkpoints", []) if entry.get("prUrl")]
    handoff = publication.get("handoff") or {}
    if handoff.get("prUrl"):
        urls.append(handoff["prUrl"])
    print(f"GITHUB_PUBLISH_OK session={session} prs={len(urls)}")


def supersede_pr(cfg: dict, old: dict, replacement: dict) -> None:
    """Close an earlier handoff PR after validating its immutable identity."""
    number = int(old.get("prNumber", 0))
    if number <= 0:
        raise PermanentPublicationError("superseded handoff has no valid PR number")
    pr = api_request(cfg, "GET", f"/repos/{cfg['repository']}/pulls/{number}") or {}
    head = pr.get("head") or {}
    if (
        str((pr.get("base") or {}).get("ref", "")) != str(old.get("base", ""))
        or str(head.get("ref", "")) != str(old.get("branch", ""))
        or str(head.get("sha", "")) != str(old.get("commit", ""))
        or str(((head.get("repo") or {}).get("full_name", ""))) != cfg["repository"]
    ):
        raise PermanentPublicationError(f"superseded PR #{number} no longer matches its recorded handoff identity")
    if pr.get("state") == "closed":
        return
    if pr.get("state") != "open":
        raise PermanentPublicationError(f"superseded PR #{number} has unexpected state {pr.get('state')!r}")
    old_body = str(pr.get("body", "")).rstrip()
    marker = f"Superseded by [{replacement.get('branch')}@{replacement.get('commit')}]({replacement.get('prUrl', '')})."
    body = old_body if marker in old_body else (old_body + "\n\n" + marker).strip()
    api_request(cfg, "PATCH", f"/repos/{cfg['repository']}/pulls/{number}", {"state": "closed", "body": body})


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
        return 3 if isinstance(exc, PermanentPublicationError) else 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
