# /// script
# requires-python = ">=3.11"
# dependencies = ["pymobiledevice3==11.50.3"]
# ///
"""Capture only the installed MT4 app over a paired USB connection."""

import argparse
import asyncio
from datetime import datetime, timezone
import json
import logging
import os
from pathlib import Path


MT4_BUNDLE = "net.metaquotes.MetaTrader4Terminal"
MAX_CAPTURE_BYTES = 16 * 1024 * 1024


def belongs_to_mt4(packet, executable: str) -> bool:
    # pcapd's process fields can truncate executable names. Empty names must
    # never match; delegated traffic is accepted only for this effective app.
    names = {executable, executable[:16], executable[:15]}
    return bool(executable) and (
        packet.comm in names or packet.ecomm in names
    )


async def capture(udid: str, seconds: int) -> int:
    from pymobiledevice3.lockdown import create_using_usbmux
    from pymobiledevice3.services.installation_proxy import InstallationProxyService
    from pymobiledevice3.services.pcapd import PcapdService

    # Select the requested hardware explicitly. Do not pair another phone or
    # fall back to Wi-Fi, a simulator, or an unfiltered all-device capture.
    async with asyncio.timeout(15):
        lockdown = await create_using_usbmux(
            serial=udid, connection_type="USB", autopair=False
        )
    async with lockdown:
        async with asyncio.timeout(15):
            async with InstallationProxyService(lockdown) as apps:
                installed = await apps.get_apps(bundle_identifiers=[MT4_BUNDLE])
            executable = installed.get(MT4_BUNDLE, {}).get("CFBundleExecutable")
            if not executable:
                raise RuntimeError("MT4 app not found on the selected phone")

        async with PcapdService(lockdown) as pcap:
            directory = Path(__file__).resolve().parents[2] / ".local" / "mt4-capture"
            directory.mkdir(parents=True, exist_ok=True, mode=0o700)
            os.chmod(directory, 0o700)
            stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S.%fZ")
            path = directory / f"mt4-{stamp}.pcapng"
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            count = 0
            payload_bytes = 0
            reason = "device_stream_ended"

            async def filtered_packets():
                nonlocal count, payload_bytes, reason
                async for packet in pcap.watch():
                    if not belongs_to_mt4(packet, executable):
                        continue
                    if payload_bytes + len(packet.data) > MAX_CAPTURE_BYTES:
                        reason = "size_limit"
                        break
                    count += 1
                    payload_bytes += len(packet.data)
                    yield packet

            try:
                with os.fdopen(fd, "wb") as out:
                    print(json.dumps({
                        "event": "capture_ready",
                        "seconds": seconds,
                        "process": executable,
                        "path": str(path),
                    }), flush=True)
                    try:
                        async with asyncio.timeout(seconds):
                            await pcap.write_to_pcap(out, filtered_packets())
                    except TimeoutError:
                        reason = "time_limit"
                    except asyncio.CancelledError:
                        reason = "user_stop"
                        raise
            except BaseException:
                # A partial file is sensitive too; leave its original mode
                # intact and never dump packets or exception payloads to logs.
                raise
            finally:
                if count == 0:
                    path.unlink(missing_ok=True)
                print(json.dumps({
                    "event": "capture_stopped",
                    "packets": count,
                    "frame_bytes": payload_bytes,
                    "reason": reason,
                    "path": str(path) if count else None,
                }), flush=True)
            return 0 if count else 2


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--udid", required=True, help="Selected iPhone hardware UDID")
    parser.add_argument("--seconds", type=int, default=120)
    args = parser.parse_args()
    if not 1 <= args.seconds <= 300:
        parser.error("--seconds must be between 1 and 300")
    logging.basicConfig(level=logging.ERROR)
    try:
        return asyncio.run(capture(args.udid, args.seconds))
    except KeyboardInterrupt:
        return 130
    except Exception as exc:
        print(json.dumps({"event": "capture_error", "error_type": type(exc).__name__}), flush=True)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
