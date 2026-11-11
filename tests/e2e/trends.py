"""Asset history acceptance with an isolated synthetic server; no real MT4."""

import argparse
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
from decimal import Decimal
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


@contextmanager
def synthetic_server(server, web, flags=("-automatic-time", "-asset-trends")):
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    origin = f"http://127.0.0.1:{port}"
    with tempfile.TemporaryFile() as log:
        process = subprocess.Popen(
            [str(server), *flags, "-listen", f"127.0.0.1:{port}", "-web", str(web)],
            cwd=ROOT, stdout=log, stderr=subprocess.STDOUT,
        )
        try:
            deadline = time.monotonic() + 40
            while True:
                if process.poll() is not None:
                    log.seek(0)
                    raise RuntimeError("Synthetic server exited: " + log.read().decode()[-3000:])
                try:
                    with urlopen(origin + "/healthz", timeout=0.5) as response:
                        if response.status == 200:
                            break
                except (URLError, TimeoutError):
                    pass
                if time.monotonic() >= deadline:
                    raise RuntimeError("Synthetic server startup timed out")
                time.sleep(0.1)
            yield origin
        finally:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


def check_locale(browser, origin, locale, artifacts):
    english = locale == "en"
    context = browser.new_context(
        viewport={"width": 1440, "height": 1100}, locale="en-GB" if english else "zh-CN",
        timezone_id="America/New_York",
    )
    page = context.new_page()
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    page.goto(origin)
    page.wait_for_load_state("networkidle")
    page.get_by_label("System password" if english else "系统密码", exact=True).fill(PASSWORD)
    with page.expect_response("**/api/portfolio/equity-history?*") as response:
        page.get_by_role("button", name="Sign in securely" if english else "安全登录", exact=True).click()
    series = response.value.json()
    assert response.value.status == 200 and series["valid_points"] > 100
    assert series["gap_points"] > 0
    assert series["pending_accounts"] == 1 and series["excluded_accounts"] == 1
    assert len(series["points"]) <= 2400
    portfolio = page.get_by_role("region", name="Asset trend" if english else "资产走势", exact=True)
    expect(portfolio.locator("canvas")).to_be_visible()
    expect(portfolio).to_contain_text("UTC+8")
    quality = ("Latest historical point is stale" if english else "最新历史点已过期") if series["stale"] else ("Partial coverage" if english else "部分覆盖")
    expect(portfolio).to_contain_text(quality)
    no_page_overflow(page)
    portfolio.screenshot(path=str(artifacts / f"overview-{locale}-desktop.png"))

    for name, days in (("24H", 1), ("30D", 30), ("90D", 90)):
        with page.expect_response("**/api/portfolio/equity-history?*") as response:
            portfolio.get_by_role("button", name=name, exact=True).click()
        body = response.value.json()
        assert body["to_ms"] - body["from_ms"] == days * 86_400_000
        expect(portfolio.get_by_role("button", name=name, exact=True)).to_have_attribute("aria-pressed", "true")
        expect(portfolio.locator("canvas")).to_be_visible()
    # Returning to a fresh cached range correctly avoids another HTTP request.
    portfolio.get_by_role("button", name="7D", exact=True).click()
    expect(portfolio.get_by_role("button", name="7D", exact=True)).to_have_attribute("aria-pressed", "true")
    expect(portfolio.locator("canvas")).to_be_visible()
    portfolio.get_by_role("button", name="Balance" if english else "余额", exact=True).click()
    expect(portfolio.get_by_role("button", name="Balance" if english else "余额", exact=True)).to_have_attribute("aria-pressed", "true")
    portfolio.get_by_role("button", name="Both" if english else "双指标", exact=True).click()

    portfolio.get_by_role("button", name="Custom" if english else "自定义", exact=True).click()
    now = datetime.now(timezone(timedelta(hours=8))).replace(second=0, microsecond=0)
    portfolio.get_by_label("Start time (Beijing)" if english else "开始时间（北京时间）", exact=True).fill((now - timedelta(hours=12)).strftime("%Y-%m-%dT%H:%M"))
    portfolio.get_by_label("End time (Beijing)" if english else "结束时间（北京时间）", exact=True).fill(now.strftime("%Y-%m-%dT%H:%M"))
    with page.expect_response("**/api/portfolio/equity-history?*") as response:
        portfolio.get_by_role("button", name="Apply time range" if english else "应用时间范围", exact=True).click()
    assert response.value.json()["to_ms"] - response.value.json()["from_ms"] == 12 * 3_600_000
    page.reload()
    expect(portfolio.get_by_role("button", name="Custom" if english else "自定义", exact=True)).to_have_attribute("aria-pressed", "true")
    expect(portfolio.locator("canvas")).to_be_visible()
    portfolio.get_by_label("End time (Beijing)" if english else "结束时间（北京时间）", exact=True).fill((now + timedelta(days=1)).strftime("%Y-%m-%dT%H:%M"))
    portfolio.get_by_role("button", name="Apply time range" if english else "应用时间范围", exact=True).click()
    expect(portfolio.get_by_role("alert")).to_contain_text("90")
    portfolio.get_by_role("button", name="7D", exact=True).click()
    expect(portfolio.locator("canvas")).to_be_visible()
    expect(portfolio.locator(".trend-events")).to_be_visible()

    row = page.locator(".accounts-table tbody tr").filter(has_text="100102")
    with page.expect_response("**/api/accounts/*/equity-history?*") as response:
        row.get_by_role("link").first.click()
    native = response.value.json()
    account = page.get_by_role("region", name="Account asset history" if english else "账户资金历史", exact=True)
    expect(account.locator("canvas")).to_be_visible()
    expect(account.locator(".trend-values")).to_contain_text("USC")
    with page.expect_response("**/api/accounts/*/equity-history?*") as response:
        account.get_by_label("Chart display unit" if english else "走势图显示单位", exact=True).select_option("USD")
    converted = response.value.json()
    native_by_id = {point.get("snapshot_id"): point for point in native["points"]}
    matched = 0
    for point in converted["points"]:
        raw = native_by_id.get(point.get("snapshot_id"))
        if raw and raw["balance"] is not None and point["balance"] is not None:
            assert Decimal(raw["balance"]) / 100 == Decimal(point["balance"])
            matched += 1
    assert matched > 100
    expect(account.locator(".trend-values")).to_contain_text("USD")
    chart = account.locator(".trend-chart")
    box = chart.bounding_box()
    page.mouse.click(box["x"] + box["width"] * 0.65, box["y"] + 100)
    expect(account.locator("[data-selected-trend]")).to_be_visible()
    account.locator(".trend-details summary").click()
    expect(account.locator(".trend-table tbody tr")).to_have_count(20)
    account.locator(".trend-table tbody tr").first.get_by_role("button").click()
    expect(account.locator("[data-selected-trend]")).to_be_visible()
    account.get_by_role("button", name="Next" if english else "下一页", exact=True).click()
    account.get_by_role("button", name="Previous" if english else "上一页", exact=True).click()
    account.screenshot(path=str(artifacts / f"account-{locale}-desktop.png"))

    # Error feedback is local to the chart; retry recovers the original page.
    def fail_trend(route):
        route.fulfill(status=503, json={"error": "Synthetic trend failure"})

    page.route("**/api/accounts/*/equity-history?*", fail_trend)
    account.get_by_role("button", name="Refresh asset trend" if english else "刷新资产走势", exact=True).click()
    expect(account.get_by_role("alert")).to_contain_text("Synthetic trend failure")
    page.unroute("**/api/accounts/*/equity-history?*", fail_trend)
    account.get_by_role("button", name="Retry" if english else "重试", exact=True).click()
    expect(account.locator("canvas")).to_be_visible()

    for width in (390, 320):
        page.set_viewport_size({"width": width, "height": 844})
        account.scroll_into_view_if_needed()
        expect(account.locator("canvas")).to_be_visible()
        no_page_overflow(page)
        account.screenshot(path=str(artifacts / f"account-{locale}-{width}.png"))
        page.get_by_role("link", name="Account overview" if english else "账户总览", exact=True).click()
        expect(portfolio.locator("canvas")).to_be_visible()
        no_page_overflow(page)
        portfolio.screenshot(path=str(artifacts / f"overview-{locale}-{width}.png"))
        page.locator(".accounts-table tbody tr").filter(has_text="100102").get_by_role("link").first.click()
        expect(account.locator("canvas")).to_be_visible()
    assert not errors, errors
    context.close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--server", type=Path, default=ROOT / ".local/bin/e2e-server")
    parser.add_argument("--web", type=Path, default=ROOT / ".local/web")
    parser.add_argument("--artifacts", type=Path, default=ROOT / ".local/browser/trends")
    args = parser.parse_args()
    args.artifacts.mkdir(parents=True, exist_ok=True)
    with synthetic_server(args.server.resolve(), args.web.resolve()) as origin, sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        try:
            for locale in ("zh-CN", "en"):
                check_locale(browser, origin, locale, args.artifacts)
            context = browser.new_context(
                viewport={"width": 390, "height": 844}, locale="zh-CN",
                is_mobile=True, has_touch=True, device_scale_factor=1,
            )
            page = context.new_page()
            page.goto(origin)
            page.wait_for_load_state("networkidle")
            page.get_by_label("系统密码", exact=True).fill(PASSWORD)
            page.get_by_role("button", name="安全登录", exact=True).click()
            region = page.get_by_role("region", name="资产走势", exact=True)
            expect(region.locator("canvas")).to_be_visible()
            region.locator(".trend-chart").scroll_into_view_if_needed()
            box = region.locator(".trend-chart").bounding_box()
            page.touchscreen.tap(box["x"] + box["width"] * 0.65, box["y"] + 100)
            expect(region.locator("[data-selected-trend]")).to_be_visible()
            no_page_overflow(page)
            region.screenshot(path=str(args.artifacts / "overview-touch.png"))
            context.close()
        finally:
            browser.close()
    print("PASS: portfolio/account history, presets/custom URL, units, selection, details, error recovery, bilingual desktop/390/320 layouts; synthetic data only")


if __name__ == "__main__":
    main()
