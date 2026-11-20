#!/usr/bin/env python3
"""Build and check the MT5 challenge worker on Linux x86_64 without logging in."""

import argparse
import hashlib
from pathlib import Path
import platform
import shutil
import struct
import subprocess
import tempfile


SCRIPT_DIR = Path(__file__).resolve().parent
DEFAULT_OUTPUT = SCRIPT_DIR.parent.parent / ".local/mt5-worker/compute"
IMAGE_SIZE = 8_404_704
IMAGE_SHA256 = "02b9f6103789bc652eb179264e99d8697b2a33d97ed6f06dabef3e83fc28a161"
ELF_AMD64 = b"\x7fELF\x02\x01"


def validate_image(image):
    if not image.is_file():
        raise ValueError("找不到终端镜像，请用 --image 指定终端可执行文件。")
    if image.stat().st_size != IMAGE_SIZE or hashlib.sha256(image.read_bytes()).hexdigest() != IMAGE_SHA256:
        raise ValueError(f"终端镜像版本不匹配，需要 SHA-256 为 {IMAGE_SHA256} 的 ARM64 镜像。")


def check_worker(worker, image):
    if not worker.is_file():
        raise ValueError("找不到计算程序，请先构建，或用 --output 指定已有程序。")
    with worker.open("rb") as executable:
        header = executable.read(20)
    if not header.startswith(ELF_AMD64) or header[18:20] != b"\x3e\x00":
        raise ValueError("计算程序需为 Linux x86_64 ELF 可执行文件。")
    # Forty-eight zero words produce a zero result. The ready word also
    # verifies that the image mapping and Linux sandbox were initialized.
    job = struct.pack("<I", 48) + bytes(48 * 8)
    result = subprocess.run([str(worker), str(image)], input=job, capture_output=True, timeout=5)
    if result.returncode != 0:
        detail = result.stderr.decode(errors="replace").strip()
        raise ValueError(f"计算程序自检失败（退出码 {result.returncode}）。{detail}")
    if result.stdout != struct.pack("<IQ", 1, 0):
        raise ValueError("计算程序的离线自检响应不匹配。")


def compile_worker(image, temporary, objdump, compiler):
    from translate import END, ENTRY, generate

    disassembly = subprocess.check_output([
        objdump, "--disassemble", "--no-show-raw-insn", "--print-imm-hex",
        f"--start-address={ENTRY:#x}", f"--stop-address={END:#x}", str(image),
    ], encoding="utf-8", timeout=60)
    generate(disassembly, temporary)
    worker = temporary / "compute"
    subprocess.run([
        compiler, "-std=gnu11", "-O1", "-static", "-s", "-march=x86-64", "-mtune=generic",
        "-I", str(temporary), str(SCRIPT_DIR / "compute.c"), "-o", str(worker),
    ], check=True, timeout=180)
    return worker


def find_tool(name):
    executable = shutil.which(name)
    if executable is None:
        raise ValueError(f"找不到构建工具 {name}。请安装编译工具，或指定对应的命令路径。")
    return executable


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", type=Path, required=True, help="supported ARM64 terminal executable")
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT, help="path to the Linux x86_64 worker")
    parser.add_argument("--check", action="store_true", help="check an existing worker without building")
    parser.add_argument("--objdump", default="llvm-objdump", help="LLVM disassembler command")
    parser.add_argument("--cc", default="cc", help="C compiler command")
    options = parser.parse_args()

    try:
        if platform.system() != "Linux" or platform.machine() not in ("x86_64", "AMD64"):
            raise ValueError("此工具需在 Linux x86_64 环境中运行。")
        image = options.image.expanduser().resolve()
        output = options.output.expanduser().absolute()
        if image == output.resolve():
            raise ValueError("--output 必须与终端镜像使用不同路径。")
        validate_image(image)
        if options.check:
            check_worker(output, image)
        else:
            objdump = find_tool(options.objdump)
            compiler = find_tool(options.cc)
            output.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            with tempfile.TemporaryDirectory(prefix="mt5-worker-", dir=output.parent) as directory:
                worker = compile_worker(image, Path(directory), objdump, compiler)
                check_worker(worker, image)
                worker.chmod(0o700)
                worker.replace(output)
    except (OSError, ValueError, subprocess.SubprocessError) as err:
        parser.exit(1, f"MT5 worker: {err}\n")

    print(f"MT5 挑战计算程序离线自检通过。{output}")


if __name__ == "__main__":
    main()
