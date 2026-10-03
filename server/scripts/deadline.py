#!/usr/bin/env python3
"""Bound an operator command and clean up its process group on timeout."""

from __future__ import annotations

import os
import signal
import subprocess
import sys


def main() -> int:
    if len(sys.argv) < 3:
        raise SystemExit("usage: deadline.py SECONDS COMMAND [ARG ...]")
    seconds = float(sys.argv[1])
    if not 0 < seconds <= 600:
        raise SystemExit("deadline must be between zero and 600 seconds")
    process = subprocess.Popen(sys.argv[2:], start_new_session=True)
    try:
        return process.wait(timeout=seconds)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=2)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
        return 124


if __name__ == "__main__":
    sys.exit(main())
