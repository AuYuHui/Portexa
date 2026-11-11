"""Synthetic known-server calendar: daily totals work without a timezone form."""

from playwright.sync_api import expect, sync_playwright

from dashboard import ARTIFACTS, ORIGIN, PASSWORD, no_page_overflow


def main():
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1440, "height": 1000}, locale="zh-CN")
        page = context.new_page()
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.goto(ORIGIN)
        page.wait_for_load_state("networkidle")
        page.get_by_label("系统密码", exact=True).fill(PASSWORD)
        page.get_by_role("button", name="安全登录").click()
        expect(page.get_by_role("heading", name="账户总览")).to_be_visible()
        row = page.locator(".accounts-table tbody tr").filter(has_text="100102")
        row.get_by_role("link").first.click()
        page.wait_for_load_state("networkidle")
        daily = page.get_by_role("region", name="当日利润与结余")
        expect(daily.locator(".metric-value").first).to_have_text("9.50", timeout=15_000)
        expect(daily).to_contain_text("UTC+2")
        expect(daily.get_by_role("link", name="查看服务器时间设置")).to_have_count(0)

        page.get_by_role("link", name="接入与设置", exact=True).click()
        server = page.locator(".server-card").filter(has=page.get_by_role("heading", name="DPrimeVU-Demo 999", exact=True))
        expect(server).to_contain_text("UTC+2（自动识别）")
        server.get_by_role("button", name="服务器时间", exact=True).click()
        dialog = page.get_by_role("dialog")
        expect(dialog.get_by_text("自动识别 · UTC+2", exact=True)).to_be_visible()
        expect(dialog.get_by_role("checkbox")).to_have_count(0)
        expect(dialog.get_by_label("服务器时区")).to_be_hidden()
        expect(dialog.get_by_role("button", name="使用自动识别", exact=True)).to_have_count(0)
        page.screenshot(path=str(ARTIFACTS / "automatic-server-time-desktop.png"), full_page=True)

        # A deliberate correction remains available without evidence fields.
        dialog.get_by_text("高级：手动校正", exact=True).click()
        dialog.get_by_label("服务器时区").fill("UTC")
        dialog.get_by_role("button", name="保存手动校正", exact=True).click()
        expect(dialog).not_to_be_visible()
        expect(server).to_contain_text("UTC（手动校正）")
        server.get_by_role("button", name="服务器时间", exact=True).click()
        dialog.get_by_role("button", name="使用自动识别", exact=True).click()
        expect(dialog).not_to_be_visible()
        expect(server).to_contain_text("UTC+2（自动识别）")

        page.set_viewport_size({"width": 390, "height": 844})
        server.get_by_role("button", name="服务器时间", exact=True).click()
        no_page_overflow(page)
        expect(dialog.get_by_label("服务器时区")).to_be_hidden()
        page.screenshot(path=str(ARTIFACTS / "automatic-server-time-mobile.png"), full_page=True)
        assert not errors, errors
        browser.close()
        print("PASS: known server automatic time, daily totals without setup, hidden advanced correction, one-click automatic restore, desktop/mobile")


if __name__ == "__main__":
    main()
