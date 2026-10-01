#!/usr/bin/env python3
"""Regression test for target repositories that pin their Node major."""
import os
import subprocess
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PROVISION = ROOT / "scripts" / "provision-box.sh"


def node_installer_function() -> str:
    source = PROVISION.read_text(encoding="utf-8")
    start = source.index("install_target_node() {")
    end = source.index("\n}\ninstall_target_node", start) + 2
    return source[start:end]


def main() -> None:
    with tempfile.TemporaryDirectory() as temp:
        root = Path(temp)
        repo = root / "repo"
        fake_bin = root / "bin"
        repo.mkdir()
        fake_bin.mkdir()
        (repo / ".node-version").write_text("24\n", encoding="utf-8")
        version_file = root / "installed-node-version"
        version_file.write_text("v22.23.3\n", encoding="utf-8")
        log_file = root / "commands.log"

        (fake_bin / "node").write_text(
            "#!/bin/sh\ncat \"$NODE_VERSION_FILE\"\n", encoding="utf-8"
        )
        (fake_bin / "curl").write_text(
            "#!/bin/sh\n"
            "while [ $# -gt 0 ]; do\n"
            "  if [ \"$1\" = \"-o\" ]; then shift; output=$1; fi\n"
            "  printf '%s ' \"$1\" >>\"$COMMAND_LOG\"\n"
            "  shift\n"
            "done\n"
            "printf 'exit 0\\n' >\"$output\"\n",
            encoding="utf-8",
        )
        (fake_bin / "apt-get").write_text(
            "#!/bin/sh\n"
            "printf 'apt-get %s\\n' \"$*\" >>\"$COMMAND_LOG\"\n"
            "printf 'v24.21.0\\n' >\"$NODE_VERSION_FILE\"\n",
            encoding="utf-8",
        )
        for command in fake_bin.iterdir():
            command.chmod(0o755)

        script = "\n".join(
            [
                "set -Eeuo pipefail",
                f"TARGET_REPO={str(repo)!r}",
                "RPT() { :; }",
                "fail() { echo \"$*\" >&2; exit 1; }",
                node_installer_function(),
                "install_target_node",
            ]
        )
        env = os.environ.copy()
        env.update(
            {
                "PATH": f"{fake_bin}:{env['PATH']}",
                "NODE_VERSION_FILE": str(version_file),
                "COMMAND_LOG": str(log_file),
            }
        )
        result = subprocess.run(["bash", "-c", script], env=env, capture_output=True, text=True)
        assert result.returncode == 0, result.stderr
        assert version_file.read_text(encoding="utf-8").strip() == "v24.21.0"
        commands = log_file.read_text(encoding="utf-8")
        assert "https://deb.nodesource.com/setup_24.x" in commands
        assert "apt-get install -y nodejs" in commands

        log_file.write_text("", encoding="utf-8")
        result = subprocess.run(["bash", "-c", script], env=env, capture_output=True, text=True)
        assert result.returncode == 0, result.stderr
        assert log_file.read_text(encoding="utf-8") == "", "matching Node major should not reinstall"

        (repo / ".node-version").write_text("not-a-version\n", encoding="utf-8")
        result = subprocess.run(["bash", "-c", script], env=env, capture_output=True, text=True)
        assert result.returncode != 0 and ".node-version" in result.stderr

    print("target Node runtime provisioning passed for upgrade, reuse, and invalid pin")


if __name__ == "__main__":
    main()
