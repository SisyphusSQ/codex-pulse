#!/usr/bin/env python3
"""Inspect and prepare Codex Pulse releases without remote side effects."""

from __future__ import annotations

import argparse
import hashlib
import json
import plistlib
import re
import shlex
import subprocess
from dataclasses import asdict
from dataclasses import dataclass
from datetime import date
from pathlib import Path
from typing import Any


EXPECTED_REPOSITORY = "SisyphusSQ/codex-pulse"
EXPECTED_REMOTE = "github.com/SisyphusSQ/codex-pulse"
CHANGELOG_PATH = Path("CHANGELOG.md")
DEVELOPMENT_PLIST_PATH = Path("scripts/macos/Info.plist")
RELEASE_BUILD_SCRIPT = Path("scripts/macos/build-release-app.sh")
APP_LAUNCH_CONFIGURATION = Path(
    "app/macos/Sources/CodexPulseAppSupport/"
    "AppLaunchConfiguration.swift"
)
MAKEFILE_PATH = Path("Makefile")
RELEASE_NOTES_TEMPLATE = (
    Path(__file__).resolve().parent.parent
    / "assets"
    / "release-notes-template.md"
)
CHANGELOG_CATEGORIES = (
    "feature",
    "optimization",
    "bugFix",
    "note",
    "script",
)
SEMVER_PATTERN = re.compile(
    r"^v?"
    r"(?P<major>0|[1-9]\d*)\."
    r"(?P<minor>0|[1-9]\d*)\."
    r"(?P<patch>0|[1-9]\d*)"
    r"(?:-(?P<prerelease>[0-9A-Za-z-]+"
    r"(?:\.[0-9A-Za-z-]+)*))?"
    r"(?:\+(?P<build>[0-9A-Za-z-]+"
    r"(?:\.[0-9A-Za-z-]+)*))?$"
)
SHA_PATTERN = re.compile(r"^[0-9a-f]{40}$")
SHA256_PATTERN = re.compile(r"^[0-9a-f]{64}$")
DATE_PATTERN = re.compile(r"^\d{4}-\d{2}-\d{2}$")


@dataclass(frozen=True)
class Version:
    """Normalized release version surfaces."""

    tag: str
    product: str
    bundle_short: str
    prerelease: str

    @property
    def is_prerelease(self) -> bool:
        return bool(self.prerelease)


@dataclass(frozen=True)
class Finding:
    severity: str
    code: str
    message: str
    path: str = ""


def emit(payload: dict[str, Any], as_json: bool) -> None:
    if as_json:
        print(
            json.dumps(
                payload,
                ensure_ascii=False,
                indent=2,
                sort_keys=True,
            )
        )
        return
    for key, value in payload.items():
        if isinstance(value, (dict, list)):
            rendered = json.dumps(value, ensure_ascii=False, indent=2)
            print(f"{key}: {rendered}")
        else:
            print(f"{key}: {value}")


def parse_version(raw_value: str) -> Version:
    value = raw_value.strip()
    match = SEMVER_PATTERN.fullmatch(value)
    if match is None:
        raise SystemExit(f"invalid SemVer: {raw_value!r}")

    prerelease = match.group("prerelease") or ""
    for identifier in prerelease.split(".") if prerelease else ():
        if identifier.isdigit() and len(identifier) > 1:
            if identifier.startswith("0"):
                raise SystemExit(
                    "numeric prerelease identifiers must not have "
                    f"leading zeroes: {identifier!r}"
                )

    core = ".".join(
        (
            match.group("major"),
            match.group("minor"),
            match.group("patch"),
        )
    )
    product = core
    if prerelease:
        product += f"-{prerelease}"
    if match.group("build"):
        product += f"+{match.group('build')}"
    return Version(
        tag=f"v{product}",
        product=product,
        bundle_short=core,
        prerelease=prerelease,
    )


def ensure_repository(raw_path: str) -> Path:
    repository = Path(raw_path).expanduser().resolve()
    if not repository.is_dir():
        raise SystemExit(f"repository is not a directory: {repository}")
    git_marker = repository / ".git"
    if not git_marker.exists():
        raise SystemExit(f"repository has no .git marker: {repository}")
    return repository


