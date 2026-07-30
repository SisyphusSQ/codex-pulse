#!/usr/bin/env python3
"""Verify a Sparkle archive signature against the public key inside the app."""

from __future__ import annotations

import argparse
import base64
import binascii
import plistlib
import stat
import subprocess
import tempfile
import zipfile
from pathlib import Path


INFO_PLIST_PATH = "Codex Pulse.app/Contents/Info.plist"
ED25519_SUBJECT_PUBLIC_KEY_INFO_PREFIX = bytes.fromhex(
    "302a300506032b6570032100"
)


def decode_base64(value: str, *, expected_length: int, label: str) -> bytes:
    try:
        decoded = base64.b64decode(value, validate=True)
    except (binascii.Error, ValueError) as error:
        raise ValueError(f"{label} is not valid base64") from error
    if len(decoded) != expected_length:
        raise ValueError(
            f"{label} must decode to {expected_length} bytes"
        )
    return decoded


def embedded_public_key(archive_path: Path) -> bytes:
    if not archive_path.is_file() or archive_path.is_symlink():
        raise ValueError("archive must be a regular non-symlink file")
    try:
        with zipfile.ZipFile(archive_path) as archive:
            matches = [
                info
                for info in archive.infolist()
                if info.filename == INFO_PLIST_PATH
            ]
            if len(matches) != 1:
                raise ValueError(
                    "archive must contain exactly one expected Info.plist"
                )
            info = matches[0]
            if info.is_dir() or stat.S_ISLNK(info.external_attr >> 16):
                raise ValueError("archive Info.plist must be a regular file")
            payload = archive.read(info)
    except zipfile.BadZipFile as error:
        raise ValueError("archive is not a valid ZIP file") from error

    try:
        properties = plistlib.loads(payload)
    except plistlib.InvalidFileException as error:
        raise ValueError("archive Info.plist is invalid") from error
    public_key = properties.get("SUPublicEDKey")
    if not isinstance(public_key, str):
        raise ValueError("archive Info.plist has no SUPublicEDKey")
    return decode_base64(
        public_key,
        expected_length=32,
        label="archive SUPublicEDKey",
    )


def verify_archive_signature(
    archive_path: Path,
    signature_value: str,
) -> None:
    public_key = embedded_public_key(archive_path)
    signature = decode_base64(
        signature_value,
        expected_length=64,
        label="Ed25519 signature",
    )
    with tempfile.TemporaryDirectory(
        prefix="codex-pulse-sparkle-verify-"
    ) as directory:
        temporary_directory = Path(directory)
        public_key_path = temporary_directory / "public.der"
        signature_path = temporary_directory / "signature.bin"
        public_key_path.write_bytes(
            ED25519_SUBJECT_PUBLIC_KEY_INFO_PREFIX + public_key
        )
        signature_path.write_bytes(signature)
        result = subprocess.run(
            [
                "openssl",
                "pkeyutl",
                "-verify",
                "-pubin",
                "-inkey",
                str(public_key_path),
                "-keyform",
                "DER",
                "-rawin",
                "-in",
                str(archive_path),
                "-sigfile",
                str(signature_path),
            ],
            check=False,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
    if result.returncode != 0:
        raise ValueError(
            "signature does not match the archive SUPublicEDKey"
        )


def parse_arguments() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--archive", type=Path, required=True)
    parser.add_argument("--ed-signature", required=True)
    return parser.parse_args()


def main() -> None:
    arguments = parse_arguments()
    try:
        verify_archive_signature(
            arguments.archive,
            arguments.ed_signature,
        )
    except ValueError as error:
        raise SystemExit(
            f"archive signature verification failed: {error}"
        ) from None


if __name__ == "__main__":
    main()
