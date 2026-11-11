"""Bounded, process-filtered TCP capture for the user's MT4 iOS-on-Mac app."""

import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import pwd
import re
import resource
import signal
import stat
import subprocess
import tempfile
import time


MAX_BYTES = 16 * 1024 * 1024


def target(pid):
    result = subprocess.run(
        ["/bin/ps", "-p", str(pid), "-o", "uid=,comm="],
        capture_output=True, text=True, check=True,
    )
    uid, executable = result.stdout.strip().split(None, 1)
    if not executable.endswith("/MetaTrader4Terminal.app/MetaTrader4Terminal"):
        raise ValueError("PID does not identify the MT4 mobile application")
    owner = pwd.getpwuid(int(uid))
    if owner.pw_uid == 0:
        raise ValueError("Refusing to capture a root-owned target")
    return owner


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--pid", type=int, required=True)
    parser.add_argument("--seconds", type=int, default=120)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    if args.pid < 1 or not 1 <= args.seconds <= 180:
        parser.error("PID must be positive; duration must be 1 to 180 seconds")
    owner = target(args.pid)
    caller_uid = int(os.environ.get("SUDO_UID", os.getuid()))
    if owner.pw_uid != caller_uid:
        raise ValueError("The target must belong to the invoking user")
    metadata_filter = "(pid = {0} or epid = {0})".format(args.pid)
    command = [
        "/usr/sbin/tcpdump", "-i", "pktap,all", "-Q", metadata_filter,
        "-n", "-s", "0", "-U", "-c", "10000", "-w", "-", "tcp",
    ]
    if args.dry_run:
        print(json.dumps({
            "pid": args.pid, "seconds": args.seconds,
            "packet_filter": "tcp", "metadata_filter": metadata_filter,
            "max_file_bytes": MAX_BYTES, "command": command,
        }))
        return 0
    if os.geteuid() != 0:
        raise PermissionError("Packet capture requires running this script with sudo")
    folder = Path(__file__).resolve().parents[2] / ".local" / "mt4-mobile-analysis"
    if not folder.exists():
        folder.mkdir(mode=0o700)
        os.chown(folder, owner.pw_uid, owner.pw_gid)
    attributes = folder.lstat()
    if not stat.S_ISDIR(attributes.st_mode) or attributes.st_uid != owner.pw_uid:
        raise ValueError("Capture folder must be a directory owned by the invoking user")
    if stat.S_IMODE(attributes.st_mode) != 0o700:
        raise ValueError("Capture folder must have mode 0700")
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    fd, filename = tempfile.mkstemp(prefix="mt4-mac-" + stamp + "-", suffix=".pcapng", dir=folder)
    os.fchown(fd, owner.pw_uid, owner.pw_gid)

    def cap_file_size():
        resource.setrlimit(resource.RLIMIT_FSIZE, (MAX_BYTES, MAX_BYTES))

    process = None
    interrupted = False
    started = time.monotonic()
    try:
        with os.fdopen(fd, "wb") as output:
            process = subprocess.Popen(
                command, stdin=subprocess.DEVNULL, stdout=output,
                stderr=subprocess.PIPE, text=True, preexec_fn=cap_file_size,
            )
            print(json.dumps({
                "event": "capture_started", "pid": args.pid,
                "seconds": args.seconds, "private_file": filename,
            }), flush=True)
            try:
                _, error = process.communicate(timeout=args.seconds)
            except (subprocess.TimeoutExpired, KeyboardInterrupt) as stop:
                interrupted = isinstance(stop, KeyboardInterrupt)
                if process.poll() is None:
                    process.send_signal(signal.SIGINT)
                try:
                    _, error = process.communicate(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    _, error = process.communicate(timeout=5)
        match = re.search(r"(\d+) packets captured", error)
        count = int(match.group(1)) if match else 0
        if not count:
            Path(filename).unlink(missing_ok=True)
        print(json.dumps({
            "event": "capture_stopped", "packets": count,
            "elapsed_seconds": round(time.monotonic() - started, 2),
            "reason": "user_stop" if interrupted else "bounded_capture_finished",
            "private_file": filename if count else None,
            "tcpdump_exit": process.returncode,
            "error": error.strip() if not count else None,
        }), flush=True)
        return 0 if count else 2
    finally:
        if process is not None and process.poll() is None:
            process.kill()
            process.wait(timeout=5)


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, subprocess.SubprocessError) as exc:
        print(json.dumps({"event": "capture_error", "error": str(exc)}), flush=True)
        raise SystemExit(1)
