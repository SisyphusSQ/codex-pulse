"""Tests for the Codex Pulse project release helper."""

from __future__ import annotations

import importlib.util
import io
import plistlib
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr
from pathlib import Path
from types import ModuleType


SKILL_ROOT = Path(__file__).resolve().parent.parent
SCRIPT_PATH = SKILL_ROOT / "scripts" / "project_version_release.py"


def load_release_module() -> ModuleType:
    spec = importlib.util.spec_from_file_location(
        "project_version_release",
        SCRIPT_PATH,
    )
    if spec is None or spec.loader is None:
        raise RuntimeError("unable to load project_version_release")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


release = load_release_module()


def run_git(repository: Path, *arguments: str) -> None:
    subprocess.run(
        ["git", "-C", str(repository), *arguments],
        check=True,
        capture_output=True,
        text=True,
    )


class VersionTests(unittest.TestCase):
    def test_stable_and_prerelease_surfaces(self) -> None:
        stable = release.parse_version("v1.2.3")
        preview = release.parse_version("1.2.3-beta.4")

        self.assertEqual(stable.tag, "v1.2.3")
        self.assertEqual(stable.bundle_short, "1.2.3")
        self.assertFalse(stable.is_prerelease)
        self.assertEqual(preview.tag, "v1.2.3-beta.4")
        self.assertEqual(preview.bundle_short, "1.2.3")
        self.assertTrue(preview.is_prerelease)

    def test_numeric_prerelease_rejects_leading_zero(self) -> None:
        with self.assertRaises(SystemExit):
            release.parse_version("v1.2.3-beta.01")

    def test_bundle_build_number_must_be_positive(self) -> None:
        with self.assertRaises(SystemExit):
            release.version_plan(
                release.parse_version("v1.2.3"),
                1,
                0,
            )

    def test_bundle_build_number_must_increase(self) -> None:
        with self.assertRaises(SystemExit):
            release.version_plan(
                release.parse_version("v1.2.3"),
                42,
                42,
            )


