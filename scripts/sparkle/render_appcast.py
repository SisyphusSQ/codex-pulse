#!/usr/bin/env python3
"""Render one signed Codex Pulse release into a shared Sparkle appcast."""

from __future__ import annotations

import argparse
import base64
import os
import re
import tempfile
from dataclasses import dataclass
from email.utils import parsedate_to_datetime
from pathlib import Path
from urllib.parse import urlparse
from xml.etree import ElementTree


SPARKLE_NAMESPACE = "http://www.andymatuschak.org/xml-namespaces/sparkle"
SEMVER_PATTERN = re.compile(
    r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)"
    r"(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$"
)
MINIMUM_SYSTEM_PATTERN = re.compile(r"^[1-9]\d*(?:\.\d+){1,2}$")
MAXIMUM_ITEMS = 20

ElementTree.register_namespace("sparkle", SPARKLE_NAMESPACE)


def sparkle(name: str) -> str:
    return f"{{{SPARKLE_NAMESPACE}}}{name}"


@dataclass(frozen=True)
class AppcastEntry:
    version: str
    build_number: int
    channel: str
    archive_url: str
    archive_length: int
    ed_signature: str
    publication_date: str
    minimum_system_version: str

    def validate(self) -> None:
        match = SEMVER_PATTERN.fullmatch(self.version)
        if match is None:
            raise ValueError("version must be SemVer without a leading v")
        prerelease = match.group(4)
        if self.channel == "stable":
            if prerelease is not None:
                raise ValueError("stable channel requires a stable SemVer")
        elif self.channel == "prerelease":
            if prerelease is None:
                raise ValueError("prerelease channel requires a prerelease SemVer")
        else:
            raise ValueError("channel must be stable or prerelease")
        if self.build_number <= 0:
            raise ValueError("build number must be positive")
        if self.archive_length <= 0:
            raise ValueError("archive length must be positive")
        if not MINIMUM_SYSTEM_PATTERN.fullmatch(self.minimum_system_version):
            raise ValueError("minimum system version is invalid")

        parsed_url = urlparse(self.archive_url)
        expected_name = (
            f"Codex-Pulse-v{self.version}-macos-arm64.zip"
        )
        expected_path = (
            f"/SisyphusSQ/codex-pulse/releases/download/v{self.version}/"
            f"{expected_name}"
        )
        if (
            parsed_url.scheme != "https"
            or parsed_url.hostname != "github.com"
            or parsed_url.username is not None
            or parsed_url.password is not None
            or parsed_url.port is not None
            or parsed_url.path != expected_path
            or parsed_url.query
            or parsed_url.fragment
        ):
            raise ValueError("archive URL is not the exact Codex Pulse GitHub release asset")

        try:
            signature = base64.b64decode(
                self.ed_signature,
                validate=True,
            )
        except ValueError as error:
            raise ValueError("Ed25519 signature is not valid base64") from error
        if len(signature) != 64:
            raise ValueError("Ed25519 signature must decode to 64 bytes")
        publication_date = parsedate_to_datetime(self.publication_date)
        if publication_date.tzinfo is None:
            raise ValueError("publication date must include a timezone")


def new_document() -> tuple[ElementTree.Element, ElementTree.Element]:
    root = ElementTree.Element("rss", {"version": "2.0"})
    channel = ElementTree.SubElement(root, "channel")
    ElementTree.SubElement(channel, "title").text = "Codex Pulse 更新"
    ElementTree.SubElement(channel, "link").text = (
        "https://github.com/SisyphusSQ/codex-pulse"
    )
    ElementTree.SubElement(channel, "description").text = (
        "Codex Pulse stable 与 prerelease 应用内更新"
    )
    ElementTree.SubElement(channel, "language").text = "zh-cn"
    return root, channel


def parse_existing(
    existing_xml: bytes | None,
) -> tuple[ElementTree.Element, ElementTree.Element]:
    if existing_xml is None:
        return new_document()
    try:
        root = ElementTree.fromstring(existing_xml)
    except ElementTree.ParseError as error:
        raise ValueError("existing appcast is not valid XML") from error
    if root.tag != "rss" or root.get("version") != "2.0":
        raise ValueError("existing appcast must be an RSS 2.0 document")
    channel = root.find("channel")
    if channel is None:
        raise ValueError("existing appcast has no channel")
    return root, channel


