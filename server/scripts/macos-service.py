#!/usr/bin/env python3
"""Manage a user-owned center LaunchAgent and immutable release directories."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import plistlib
import re
import shutil
import stat
import subprocess
import tempfile
from pathlib import Path


def private_directory(path: Path) -> None:
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    metadata = path.stat()
    if metadata.st_uid != os.getuid() or metadata.st_mode & 0o077:
        raise SystemExit("service directories must be user-owned and 0700")


def service_loaded(target: str) -> bool:
    result = subprocess.run(
        ["launchctl", "print", target], capture_output=True, timeout=15
    )
    return result.returncode == 0


def stop(target: str) -> None:
    if service_loaded(target):
        subprocess.run(
            ["launchctl", "bootout", target], check=True, timeout=30
        )


def validate_managed(plist: Path, root: Path, label: str) -> None:
    if plist.is_symlink():
        raise SystemExit("LaunchAgent plist must not be a symlink")
    if plist.exists():
        metadata = plist.stat()
        saved = plistlib.loads(plist.read_bytes())
        expected = str(root / "current/codex-pulse-server")
        arguments = saved.get("ProgramArguments", [])
        if (metadata.st_uid != os.getuid()
                or saved.get("Label") != label
                or not arguments or arguments[0] != expected):
            raise SystemExit("refusing to change an unrelated LaunchAgent")


def install(args: argparse.Namespace, root: Path, plist: Path,
            target: str, label: str) -> None:
    package = args.package.resolve(strict=True)
    config = args.config.resolve(strict=True)
    metadata = config.stat()
    if (not stat.S_ISREG(metadata.st_mode)
            or metadata.st_uid != os.getuid()
            or stat.S_IMODE(metadata.st_mode) != 0o600):
        raise SystemExit("configuration must be user-owned and 0600")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,80}", args.release_id):
        raise SystemExit("release-id must be a safe version/commit identifier")
    binary = package / "codex-pulse-server"
    if not binary.is_file() or binary.is_symlink():
        raise SystemExit("package must contain the center binary")
    for path in package.rglob("*"):
        if path.is_symlink() or ".local." in path.name:
            raise SystemExit("release packages cannot include links/private config")
    current = root / "current"
    if current.exists() and not current.is_symlink():
        raise SystemExit("current must be a managed release link")
    releases = root / "releases"
    private_directory(releases)
    release = releases / args.release_id
    if release.exists():
        raise SystemExit("release directory already exists; use a new release-id")
    shutil.copytree(package, release)
    release.chmod(0o700)
    digest = hashlib.sha256((release / binary.name).read_bytes()).hexdigest()
    manifest = {"release_id": args.release_id, "sha256": digest}
    (release / "deployment.json").write_text(json.dumps(manifest, indent=2))
    (release / "deployment.json").chmod(0o600)
    logs = root / "logs"
    private_directory(logs)
    for name in ("stdout.log", "stderr.log"):
        path = logs / name
        path.touch(mode=0o600, exist_ok=True)
        path.chmod(0o600)
    stop(target)
    link = root / ".next-release"
    if link.exists() or link.is_symlink():
        raise SystemExit("unfinished release link exists; inspect it first")
    link.symlink_to(release, target_is_directory=True)
    link.replace(current)
    definition = {
        "Label": label,
        "ProgramArguments": [str(current / binary.name), "--config",
                             str(config), "http"],
        "WorkingDirectory": str(root),
        "RunAtLoad": True,
        "KeepAlive": {"SuccessfulExit": False},
        "ThrottleInterval": 10,
        "ExitTimeOut": 15,
        "Umask": 0o077,
        "StandardOutPath": str(logs / "stdout.log"),
        "StandardErrorPath": str(logs / "stderr.log"),
    }
    plist.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=plist.parent, delete=False) as file:
        staged = Path(file.name)
        file.write(plistlib.dumps(definition))
    staged.chmod(0o600)
    staged.replace(plist)
    subprocess.run(
        ["launchctl", "bootstrap", f"gui/{os.getuid()}", str(plist)],
        check=True, timeout=30,
    )
    print(json.dumps({"installed": label, **manifest}))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("install", "start", "stop",
                                          "restart", "status", "remove"))
    parser.add_argument("--environment", choices=("development", "production"),
                        required=True)
    parser.add_argument("--root", type=Path,
                        default=Path.home() / "Library/Application Support"
                        / "Codex Pulse Center")
    parser.add_argument("--package", type=Path)
    parser.add_argument("--config", type=Path)
    parser.add_argument("--release-id")
    args = parser.parse_args()
    if os.uname().sysname != "Darwin":
        raise SystemExit("center LaunchAgent management requires macOS")
    root = args.root.expanduser().absolute() / args.environment
    label = "com.sisyphussq.codexpulse.center." + args.environment
    target = f"gui/{os.getuid()}/{label}"
    plist = Path.home() / "Library/LaunchAgents" / (label + ".plist")
    validate_managed(plist, root, label)
    if service_loaded(target) and not plist.exists():
        raise SystemExit("loaded service has no managed plist; inspect it first")
    if args.action == "install":
        if not all((args.package, args.config, args.release_id)):
            parser.error("install requires package, config and release-id")
        private_directory(root)
        install(args, root, plist, target, label)
    elif args.action == "stop":
        stop(target)
    elif args.action == "remove":
        stop(target)
        plist.unlink(missing_ok=True)
        print("LaunchAgent removed; releases/configuration/history retained")
    elif args.action == "status":
        subprocess.run(["launchctl", "print", target], check=True, timeout=15)
    else:
        if not plist.is_file():
            raise SystemExit("install the service before starting it")
        if args.action == "restart":
            stop(target)
        if not service_loaded(target):
            subprocess.run(
                ["launchctl", "bootstrap", f"gui/{os.getuid()}", str(plist)],
                check=True, timeout=30,
            )


if __name__ == "__main__":
    main()
