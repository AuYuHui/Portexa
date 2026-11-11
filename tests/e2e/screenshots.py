"""Capture public README images from a new, isolated synthetic server only."""

from contextlib import contextmanager
from pathlib import Path
import socket
import subprocess
import tempfile
import time
from urllib.error import URLError
from urllib.request import urlopen

from playwright.sync_api import expect, sync_playwright

from dashboard import PASSWORD, no_page_overflow


ROOT = Path(__file__).resolve().parents[2]
ARTIFACTS = ROOT / "docs" / "screenshots"
SERVER = ROOT / ".local" / "bin" / "e2e-server"
WEB = ROOT / ".local" / "web"


@contextmanager
def synthetic_server():
    if not SERVER.is_file() or not (WEB / "index.html").is_file():
        raise RuntimeError("Build the web app and tests/e2e/server first; see docs/screenshots.md")
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    origin = f"http://127.0.0.1:{port}"
    with tempfile.TemporaryFile() as log:
        process = subprocess.Popen(
            [str(SERVER), "-automatic-time", "-listen", f"127.0.0.1:{port}", "-web", str(WEB)],
            cwd=ROOT,
            stdout=log,
            stderr=subprocess.STDOUT,
        )
        try:
            deadline = time.monotonic() + 20
            while True:
                if process.poll() is not None:
                    raise RuntimeError("Synthetic screenshot server exited before becoming ready")
                try:
                    with urlopen(origin + "/healthz", timeout=0.5) as response:
                        if response.status == 200:
                            break
                except (URLError, TimeoutError):
                    pass
                if time.monotonic() >= deadline:
                    raise RuntimeError("Synthetic screenshot server did not become ready")
                time.sleep(0.1)
            yield origin
        finally:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


def capture(page, name):
    page.wait_for_load_state("networkidle")
    page.evaluate("document.fonts.ready")
    page.mouse.move(0, 0)
    no_page_overflow(page)
    page.screenshot(path=str(ARTIFACTS / name), full_page=True, animations="disabled")


def capture_locale(browser, origin, locale):
    english = locale == "en"
    context = browser.new_context(
        locale="en-GB" if english else "zh-CN",
        viewport={"width": 1440, "height": 1000},
        device_scale_factor=1,
    )
    try:
        page = context.new_page()
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.goto(origin)
        page.wait_for_load_state("networkidle")
        expect(page.locator("html")).to_have_attribute("lang", locale)
        page.get_by_label("System password" if english else "系统密码", exact=True).fill(PASSWORD)
        page.get_by_role("button", name="Sign in securely" if english else "安全登录", exact=True).click()
        overview = "Account overview" if english else "账户总览"
        expect(page.get_by_role("heading", name=overview)).to_be_visible()
        expect(page.locator(".accounts-table tbody tr")).to_have_count(5)

        row = page.locator(".accounts-table tbody tr").filter(has_text="100102")
        row.get_by_role("link").first.click()
        daily = page.get_by_role(
            "region", name="Today's profit and balance change" if english else "当日利润与结余", exact=True
        )
        expect(daily.locator(".daily-metrics .metric-value").nth(0)).to_have_text("9.50", timeout=20_000)
        deposits = page.locator('[data-fund-total="deposits"]')
        expect(deposits).to_have_text("1,100.00", timeout=20_000)
        expect(page.locator('[data-fund-total="withdrawals"]')).to_have_text("20.00")
        expect(page.locator(".funds-table tbody tr")).to_have_count(7)
        capture(page, f"account-{locale}.png")

        page.get_by_role("link", name=overview, exact=True).click()
        expect(page.get_by_role("heading", name=overview)).to_be_visible()
        expect(page.locator(".hero-value")).to_contain_text("27,049.11")
        expect(row).to_contain_text("1,100.00 / 20.00")
        expect(page.get_by_text("Paused · Stale" if english else "已暂停 · 过期", exact=True)).to_be_visible()
        capture(page, f"overview-{locale}.png")
        assert not errors, errors
    finally:
        context.close()


def main():
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    with synthetic_server() as origin, sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        try:
            for locale in ("en", "zh-CN"):
                capture_locale(browser, origin, locale)
        finally:
            browser.close()
    print("PASS: four desktop screenshots captured from synthetic data in docs/screenshots/")


if __name__ == "__main__":
    main()
