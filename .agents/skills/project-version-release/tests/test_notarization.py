"""Submission recovery tests without contacting Apple or accessing a Keychain."""

from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path


REPOSITORY = Path(__file__).resolve().parents[4]
NOTARIZE = REPOSITORY / "scripts/macos/notarize-release-archive.sh"
BUILD = REPOSITORY / "scripts/macos/build-release-app.sh"


class NotarizationTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.archive = self.root / "candidate.zip"
        self.archive.write_bytes(b"candidate archive")
        self.record = self.root / "notary-app"
        self.calls = self.root / "calls.jsonl"
        xcrun = self.root / "xcrun"
        xcrun.write_text(
            "#!/usr/bin/env python3\n"
            "import json, os, pathlib, sys\n"
            "action = sys.argv[2]\n"
            "with open(os.environ['NOTARY_TEST_CALLS'], 'a') as calls:\n"
            "    calls.write(json.dumps(sys.argv[1:]) + '\\n')\n"
            "scenario = os.environ.get('NOTARY_TEST_SCENARIO', '')\n"
            "if action == 'submit':\n"
            "    if scenario == 'unknown':\n"
            "        raise SystemExit(1)\n"
            "    print(json.dumps({'id': '12345678-1234-1234-1234-123456789abc'}))\n"
            "elif action == 'wait':\n"
            "    if scenario == 'timeout':\n"
            "        raise SystemExit(1)\n"
            "    print(json.dumps({'status': 'Invalid' if scenario == 'invalid' else 'Accepted'}))\n"
            "elif action == 'log':\n"
            "    pathlib.Path(sys.argv[-1]).write_text('{}')\n",
            encoding="utf-8",
        )
        xcrun.chmod(0o700)
        self.environment = dict(os.environ)
        self.environment.update(
            PATH=f"{self.root}:{os.environ['PATH']}",
            NOTARY_TEST_CALLS=str(self.calls),
        )

    def run_notarization(self, scenario: str = "") -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["bash", str(NOTARIZE), str(self.archive), "test-profile", str(self.record)],
            env={**self.environment, "NOTARY_TEST_SCENARIO": scenario},
            capture_output=True,
            text=True,
            timeout=10,
        )

    def actions(self) -> list[str]:
        return [
            json.loads(line)[1]
            for line in self.calls.read_text(encoding="utf-8").splitlines()
        ]

    def test_accepted_submission_is_reused(self) -> None:
        self.assertEqual(self.run_notarization().returncode, 0)
        self.assertEqual(self.run_notarization().returncode, 0)
        self.assertEqual(self.actions(), ["submit", "wait", "wait"])
        self.assertEqual(self.record.with_suffix(".submit.json").stat().st_mode & 0o777, 0o600)

    def test_changed_archive_cannot_reuse_submission(self) -> None:
        self.assertEqual(self.run_notarization().returncode, 0)
        self.archive.write_bytes(b"different archive")
        result = self.run_notarization()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("differs from retained submission", result.stderr)
        self.assertEqual(self.actions(), ["submit", "wait"])

    def test_invalid_submission_retains_diagnostic_log(self) -> None:
        result = self.run_notarization("invalid")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("status is Invalid", result.stderr)
        self.assertEqual(self.actions(), ["submit", "wait", "log"])
        self.assertTrue(self.record.with_suffix(".log.json").is_file())

    def test_unknown_submit_result_is_never_resubmitted(self) -> None:
        self.assertNotEqual(self.run_notarization("unknown").returncode, 0)
        result = self.run_notarization()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("result is unknown", result.stderr)
        self.assertEqual(self.actions(), ["submit"])

    def test_wait_timeout_resumes_original_submission(self) -> None:
        self.assertNotEqual(self.run_notarization("timeout").returncode, 0)
        self.assertEqual(self.run_notarization().returncode, 0)
        self.assertEqual(self.actions(), ["submit", "wait", "wait"])

    def test_build_cannot_silently_fall_back_to_unsigned(self) -> None:
        result = subprocess.run(
            ["bash", str(BUILD)], capture_output=True, text=True, timeout=10,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires --signing-identity and --notary-profile", result.stderr)


if __name__ == "__main__":
    unittest.main()