class ReleaseNotesTests(unittest.TestCase):
    def test_preview_notes_match_gatekeeper_flow(self) -> None:
        rendered = release.render_release_notes(
            release.parse_version("v0.1.0-beta.1"),
            "preview",
            "unsigned",
            "开发预览",
            "<待生成>",
        )

        self.assertIn("请不要移到废纸篓", rendered)
        self.assertIn("系统设置 → 隐私与安全性", rendered)
        self.assertIn("仍要打开", rendered)
        self.assertIn("不要关闭 Gatekeeper", rendered)
        self.assertNotIn("{{", rendered)

    def test_stable_notes_do_not_include_gatekeeper_bypass(self) -> None:
        rendered = release.render_release_notes(
            release.parse_version("v0.1.0"),
            "stable",
            "signed-notarized",
            "正式版本",
            "a" * 64,
        )

        self.assertNotIn("移到废纸篓", rendered)
        self.assertNotIn("仍要打开", rendered)
        self.assertIn("Developer ID", rendered)

    def test_preview_requires_prerelease_semver(self) -> None:
        with self.assertRaises(SystemExit):
            release.render_release_notes(
                release.parse_version("v0.1.0"),
                "preview",
                "unsigned",
                "错误版本",
                "<待生成>",
            )

    def test_stable_notes_require_signed_distribution(
        self,
    ) -> None:
        with self.assertRaises(SystemExit):
            release.render_release_notes(
                release.parse_version("v0.1.0"),
                "stable",
                "unsigned",
                "正式版本",
                "a" * 64,
            )

    def test_signed_preview_uses_standard_first_open(self) -> None:
        rendered = release.render_release_notes(
            release.parse_version("v0.1.0-beta.1"),
            "preview",
            "signed-notarized",
            "已签名测试版",
            "a" * 64,
        )

        self.assertIn("预发布版本", rendered)
        self.assertNotIn("移到废纸篓", rendered)

    def test_write_target_is_fixed_under_release_artifacts(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            repository = Path(temporary)
            result = release.render_notes_command(
                repository,
                release.parse_version("v0.1.0-beta.1"),
                "preview",
                "unsigned",
                "开发预览",
                "<待生成>",
                True,
            )
            notes_path = Path(result["path"])

            self.assertTrue(notes_path.is_file())
            self.assertEqual(
                notes_path.relative_to(repository.resolve()),
                Path(
                    ".artifacts/releases/v0.1.0-beta.1/"
                    "release-notes.md"
                ),
            )

    def test_release_output_rejects_symlink_escape(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            repository = root / "repo"
            external = root / "external"
            repository.mkdir()
            external.mkdir()
            (repository / ".artifacts").symlink_to(
                external,
                target_is_directory=True,
            )

            with self.assertRaises(SystemExit):
                release.render_notes_command(
                    repository,
                    release.parse_version("v0.1.0-beta.1"),
                    "preview",
                    "unsigned",
                    "开发预览",
                    "<待生成>",
                    True,
                )


class ChangelogTests(unittest.TestCase):
    def test_archive_rejects_changelog_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            repository = root / "repo"
            repository.mkdir()
            external = root / "outside.md"
            external.write_text(
                "## Unreleased\n\n#### feature:\n1. 外部内容\n",
                encoding="utf-8",
            )
            (repository / "CHANGELOG.md").symlink_to(external)

            with self.assertRaises(SystemExit):
                release.archive_changelog(
                    repository,
                    release.parse_version("v0.1.0"),
                    "2026-07-24",
                    True,
                )

    def test_archive_is_dry_run_by_default_and_preserves_go_mod(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            repository = Path(temporary)
            changelog = repository / "CHANGELOG.md"
            go_mod = repository / "go.mod"
            changelog.write_text(
                "## Unreleased\n\n"
                "#### feature:\n"
                "1. 新能力\n\n"
                "#### bugFix:\n"
                "1. 修复问题\n",
                encoding="utf-8",
            )
            go_mod.write_text(
                "module github.com/example/project\n\ngo 1.25.0\n",
                encoding="utf-8",
            )
            original_changelog = changelog.read_text(encoding="utf-8")
            original_go_mod = go_mod.read_text(encoding="utf-8")

            result = release.archive_changelog(
                repository,
                release.parse_version("v0.1.0"),
                "2026-07-24",
                False,
            )

            self.assertTrue(result["changed"])
            self.assertEqual(
                changelog.read_text(encoding="utf-8"),
                original_changelog,
            )
            self.assertEqual(
                go_mod.read_text(encoding="utf-8"),
                original_go_mod,
            )

    def test_archive_write_resets_unreleased_and_adds_release(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            repository = Path(temporary)
            changelog = repository / "CHANGELOG.md"
            changelog.write_text(
                "前言\n\n"
                "## Unreleased\n\n"
                "#### feature:\n"
                "1. 新能力\n",
                encoding="utf-8",
            )

            release.archive_changelog(
                repository,
                release.parse_version("v0.1.0"),
                "2026-07-24",
                True,
            )
            rendered = changelog.read_text(encoding="utf-8")

            self.assertIn("## Unreleased", rendered)
            self.assertIn("## v0.1.0 - 2026-07-24", rendered)
            self.assertEqual(rendered.count("1. 新能力"), 1)


class RepositoryCheckTests(unittest.TestCase):
    def create_repository(self, root: Path) -> Path:
        repository = root / "repo"
        repository.mkdir()
        run_git(repository, "init", "-b", "main")
        run_git(
            repository,
            "remote",
            "add",
            "origin",
            "https://github.com/SisyphusSQ/codex-pulse.git",
        )
        (repository / "scripts" / "macos").mkdir(parents=True)
        (repository / "app" / "macos" / "Sources").mkdir(
            parents=True
        )
        with (
            repository / "scripts" / "macos" / "Info.plist"
        ).open("wb") as plist_file:
            plistlib.dump(
                {
                    "CFBundleIdentifier": "com.example.release",
                    "CFBundleDisplayName": "Codex Pulse",
                    "CFBundleShortVersionString": "0.1.0",
                    "CFBundleVersion": "1",
                },
                plist_file,
            )
        (repository / "scripts" / "macos" / "build-release-app.sh").write_text(
            "#!/bin/sh\n",
            encoding="utf-8",
        )
        launch = (
            repository
            / "app"
            / "macos"
            / "Sources"
            / "CodexPulseAppSupport"
        )
        launch.mkdir()
        (launch / "AppLaunchConfiguration.swift").write_text(
            'let clientVersion = "0.1.0"\n',
            encoding="utf-8",
        )
        (repository / "Makefile").write_text(
            "release:\n\t@true\n",
            encoding="utf-8",
        )
        (repository / ".gitignore").write_text(
            ".artifacts/\n",
            encoding="utf-8",
        )
        run_git(repository, "add", ".")
        run_git(
            repository,
            "-c",
            "user.name=Test",
            "-c",
            "user.email=test@example.com",
            "commit",
            "-m",
            "fixture",
        )
        return repository

    def create_release_inputs(
        self,
        repository: Path,
        tag: str,
    ) -> str:
        release_root = (
            repository / ".artifacts" / "releases" / tag
        )
        release_root.mkdir(parents=True)
        artifact = (
            release_root
            / f"Codex-Pulse-{tag}-macos-arm64.zip"
        )
        artifact.write_bytes(b"release archive")
        digest = release.sha256_file(artifact)
        (release_root / "SHA256SUMS").write_text(
            f"{digest}  {artifact.name}\n",
            encoding="utf-8",
        )
        (release_root / "release-notes.md").write_text(
            f"# Codex Pulse {tag}\n\nReady for review.\n",
            encoding="utf-8",
        )
        return digest

    def test_clean_fixture_still_requires_manual_release_gates(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            repository = self.create_repository(Path(temporary))

            result = release.inspect_repository(
                repository,
                release.parse_version("v0.1.0"),
                "stable",
            )

            self.assertTrue(result["source_preflight_ready"])
            self.assertFalse(result["stable_release_ready"])
            self.assertEqual(
                result["readiness"],
                "manual_gates_required",
            )
            self.assertEqual(result["blockers"], [])

    def test_dirty_repository_is_blocked(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            repository = self.create_repository(Path(temporary))
            (repository / "dirty.txt").write_text(
                "dirty\n",
                encoding="utf-8",
            )

            result = release.inspect_repository(
                repository,
                release.parse_version("v0.1.0"),
                "stable",
            )

            codes = {item["code"] for item in result["blockers"]}
            self.assertIn("git.dirty", codes)
            self.assertFalse(result["source_preflight_ready"])
            self.assertFalse(result["stable_release_ready"])

    def test_local_release_inputs_verify_assets_and_checksum(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            repository = self.create_repository(Path(temporary))
            digest = self.create_release_inputs(
                repository,
                "v0.1.0-beta.1",
            )
            head = release.run_git(
                repository,
                ["rev-parse", "HEAD"],
            )

            result = release.validate_local_release_inputs(
                repository,
                release.parse_version("v0.1.0-beta.1"),
                "preview",
                head,
            )

            self.assertEqual(result["sha256"], digest)


class ReleasePlanTests(unittest.TestCase):
    def test_preview_plan_is_draft_and_verifies_existing_tag(self) -> None:
        result = release.release_plan(
            release.parse_version("v0.1.0-beta.1"),
            "preview",
            "a" * 40,
        )
        commands = "\n".join(
            command
            for phase in result["phases"]
            for command in phase["commands"]
        )

        self.assertIn("git tag -s", commands)
        self.assertIn("--verify-tag", commands)
        self.assertIn("--draft", commands)
        self.assertIn("--prerelease", commands)
        self.assertIn("--draft=false", commands)
        publish_phase = result["phases"][-1]
        self.assertTrue(
            publish_phase["requires_separate_approval"]
        )

    def test_release_sha_must_be_full_lowercase_hash(self) -> None:
        with self.assertRaises(SystemExit):
            release.release_plan(
                release.parse_version("v0.1.0"),
                "stable",
                "abc123",
            )


class CliTests(unittest.TestCase):
    def test_invalid_command_exits_without_traceback(self) -> None:
        stderr = io.StringIO()
        with redirect_stderr(stderr), self.assertRaises(SystemExit):
            release.main(["unknown"])
        self.assertNotIn("Traceback", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
