"""Behavior tests for the Sparkle appcast renderer."""

from __future__ import annotations

import base64
import importlib.util
import plistlib
import subprocess
import sys
import tempfile
import unittest
import zipfile
from pathlib import Path
from types import ModuleType
from xml.etree import ElementTree


REPOSITORY_ROOT = Path(__file__).resolve().parents[4]
SCRIPT_PATH = REPOSITORY_ROOT / "scripts" / "sparkle" / "render_appcast.py"
SPARKLE_NAMESPACE = "http://www.andymatuschak.org/xml-namespaces/sparkle"


def load_renderer() -> ModuleType:
    spec = importlib.util.spec_from_file_location("render_appcast", SCRIPT_PATH)
    if spec is None or spec.loader is None:
        raise RuntimeError("unable to load render_appcast")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


appcast = load_renderer()


def load_signature_verifier() -> ModuleType:
    script_path = (
        REPOSITORY_ROOT
        / "scripts"
        / "sparkle"
        / "verify_archive_signature.py"
    )
    spec = importlib.util.spec_from_file_location(
        "verify_archive_signature",
        script_path,
    )
    if spec is None or spec.loader is None:
        raise RuntimeError("unable to load verify_archive_signature")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def entry(
    *,
    version: str,
    build_number: int,
    channel: str,
) -> object:
    return appcast.AppcastEntry(
        version=version,
        build_number=build_number,
        channel=channel,
        archive_url=(
            "https://github.com/SisyphusSQ/codex-pulse/releases/download/"
            f"v{version}/Codex-Pulse-v{version}-macos-arm64.zip"
        ),
        archive_length=1024 + build_number,
        ed_signature=base64.b64encode(bytes([build_number]) * 64).decode(),
        publication_date="Thu, 30 Jul 2026 10:00:00 +0800",
        minimum_system_version="15.0",
    )


def items(xml_payload: bytes) -> list[ElementTree.Element]:
    root = ElementTree.fromstring(xml_payload)
    return root.findall("./channel/item")


class AppcastRenderingTests(unittest.TestCase):
    def test_stable_and_prerelease_share_one_monotonic_feed(self) -> None:
        stable = appcast.render_appcast(
            None,
            entry(version="0.1.0", build_number=10, channel="stable"),
        )
        combined = appcast.render_appcast(
            stable,
            entry(
                version="0.2.0-beta.1",
                build_number=11,
                channel="prerelease",
            ),
        )
        rendered_items = items(combined)

        self.assertEqual(
            [
                item.findtext(f"{{{SPARKLE_NAMESPACE}}}version")
                for item in rendered_items
            ],
            ["11", "10"],
        )
        self.assertEqual(
            rendered_items[0].findtext(
                f"{{{SPARKLE_NAMESPACE}}}channel"
            ),
            "prerelease",
        )
        self.assertIsNone(
            rendered_items[1].find(f"{{{SPARKLE_NAMESPACE}}}channel")
        )

    def test_new_stable_preserves_prerelease_history(self) -> None:
        existing = appcast.render_appcast(
            None,
            entry(
                version="0.2.0-beta.1",
                build_number=11,
                channel="prerelease",
            ),
        )
        combined = appcast.render_appcast(
            existing,
            entry(version="0.2.0", build_number=12, channel="stable"),
        )

        self.assertEqual(
            [
                item.findtext(
                    f"{{{SPARKLE_NAMESPACE}}}shortVersionString"
                )
                for item in items(combined)
            ],
            ["0.2.0", "0.2.0-beta.1"],
        )

    def test_build_number_must_increase_across_channels(self) -> None:
        existing = appcast.render_appcast(
            None,
            entry(version="0.2.0", build_number=12, channel="stable"),
        )

        with self.assertRaises(ValueError):
            appcast.render_appcast(
                existing,
                entry(
                    version="0.3.0-beta.1",
                    build_number=12,
                    channel="prerelease",
                ),
            )

    def test_channel_must_match_semver_surface(self) -> None:
        with self.assertRaises(ValueError):
            entry(
                version="0.2.0-beta.1",
                build_number=12,
                channel="stable",
            ).validate()

        with self.assertRaises(ValueError):
            entry(
                version="0.2.0",
                build_number=12,
                channel="prerelease",
            ).validate()

    def test_archive_url_must_be_the_exact_release_asset(self) -> None:
        valid_entry = entry(
            version="0.2.0-beta.1",
            build_number=12,
            channel="prerelease",
        )
        for invalid_url in (
            (
                "https://github.com/SisyphusSQ/codex-pulse/releases/download/"
                "v0.2.0-beta.1/extra/"
                "Codex-Pulse-v0.2.0-beta.1-macos-arm64.zip"
            ),
            (
                "https://user@github.com/SisyphusSQ/codex-pulse/releases/"
                "download/v0.2.0-beta.1/"
                "Codex-Pulse-v0.2.0-beta.1-macos-arm64.zip"
            ),
        ):
            invalid_entry = appcast.AppcastEntry(
                version=valid_entry.version,
                build_number=valid_entry.build_number,
                channel=valid_entry.channel,
                archive_url=invalid_url,
                archive_length=valid_entry.archive_length,
                ed_signature=valid_entry.ed_signature,
                publication_date=valid_entry.publication_date,
                minimum_system_version=valid_entry.minimum_system_version,
            )
            with self.assertRaises(ValueError):
                invalid_entry.validate()


