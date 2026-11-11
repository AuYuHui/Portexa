"""Synthetic acceptance for server-day profit, balance changes and missing data."""

import re

from playwright.sync_api import expect, sync_playwright

from dashboard import ARTIFACTS, ORIGIN, PASSWORD, no_page_overflow


DETAIL = re.compile(r"/api/accounts/[^/?]+(?:\?.*)?$")
OVERVIEW = re.compile(r"/api/overview$")


def legacy_api(route):
    response = route.fetch()
    body = response.json()
    if "accounts" in body:
        for account in body["accounts"]:
            account.pop("daily", None)
    elif "account" in body:
        body["account"].pop("daily", None)
    route.fulfill(response=response, json=body)


def main():
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1440, "height": 1000}, locale="zh-CN")
        page = context.new_page()
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        # Updating the web bundle before restarting an older backend stays usable.
        page.route(OVERVIEW, legacy_api)
        page.route(DETAIL, legacy_api)
        page.goto(ORIGIN)
        page.wait_for_load_state("networkidle")
        page.get_by_label("系统密码", exact=True).fill(PASSWORD)
        page.get_by_role("button", name="安全登录").click()
        expect(page.get_by_role("heading", name="账户总览")).to_be_visible()
        expect(page.locator(".hero-value")).to_contain_text("27,049.11")
        row = page.locator(".accounts-table tbody tr").filter(has_text="100102")
        row.get_by_role("link").first.click()
        page.wait_for_load_state("networkidle")
        expect(page.locator(".detail-metrics .metric-value").first).to_have_text("827,650.53 USC")
        expect(page.locator(".daily-panel")).to_have_count(0)
        page.unroute(OVERVIEW)
        page.unroute(DETAIL)
        page.reload()
        page.wait_for_load_state("networkidle")
        panel = page.get_by_role("region", name="当日利润与结余")
        expect(panel).to_contain_text("时间校准中")
        panel.get_by_role("link", name="查看服务器时间设置").click()
        server = page.locator(".server-card").filter(has=page.get_by_role("heading", name="Synthetic-Live01", exact=True))
        server.get_by_role("button", name="服务器时间", exact=True).click()
        dialog = page.get_by_role("dialog")
        expect(dialog).to_contain_text("交易时间自动校准中")
        expect(dialog).to_contain_text("0 / 3 笔新平仓")
        expect(dialog.get_by_label("服务器时区")).to_be_hidden()
        dialog.get_by_text("高级：手动校正", exact=True).click()
        dialog.get_by_label("服务器时区").fill("Europe/Helsinki")
        expect(dialog.get_by_role("checkbox")).to_have_count(0)
        dialog.get_by_role("button", name="保存手动校正", exact=True).click()
        expect(dialog).not_to_be_visible()
        expect(server).to_contain_text("手动校正")
        page.get_by_role("link", name="账户总览", exact=True).click()
        page.locator(".accounts-table tbody tr").filter(has_text="100102").get_by_role("link").first.click()
        page.wait_for_load_state("networkidle")
        values = panel.locator(".daily-metrics .metric-value")
        expect(values.nth(0)).to_have_text("9.50", timeout=15_000)
        expect(values.nth(1)).to_have_text("89.50")
        expect(panel.locator(".card-label").nth(0)).to_contain_text("USC")
        expect(panel).to_contain_text("已平仓 2 笔 · 余额调整 80.00 · 信用变动 25.00")
        expect(page.locator(".detail-metrics .metric-value").first).to_have_text("827,650.53 USC")
        no_page_overflow(page)
        page.screenshot(path=str(ARTIFACTS / "daily-usc-desktop.png"), full_page=True)

        panel.get_by_role("button", name="刷新当日", exact=True).click()
        expect(panel.get_by_role("status")).to_contain_text("已加入队列")
        page.get_by_role("link", name="账户总览", exact=True).click()
        page.wait_for_load_state("networkidle")
        row = page.locator(".accounts-table tbody tr").filter(has_text="100102")
        expect(row.get_by_text("+9.50", exact=True)).to_be_visible()
        expect(row.get_by_text("+89.50", exact=True)).to_be_visible()
        page.screenshot(path=str(ARTIFACTS / "daily-overview-desktop.png"), full_page=True)
        row.get_by_role("link").first.click()
        page.wait_for_load_state("networkidle")

        mode = "failed"

        def change_state(route):
            response = route.fetch()
            body = response.json()
            daily = body["account"]["daily"]
            if mode == "failed":
                daily.update(state="failed", error_code="timeout", stale=True)
            elif mode == "missing":
                daily.update(state="idle", error_code=None, stale=False, report=None)
            elif mode == "missing_failed":
                daily.update(state="failed", error_code="timeout", stale=False, report=None)
            route.fulfill(response=response, json=body)

        page.route(DETAIL, change_state)
        page.reload()
        page.wait_for_load_state("networkidle")
        expect(values.nth(0)).to_have_text("9.50")
        expect(panel).to_contain_text("保留上次当日结果")
        mode = "missing"
        page.reload()
        page.wait_for_load_state("networkidle")
        expect(values.nth(0)).to_have_text("—")
        expect(values.nth(1)).to_have_text("—")
        mode = "missing_failed"
        page.reload()
        page.wait_for_load_state("networkidle")
        expect(values.nth(0)).to_have_text("—")
        expect(panel).to_contain_text("尚无当日完整结果")
        mode = "ready"
        page.set_viewport_size({"width": 390, "height": 844})
        page.reload()
        page.wait_for_load_state("networkidle")
        expect(values.nth(0)).to_have_text("9.50")
        expect(values.nth(1)).to_have_text("89.50")
        no_page_overflow(page)
        page.screenshot(path=str(ARTIFACTS / "daily-usc-mobile.png"), full_page=True)
        assert not errors, errors
        browser.close()
        print("PASS: old API compatibility, timezone confirmation, native USC daily profit/balance, credits, refresh, failed/missing data and mobile")


if __name__ == "__main__":
    main()
