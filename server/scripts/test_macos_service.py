"""Regression coverage for launchd's asynchronous bootout completion."""

import importlib.util
import subprocess
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location(
    "macos_service", Path(__file__).with_name("macos-service.py")
)
service = importlib.util.module_from_spec(spec)
spec.loader.exec_module(service)


class StopTests(unittest.TestCase):
    def test_waits_for_process_after_job_disappears(self):
        detail = subprocess.CompletedProcess([], 0, "\tpid = 42\n", "")
        with (
            patch.object(service.subprocess, "run", return_value=detail) as run,
            patch.object(service, "service_loaded", return_value=False),
            patch.object(service, "process_exists", side_effect=[True, False]),
            patch.object(service.time, "sleep") as sleep,
        ):
            service.stop("gui/501/test")
            sleep.assert_called_once_with(0.1)
            self.assertEqual(run.call_args_list[-1].args[0],
                             ["launchctl", "bootout", "gui/501/test"])

    def test_unloaded_service_does_not_bootout(self):
        detail = subprocess.CompletedProcess([], 113, "", "not found")
        with patch.object(service.subprocess, "run", return_value=detail) as run:
            service.stop("gui/501/test")
            self.assertEqual(run.call_count, 1)

    def test_stuck_job_stops_deployment(self):
        detail = subprocess.CompletedProcess([], 0, "pid = 42\n", "")
        with (
            patch.object(service.subprocess, "run", return_value=detail),
            patch.object(service, "service_loaded", return_value=True),
            patch.object(service.time, "monotonic", side_effect=[0, 31]),
        ):
            with self.assertRaisesRegex(RuntimeError, "did not finish stopping"):
                service.stop("gui/501/test")


if __name__ == "__main__":
    unittest.main()
