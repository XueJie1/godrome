#!/usr/bin/env python3
"""Prompt without echo; keep live-test credentials only in process memory."""
import argparse
import getpass
import os
from pathlib import Path
import subprocess
import sys

parser = argparse.ArgumentParser()
parser.add_argument("server")
parser.add_argument("username")
parser.add_argument("--native", action="store_true", help="play in an actual native window at no more than 50 percent")
args = parser.parse_args()
if not sys.stdin.isatty():
    parser.error("use a terminal for password input without echo")
env = os.environ.copy()
env.update(GODROME_TEST_SERVER=args.server, GODROME_TEST_USER=args.username,
           GODROME_TEST_PASSWORD=getpass.getpass("Test password (not saved): "))
command = ["./build/godrome-acceptance", "--scenario", "live", "--volume", "50", "--audio-output", "", "--quit-after", "5m", "--report", "docs/evidence/live-desktop.json"] if args.native else ["go", "test", "-race", "-count=1", "-run", "^TestRealNavidrome$", "-v", "."]
subprocess.run(command,
               cwd=Path(__file__).resolve().parents[1], env=env, check=True)