def safe_repository_path(
    repository: Path,
    relative_path: Path,
    *,
    reject_symlink: bool = False,
) -> Path:
    canonical_repository = repository.resolve()
    candidate = canonical_repository / relative_path
    resolved = candidate.resolve()
    try:
        resolved.relative_to(canonical_repository)
    except ValueError:
        raise SystemExit(
            f"path resolves outside repository: {relative_path}"
        ) from None
    if reject_symlink and candidate.is_symlink():
        raise SystemExit(
            f"refusing to write through symlink: {relative_path}"
        )
    return candidate


def run_git(repository: Path, arguments: list[str]) -> str:
    result = subprocess.run(
        ["git", "-C", str(repository), *arguments],
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        message = result.stderr.strip() or result.stdout.strip()
        raise SystemExit(f"git {' '.join(arguments)} failed: {message}")
    return result.stdout.strip()


def normalize_remote(remote: str) -> str:
    value = remote.strip()
    if value.startswith("git@github.com:"):
        value = "github.com/" + value.removeprefix("git@github.com:")
    value = value.removeprefix("https://")
    value = value.removeprefix("http://")
    return value.removesuffix(".git")


def load_plist(path: Path) -> dict[str, Any]:
    with path.open("rb") as plist_file:
        payload = plistlib.load(plist_file)
    if not isinstance(payload, dict):
        raise SystemExit(f"plist root must be a dictionary: {path}")
    return payload


def inspect_repository(
    repository: Path,
    version: Version,
    channel: str,
) -> dict[str, Any]:
    findings: list[Finding] = []
    remote = run_git(repository, ["remote", "get-url", "origin"])
    branch = run_git(repository, ["branch", "--show-current"])
    head = run_git(repository, ["rev-parse", "HEAD"])
    status_lines = run_git(
        repository,
        ["status", "--porcelain=v1", "--untracked-files=all"],
    ).splitlines()

    if normalize_remote(remote) != EXPECTED_REMOTE:
        findings.append(
            Finding(
                "error",
                "repository.remote",
                "origin is not SisyphusSQ/codex-pulse",
            )
        )
    if status_lines:
        findings.append(
            Finding(
                "error",
                "git.dirty",
                f"working tree has {len(status_lines)} changed paths",
            )
        )
    if branch != "main":
        findings.append(
            Finding(
                "error",
                "git.branch",
                f"release branch is {branch or '<detached>'}, expected main",
            )
        )

    plist_path = repository / DEVELOPMENT_PLIST_PATH
    plist_values: dict[str, Any] = {}
    if plist_path.exists():
        plist_values = load_plist(plist_path)
        identifier = str(plist_values.get("CFBundleIdentifier", ""))
        display_name = str(plist_values.get("CFBundleDisplayName", ""))
        if identifier.endswith(".development"):
            findings.append(
                Finding(
                    "error",
                    "bundle.development_identifier",
                    "Info.plist still uses the development bundle identifier",
                    str(DEVELOPMENT_PLIST_PATH),
                )
            )
        if "Development" in display_name:
            findings.append(
                Finding(
                    "error",
                    "bundle.development_name",
                    "Info.plist still uses the development display name",
                    str(DEVELOPMENT_PLIST_PATH),
                )
            )
    else:
        findings.append(
            Finding(
                "error",
                "bundle.plist_missing",
                "development Info.plist is missing",
                str(DEVELOPMENT_PLIST_PATH),
            )
        )

    if not (repository / RELEASE_BUILD_SCRIPT).is_file():
        findings.append(
            Finding(
                "error",
                "release.build_script_missing",
                "project-owned release build script is missing",
                str(RELEASE_BUILD_SCRIPT),
            )
        )

    launch_path = repository / APP_LAUNCH_CONFIGURATION
    if launch_path.is_file():
        launch_text = launch_path.read_text(encoding="utf-8")
        if "/private/tmp/cp-app-" in launch_text:
            findings.append(
                Finding(
                    "error",
                    "runtime.ephemeral_default",
                    "App default runtime is still an ephemeral temp directory",
                    str(APP_LAUNCH_CONFIGURATION),
                )
            )
        if 'clientVersion: String = "dev"' in launch_text:
            findings.append(
                Finding(
                    "error",
                    "version.client_default_dev",
                    "App default client version is still dev",
                    str(APP_LAUNCH_CONFIGURATION),
                )
            )

    makefile_path = repository / MAKEFILE_PATH
    if makefile_path.is_file():
        makefile_text = makefile_path.read_text(encoding="utf-8")
        if "main.applicationVersion=dev" in makefile_text:
            findings.append(
                Finding(
                    "error",
                    "version.helper_dev",
                    "Helper build version is still dev",
                    str(MAKEFILE_PATH),
                )
            )

    if channel == "stable" and version.is_prerelease:
        findings.append(
            Finding(
                "error",
                "version.channel_mismatch",
                "stable channel cannot use a prerelease SemVer",
            )
        )
    if channel == "preview" and not version.is_prerelease:
        findings.append(
            Finding(
                "error",
                "version.channel_mismatch",
                "preview channel requires a prerelease SemVer",
            )
        )

    blockers = [
        asdict(item)
        for item in findings
        if item.severity == "error"
    ]
    source_preflight_ready = not blockers
    return {
        "repository": EXPECTED_REPOSITORY,
        "root": str(repository),
        "origin": remote,
        "branch": branch,
        "head": head,
        "dirty_path_count": len(status_lines),
        "version": asdict(version),
        "channel": channel,
        "bundle": {
            "identifier": plist_values.get("CFBundleIdentifier", ""),
            "display_name": plist_values.get("CFBundleDisplayName", ""),
            "short_version": plist_values.get(
                "CFBundleShortVersionString",
                "",
            ),
            "build_number": plist_values.get("CFBundleVersion", ""),
        },
        "source_preflight_ready": source_preflight_ready,
        "stable_release_ready": False,
        "readiness": (
            "manual_gates_required"
            if source_preflight_ready
            else "blocked"
        ),
        "blockers": blockers,
        "required_manual_gates": manual_gates(channel),
    }


def manual_gates(channel: str) -> list[str]:
    shared = [
        "make verify",
        "real Codex Home product acceptance",
        "remote tag, Release, asset, and SHA-256 readback",
    ]
    if channel == "stable":
        return [
            *shared,
            "signed and notarized final App ZIP",
            "fresh macOS user standard first-open and restart readback",
        ]
    return [
        *shared,
        (
            "explicit preview distribution decision: unsigned or "
            "signed-notarized"
        ),
        "fresh macOS user preview first-open and restart readback",
    ]


def version_plan(
    version: Version,
    current_build_number: int,
    build_number: int,
) -> dict[str, Any]:
    if current_build_number < 0:
        raise SystemExit(
            "current build number must be zero or a positive integer"
        )
    if build_number < 1:
        raise SystemExit("build number must be a positive integer")
    if build_number <= current_build_number:
        raise SystemExit(
            "build number must be greater than current build number"
        )
    return {
        "tag": version.tag,
        "product_version": version.product,
        "bundle_short_version": version.bundle_short,
        "current_bundle_build_number": str(current_build_number),
        "bundle_build_number": str(build_number),
        "contract_version": "unchanged unless core.proto changes",
        "data_schema_versions": "unchanged unless their schemas change",
        "writes": "none",
    }


def changelog_bounds(text: str) -> tuple[int, int, int] | None:
    heading = re.search(r"(?m)^## Unreleased\s*$", text)
    if heading is None:
        return None
    next_release = re.search(
        r"(?m)^## (?!Unreleased\s*$).+$",
        text[heading.end():],
    )
    end = len(text)
    if next_release is not None:
        end = heading.end() + next_release.start()
    return heading.start(), heading.end(), end


def empty_unreleased_block() -> str:
    lines = ["## Unreleased", ""]
    for category in CHANGELOG_CATEGORIES:
        lines.extend((f"#### {category}:", ""))
    return "\n".join(lines).rstrip() + "\n\n"


def archive_changelog(
    repository: Path,
    version: Version,
    release_date: str,
    write: bool,
) -> dict[str, Any]:
    if version.is_prerelease:
        raise SystemExit("archive-changelog only accepts a stable version")
    if DATE_PATTERN.fullmatch(release_date) is None:
        raise SystemExit("release date must use YYYY-MM-DD")
    date.fromisoformat(release_date)

    changelog = safe_repository_path(
        repository,
        CHANGELOG_PATH,
        reject_symlink=True,
    )
    original = changelog.read_text(encoding="utf-8")
    bounds = changelog_bounds(original)
    if bounds is None:
        raise SystemExit("CHANGELOG.md has no Unreleased section")
    start, heading_end, end = bounds
    unreleased_body = original[heading_end:end].strip()
    item_count = len(
        re.findall(r"(?m)^\d+\.\s+\S", unreleased_body)
    )
    if item_count == 0:
        raise SystemExit("Unreleased contains no numbered entries")
    release_heading = f"## {version.tag} - {release_date}"
    if re.search(
        rf"(?m)^## {re.escape(version.tag)}(?:\s|$)",
        original,
    ):
        raise SystemExit(f"CHANGELOG already contains {version.tag}")

    replacement = (
        empty_unreleased_block()
        + release_heading
        + "\n\n"
        + unreleased_body
        + "\n\n"
    )
    updated = (
        original[:start]
        + replacement
        + original[end:].lstrip("\n")
    )
    if write:
        changelog.write_text(updated, encoding="utf-8")
    return {
        "write": write,
        "changed": updated != original,
        "path": str(CHANGELOG_PATH),
        "release_heading": release_heading,
        "archived_item_count": item_count,
        "target": "CHANGELOG.md -> Unreleased",
    }


def preview_first_open() -> str:
    return "\n".join(
        (
            "Codex Pulse 当前是未签名、未公证的开发预览版。"
            "macOS 首次启动时会阻止应用打开。",
            "",
            "1. 下载并解压发行 ZIP。",
            "2. 将 `Codex Pulse.app` 拖入“应用程序”文件夹。",
            "3. 双击应用。macOS 可能询问是否移到废纸篓；"
            "请不要移到废纸篓，关闭该提示。",
            "4. 打开“系统设置 → 隐私与安全性”。",
            "5. 找到 Codex Pulse 被阻止打开的提示，"
            "点击“仍要打开”。",
            "6. 输入登录密码或使用 Touch ID。",
            "7. 再次出现确认窗口时，点击“打开”。",
            "",
            "如果没有看到“仍要打开”，请重新尝试启动一次"
            "应用，再返回“隐私与安全性”查看。"
            "不要关闭 Gatekeeper。",
        )
    )


def stable_first_open() -> str:
    return "\n".join(
        (
            "1. 下载并解压发行 ZIP。",
            "2. 将 `Codex Pulse.app` 拖入“应用程序”文件夹。",
            "3. 双击打开；macOS 首次确认从互联网下载的"
            "应用时，选择“打开”。",
            "4. 按应用提示确认 Codex Home。"
            "默认路径为 `~/.codex`。",
            "5. 在“本机状态”中查看首次索引进度和"
            "数据健康状态。",
        )
    )


def render_release_notes(
    version: Version,
    channel: str,
    distribution: str,
    summary: str,
    sha256: str,
) -> str:
    if channel == "stable" and version.is_prerelease:
        raise SystemExit("stable notes cannot use a prerelease SemVer")
    if channel == "preview" and not version.is_prerelease:
        raise SystemExit("preview notes require a prerelease SemVer")
    if distribution not in ("unsigned", "signed-notarized"):
        raise SystemExit(f"unsupported distribution: {distribution}")
    if channel == "stable" and distribution != "signed-notarized":
        raise SystemExit(
            "stable notes require a signed-notarized distribution"
        )
    if sha256 != "<待生成>" and SHA256_PATTERN.fullmatch(sha256) is None:
        raise SystemExit("sha256 must be 64 lowercase hexadecimal characters")

    template = RELEASE_NOTES_TEMPLATE.read_text(encoding="utf-8")
    if distribution == "unsigned":
        notice = (
            "> 这是开发预览版，尚未完成 Developer ID 签名和 "
            "Apple 公证。"
        )
        first_open = preview_first_open()
        limitations = (
            "- 未签名、未公证，仅供已了解风险的"
            "测试用户使用。\n"
            "- 首次打开需要在“隐私与安全性”中手工允许。"
        )
    else:
        if channel == "preview":
            notice = (
                "> 这是预发布版本；发行资产已通过 Developer ID "
                "签名、Apple 公证和资产校验。"
            )
        else:
            notice = (
                "> 本版本已通过 Developer ID 签名、Apple 公证和"
                "发布资产校验。"
            )
        first_open = stable_first_open()
        limitations = (
            "- 请按本版本实际限制补充；"
            "不得保留此占位文本。"
        )

    replacements = {
        "{{VERSION}}": version.tag,
        "{{RELEASE_KIND_NOTICE}}": notice,
        "{{SUMMARY}}": summary.strip(),
        "{{CHANGE_1}}": "请填写用户可感知的变化",
        "{{CHANGE_2}}": "请填写重要修复或兼容性变化",
        "{{CHANGE_3}}": "请填写验证或数据语义变化",
        "{{FIRST_OPEN_INSTRUCTIONS}}": first_open,
        "{{KNOWN_LIMITATIONS}}": limitations,
        "{{SHA256}}": sha256,
        "{{CHANGELOG_URL}}": (
            f"https://github.com/{EXPECTED_REPOSITORY}/compare/"
            f"<previous-tag>...{version.tag}"
        ),
    }
    rendered = template
    for placeholder, value in replacements.items():
        rendered = rendered.replace(placeholder, value)
    return rendered


def release_output_directory(
    repository: Path,
    version: Version,
) -> Path:
    repository = repository.resolve()
    output = safe_repository_path(
        repository,
        Path(".artifacts") / "releases" / version.tag,
    ).resolve()
    allowed_root = (repository / ".artifacts" / "releases").resolve()
    try:
        allowed_root.relative_to(repository)
        output.relative_to(allowed_root)
    except ValueError:
        raise SystemExit(
            "release output resolves outside repository"
        ) from None
    return output


def render_notes_command(
    repository: Path,
    version: Version,
    channel: str,
    distribution: str,
    summary: str,
    sha256: str,
    write: bool,
) -> dict[str, Any]:
    rendered = render_release_notes(
        version,
        channel,
        distribution,
        summary,
        sha256,
    )
    output = release_output_directory(repository, version)
    notes_path = output / "release-notes.md"
    if notes_path.exists() and notes_path.is_symlink():
        raise SystemExit(f"release notes target is a symlink: {notes_path}")
    if write:
        output.mkdir(mode=0o700, parents=True, exist_ok=True)
        notes_path.write_text(rendered, encoding="utf-8")
    return {
        "write": write,
        "path": str(notes_path),
        "version": version.tag,
        "channel": channel,
        "distribution": distribution,
        "content": rendered,
    }


def shell_command(arguments: list[str]) -> str:
    return shlex.join(arguments)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def local_tag_exists(repository: Path, tag: str) -> bool:
    result = subprocess.run(
        [
            "git",
            "-C",
            str(repository),
            "show-ref",
            "--verify",
            "--quiet",
            f"refs/tags/{tag}",
        ],
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode not in (0, 1):
        raise SystemExit(
            f"unable to inspect local tag {tag}: "
            f"{result.stderr.strip()}"
        )
    return result.returncode == 0


def validate_local_release_inputs(
    repository: Path,
    version: Version,
    channel: str,
    release_sha: str,
) -> dict[str, str]:
    repository = repository.resolve()
    inspection = inspect_repository(
        repository,
        version,
        channel,
    )
    if inspection["blockers"]:
        codes = ", ".join(
            item["code"] for item in inspection["blockers"]
        )
        raise SystemExit(f"release preflight is blocked: {codes}")
    if inspection["head"] != release_sha:
        raise SystemExit(
            "release SHA does not match the checked-out HEAD"
        )
    if local_tag_exists(repository, version.tag):
        raise SystemExit(f"local tag already exists: {version.tag}")

    release_root = Path(".artifacts") / "releases" / version.tag
    artifact_name = (
        f"Codex-Pulse-{version.tag}-macos-arm64.zip"
    )
    artifact = safe_repository_path(
        repository,
        release_root / artifact_name,
    )
    checksums = safe_repository_path(
        repository,
        release_root / "SHA256SUMS",
    )
    notes = safe_repository_path(
        repository,
        release_root / "release-notes.md",
    )
    for required_path in (artifact, checksums, notes):
        if not required_path.is_file() or required_path.stat().st_size == 0:
            relative = required_path.relative_to(repository)
            raise SystemExit(
                f"required release input is missing or empty: {relative}"
            )

    digest = sha256_file(artifact)
    checksum_lines = checksums.read_text(
        encoding="utf-8"
    ).splitlines()
    checksum_matches = any(
        digest in line and artifact_name in line
        for line in checksum_lines
    )
    if not checksum_matches:
        raise SystemExit(
            "SHA256SUMS does not match the release artifact"
        )

    notes_text = notes.read_text(encoding="utf-8")
    if "{{" in notes_text or "请填写" in notes_text:
        raise SystemExit("release notes still contain placeholders")
    if version.tag not in notes_text:
        raise SystemExit("release notes do not mention the target version")

    return {
        "artifact": str(artifact.relative_to(repository)),
        "checksums": str(checksums.relative_to(repository)),
        "notes": str(notes.relative_to(repository)),
        "sha256": digest,
    }


def release_plan(
    version: Version,
    channel: str,
    release_sha: str,
) -> dict[str, Any]:
    if SHA_PATTERN.fullmatch(release_sha) is None:
        raise SystemExit(
            "release SHA must be 40 lowercase hexadecimal characters"
        )
    if channel == "stable" and version.is_prerelease:
        raise SystemExit("stable release cannot use a prerelease SemVer")
    if channel == "preview" and not version.is_prerelease:
        raise SystemExit("preview release requires a prerelease SemVer")

    release_dir = f".artifacts/releases/{version.tag}"
    artifact = (
        f"{release_dir}/Codex-Pulse-{version.tag}-macos-arm64.zip"
    )
    checksums = f"{release_dir}/SHA256SUMS"
    notes = f"{release_dir}/release-notes.md"
    create_arguments = [
        "gh",
        "release",
        "create",
        version.tag,
        f"{artifact}#Codex Pulse for macOS (Apple Silicon)",
        checksums,
        "--repo",
        EXPECTED_REPOSITORY,
        "--verify-tag",
        "--draft",
        "--title",
        f"Codex Pulse {version.tag}",
        "--notes-file",
        notes,
    ]
    if channel == "preview":
        create_arguments.append("--prerelease")

    phases = [
        {
            "name": "create_local_signed_tag",
            "scope": "local_git",
            "requires_approval": True,
            "commands": [
                shell_command(
                    [
                        "git",
                        "tag",
                        "-s",
                        version.tag,
                        release_sha,
                        "-m",
                        f"Codex Pulse {version.tag}",
                    ]
                ),
                shell_command(["git", "tag", "-v", version.tag]),
            ],
        },
        {
            "name": "push_tag",
            "scope": "remote_git",
            "requires_approval": True,
            "commands": [
                shell_command(
                    [
                        "git",
                        "push",
                        "origin",
                        f"refs/tags/{version.tag}",
                    ]
                ),
            ],
        },
        {
            "name": "verify_remote_tag",
            "scope": "remote_readback",
            "requires_approval": False,
            "commands": [
                shell_command(
                    [
                        "git",
                        "ls-remote",
                        "--tags",
                        "origin",
                        f"refs/tags/{version.tag}",
                        f"refs/tags/{version.tag}^{{}}",
                    ]
                ),
            ],
        },
        {
            "name": "create_draft_release",
            "scope": "github",
            "requires_approval": True,
            "commands": [shell_command(create_arguments)],
        },
        {
            "name": "verify_draft_release",
            "scope": "github_readback",
            "requires_approval": False,
            "commands": [
                shell_command(
                    [
                        "gh",
                        "release",
                        "view",
                        version.tag,
                        "--repo",
                        EXPECTED_REPOSITORY,
                        "--json",
                        (
                            "tagName,targetCommitish,isDraft,"
                            "isPrerelease,name,body,assets,url"
                        ),
                    ]
                ),
            ],
        },
        {
            "name": "publish_release",
            "scope": "github",
            "requires_approval": True,
            "requires_separate_approval": True,
            "commands": [
                shell_command(
                    [
                        "gh",
                        "release",
                        "edit",
                        version.tag,
                        "--repo",
                        EXPECTED_REPOSITORY,
                        "--draft=false",
                    ]
                ),
            ],
        },
    ]
    return {
        "version": version.tag,
        "channel": channel,
        "release_sha": release_sha,
        "artifact": artifact,
        "checksums": checksums,
        "notes": notes,
        "phases": phases,
        "side_effects": (
            "plan only; run each mutating command separately after approval"
        ),
    }


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)

    check = commands.add_parser("check")
    check.add_argument("--repo", required=True)
    check.add_argument("--version", required=True)
    check.add_argument(
        "--channel",
        choices=("stable", "preview"),
        required=True,
    )
    check.add_argument("--json", action="store_true")

    plan = commands.add_parser("version-plan")
    plan.add_argument("--version", required=True)
    plan.add_argument(
        "--current-build-number",
        required=True,
        type=int,
    )
    plan.add_argument("--build-number", required=True, type=int)
    plan.add_argument("--json", action="store_true")

    archive = commands.add_parser("archive-changelog")
    archive.add_argument("--repo", required=True)
    archive.add_argument("--version", required=True)
    archive.add_argument("--date", required=True)
    archive.add_argument("--write", action="store_true")
    archive.add_argument("--json", action="store_true")

    notes = commands.add_parser("render-notes")
    notes.add_argument("--repo", required=True)
    notes.add_argument("--version", required=True)
    notes.add_argument(
        "--channel",
        choices=("stable", "preview"),
        required=True,
    )
    notes.add_argument(
        "--distribution",
        choices=("unsigned", "signed-notarized"),
        required=True,
    )
    notes.add_argument("--summary", required=True)
    notes.add_argument("--sha256", default="<待生成>")
    notes.add_argument("--write", action="store_true")
    notes.add_argument("--json", action="store_true")

    release = commands.add_parser("release-plan")
    release.add_argument("--repo", required=True)
    release.add_argument("--version", required=True)
    release.add_argument(
        "--channel",
        choices=("stable", "preview"),
        required=True,
    )
    release.add_argument("--release-sha", required=True)
    release.add_argument("--json", action="store_true")

    return parser


def main(argv: list[str] | None = None) -> int:
    arguments = build_parser().parse_args(argv)

    if arguments.command == "check":
        repository = ensure_repository(arguments.repo)
        version = parse_version(arguments.version)
        payload = inspect_repository(
            repository,
            version,
            arguments.channel,
        )
    elif arguments.command == "version-plan":
        payload = version_plan(
            parse_version(arguments.version),
            arguments.current_build_number,
            arguments.build_number,
        )
    elif arguments.command == "archive-changelog":
        payload = archive_changelog(
            ensure_repository(arguments.repo),
            parse_version(arguments.version),
            arguments.date,
            arguments.write,
        )
    elif arguments.command == "render-notes":
        payload = render_notes_command(
            ensure_repository(arguments.repo),
            parse_version(arguments.version),
            arguments.channel,
            arguments.distribution,
            arguments.summary,
            arguments.sha256,
            arguments.write,
        )
    elif arguments.command == "release-plan":
        repository = ensure_repository(arguments.repo)
        version = parse_version(arguments.version)
        validate_local_release_inputs(
            repository,
            version,
            arguments.channel,
            arguments.release_sha,
        )
        payload = release_plan(
            version,
            arguments.channel,
            arguments.release_sha,
        )
    else:
        raise SystemExit(f"unsupported command: {arguments.command}")

    emit(payload, arguments.json)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