def item_build_number(item: ElementTree.Element) -> int:
    raw_value = item.findtext(sparkle("version"))
    if raw_value is None or not raw_value.isdigit():
        raise ValueError("existing appcast item has an invalid build number")
    return int(raw_value)


def make_item(entry: AppcastEntry) -> ElementTree.Element:
    item = ElementTree.Element("item")
    ElementTree.SubElement(item, "title").text = (
        f"Codex Pulse v{entry.version}"
    )
    ElementTree.SubElement(item, "pubDate").text = entry.publication_date
    ElementTree.SubElement(item, sparkle("version")).text = str(
        entry.build_number
    )
    ElementTree.SubElement(
        item,
        sparkle("shortVersionString"),
    ).text = entry.version
    ElementTree.SubElement(
        item,
        sparkle("minimumSystemVersion"),
    ).text = entry.minimum_system_version
    if entry.channel == "prerelease":
        ElementTree.SubElement(
            item,
            sparkle("channel"),
        ).text = "prerelease"
    ElementTree.SubElement(
        item,
        "enclosure",
        {
            "url": entry.archive_url,
            "length": str(entry.archive_length),
            "type": "application/octet-stream",
            sparkle("edSignature"): entry.ed_signature,
        },
    )
    return item


def render_appcast(
    existing_xml: bytes | None,
    entry: AppcastEntry,
) -> bytes:
    entry.validate()
    root, channel = parse_existing(existing_xml)
    existing_items = channel.findall("item")
    existing_build_numbers = [
        item_build_number(item) for item in existing_items
    ]
    if existing_build_numbers and entry.build_number <= max(
        existing_build_numbers
    ):
        raise ValueError(
            "build number must increase across stable and prerelease channels"
        )

    new_item = make_item(entry)
    first_item_index = next(
        (
            index
            for index, child in enumerate(list(channel))
            if child.tag == "item"
        ),
        len(channel),
    )
    channel.insert(first_item_index, new_item)
    for stale_item in channel.findall("item")[MAXIMUM_ITEMS:]:
        channel.remove(stale_item)

    ElementTree.indent(root, space="  ")
    return ElementTree.tostring(
        root,
        encoding="utf-8",
        xml_declaration=True,
    ) + b"\n"


def write_atomically(path: Path, payload: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(
        prefix=f".{path.name}.",
        dir=path.parent,
    )
    try:
        with os.fdopen(descriptor, "wb") as output:
            output.write(payload)
        os.replace(temporary_name, path)
    finally:
        if os.path.exists(temporary_name):
            os.unlink(temporary_name)


def parse_arguments() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--existing", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--build-number", required=True, type=int)
    parser.add_argument(
        "--channel",
        choices=("stable", "prerelease"),
        required=True,
    )
    parser.add_argument("--archive-url", required=True)
    parser.add_argument("--archive-length", required=True, type=int)
    parser.add_argument("--ed-signature", required=True)
    parser.add_argument("--publication-date", required=True)
    parser.add_argument("--minimum-system-version", default="15.0")
    return parser.parse_args()


def main() -> None:
    arguments = parse_arguments()
    existing_xml = None
    if arguments.existing is not None:
        if not arguments.existing.is_file():
            raise SystemExit(
                f"existing appcast is unavailable: {arguments.existing}"
            )
        existing_xml = arguments.existing.read_bytes()
    entry = AppcastEntry(
        version=arguments.version,
        build_number=arguments.build_number,
        channel=arguments.channel,
        archive_url=arguments.archive_url,
        archive_length=arguments.archive_length,
        ed_signature=arguments.ed_signature,
        publication_date=arguments.publication_date,
        minimum_system_version=arguments.minimum_system_version,
    )
    try:
        rendered = render_appcast(existing_xml, entry)
    except ValueError as error:
        raise SystemExit(f"appcast rendering failed: {error}") from None
    write_atomically(arguments.output, rendered)


if __name__ == "__main__":
    main()
