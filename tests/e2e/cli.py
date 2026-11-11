"""Exercise local administration with synthetic credentials and no MT4 accounts."""

import http.cookiejar
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


BINARY = str(Path(".local/bin/portexa").resolve())
ORIGIN = "http://127.0.0.1:18081"
PASSWORD = "synthetic-cli-login-password"


def command(*args, password=None, success=True):
    result = subprocess.run([BINARY, *args], input=password, text=True, capture_output=True, timeout=20)
    assert (result.returncode == 0) == success, result.stderr
    return result


def start(data):
    process = subprocess.Popen([BINARY, "serve", "-data", str(data), "-dev-http", "-listen", "127.0.0.1:18081"], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    for _ in range(100):
        if process.poll() is not None:
            raise AssertionError(process.communicate())
        try:
            if urllib.request.urlopen(ORIGIN + "/healthz", timeout=1).status == 200:
                return process
        except (urllib.error.URLError, TimeoutError):
            time.sleep(.1)
    process.terminate()
    process.wait(timeout=10)
    raise AssertionError("server did not start")


def stop(process):
    process.terminate()
    stdout, stderr = process.communicate(timeout=15)
    assert process.returncode == 0, stderr
    return stdout


def call(opener, path, password=None):
    data = json.dumps({"password": password}).encode() if password is not None else None
    req = urllib.request.Request(ORIGIN + path, data=data, headers={"Origin": ORIGIN, "Content-Type": "application/json"})
    try:
        response = opener.open(req, timeout=10)
        return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)


def main():
    with tempfile.TemporaryDirectory(prefix="portexa-cli-") as root:
        original = Path(root) / "original"
        restored = Path(root) / "restored"
        backup = Path(root) / "backup.sqlite"
        command("init", "-data", str(original), "-password-stdin", password=PASSWORD + "\n")
        command("init", "-data", str(original), "-password-stdin", password=PASSWORD + "\n", success=False)
        cookie_jar = http.cookiejar.CookieJar()
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookie_jar))
        process = start(original)
        try:
            assert call(opener, "/api/auth/login", PASSWORD)[0] == 200
            assert call(opener, "/api/overview")[1]["key_ready"]
            assert call(opener, "/api/accounts")[1] == []
            command("backup", "-data", str(original), "-file", str(backup), success=False)
        finally:
            stop(process)
        command("backup", "-data", str(original), "-file", str(backup))
        command("restore", "-data", str(restored), "-file", str(backup))
        keys = restored / "keys"
        keys.mkdir(mode=0o700)
        for name in ("credentials.key", "credentials.key.usage"):
            shutil.copy2(original / "keys" / name, keys / name)
        process = start(restored)
        try:
            assert call(opener, "/api/accounts")[0] == 401, "restored session was not revoked"
            assert call(opener, "/api/auth/login", PASSWORD)[0] == 200
            assert call(opener, "/api/overview")[1]["key_ready"]
        finally:
            stop(process)
        command("password-reset", "-data", str(restored), "-password-stdin", password="new-synthetic-cli-password\n")
        process = start(restored)
        try:
            assert call(opener, "/api/accounts")[0] == 401
            assert call(opener, "/api/auth/login", PASSWORD)[0] == 401
            assert call(opener, "/api/auth/login", "new-synthetic-cli-password")[0] == 200
        finally:
            stop(process)
        assert len(list((restored / "backups").glob("portexa-day-*.sqlite"))) >= 1
        print("PASS: CLI init/refusal/lock, private key loading, serve, daily backup, verified backup/restore, session revocation, password reset; zero MT4 accounts")


if __name__ == "__main__":
    main()