class ReleaseScriptContractTests(unittest.TestCase):
    def test_install_dmg_contains_signed_app_and_applications_link(
        self,
    ) -> None:
        script = (
            REPOSITORY_ROOT
            / "scripts"
            / "macos"
            / "create-install-dmg.sh"
        )
        if not script.is_file():
            self.fail("install DMG builder is missing")

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            app = root / "Codex Pulse.app"
            executable = app / "Contents" / "MacOS" / "Codex Pulse"
            executable.parent.mkdir(parents=True)
            executable.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
            executable.chmod(0o755)
            with (app / "Contents" / "Info.plist").open("wb") as plist_file:
                plistlib.dump(
                    {
                        "CFBundleExecutable": "Codex Pulse",
                        "CFBundleIdentifier": "com.sisyphussq.codexpulse.test",
                        "CFBundleName": "Codex Pulse",
                        "CFBundlePackageType": "APPL",
                        "CFBundleShortVersionString": "0.0.0",
                        "CFBundleVersion": "1",
                    },
                    plist_file,
                )
            subprocess.run(
                [
                    "codesign",
                    "--force",
                    "--sign",
                    "-",
                    "--timestamp=none",
                    str(app),
                ],
                check=True,
                capture_output=True,
            )

            dmg = root / "Codex-Pulse-test-macos-arm64.dmg"
            result = subprocess.run(
                [
                    str(script),
                    "--app",
                    str(app),
                    "--output",
                    str(dmg),
                    "--volume-name",
                    "Codex Pulse Test",
                ],
                check=False,
                capture_output=True,
                text=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertGreater(dmg.stat().st_size, 0)
            subprocess.run(
                ["hdiutil", "verify", str(dmg)],
                check=True,
                capture_output=True,
            )

            mountpoint = root / "mounted"
            mountpoint.mkdir()
            subprocess.run(
                [
                    "hdiutil",
                    "attach",
                    "-readonly",
                    "-nobrowse",
                    "-mountpoint",
                    str(mountpoint),
                    str(dmg),
                ],
                check=True,
                capture_output=True,
            )
            try:
                self.assertEqual(
                    {path.name for path in mountpoint.iterdir()},
                    {"Codex Pulse.app", "Applications"},
                )
                self.assertEqual(
                    (mountpoint / "Applications").readlink(),
                    Path("/Applications"),
                )
                subprocess.run(
                    [
                        "codesign",
                        "--verify",
                        "--deep",
                        "--strict",
                        str(mountpoint / "Codex Pulse.app"),
                    ],
                    check=True,
                    capture_output=True,
                )
            finally:
                subprocess.run(
                    ["hdiutil", "detach", str(mountpoint)],
                    check=True,
                    capture_output=True,
                )

    def test_archive_signature_verifier_reads_the_embedded_public_key(self) -> None:
        verifier = load_signature_verifier()
        expected_key = bytes(range(32))
        with tempfile.TemporaryDirectory() as directory:
            archive_path = Path(directory) / "Codex-Pulse-test.zip"
            with zipfile.ZipFile(archive_path, "w") as archive:
                archive.writestr(
                    "Codex Pulse.app/Contents/Info.plist",
                    plistlib.dumps(
                        {
                            "SUPublicEDKey": base64.b64encode(
                                expected_key
                            ).decode()
                        }
                    ),
                )

            self.assertEqual(
                verifier.embedded_public_key(archive_path),
                expected_key,
            )

    def test_private_key_is_only_read_from_standard_input(self) -> None:
        script = (
            REPOSITORY_ROOT / "scripts" / "sparkle" / "generate_appcast.sh"
        ).read_text(encoding="utf-8")

        self.assertIn("--ed-key-file -", script)
        self.assertNotIn("--private-key", script)
        self.assertNotIn("SPARKLE_PRIVATE_KEY", script)
        self.assertIn("verify_archive_signature.py", script)

    def test_app_bundlers_embed_framework_and_standard_rpath(self) -> None:
        package = (
            REPOSITORY_ROOT / "app" / "macos" / "Package.swift"
        ).read_text(encoding="utf-8")
        release_script = (
            REPOSITORY_ROOT / "scripts" / "macos" / "build-release-app.sh"
        ).read_text(encoding="utf-8")
        development_script = (
            REPOSITORY_ROOT / "scripts" / "macos" / "build-dev-app.sh"
        ).read_text(encoding="utf-8")

        self.assertIn("@executable_path/../Frameworks", package)
        for script in (release_script, development_script):
            self.assertIn("Contents/Frameworks", script)
            self.assertIn("Sparkle.framework", script)

        self.assertIn("SUFeedURL", release_script)
        self.assertIn("SUPublicEDKey", release_script)


if __name__ == "__main__":
    unittest.main()
