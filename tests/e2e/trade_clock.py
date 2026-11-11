"""Unknown-server clock is calibrated by real synthetic publication events."""

from playwright.sync_api import expect, sync_playwright

from dashboard import ARTIFACTS, ORIGIN, PASSWORD, no_page_overflow


def main():
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page(viewport={"width": 1440, "height": 1000}, locale="zh-CN")
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
        panel = page.get_by_role("region", name="当日利润与结余")
        expect(panel.locator(".metric-value").first).to_have_text("9.50", timeout=15_000)
        expect(panel).to_contain_text("UTC+5:45")
        expect(panel.get_by_role("link", name="查看服务器时间设置")).to_have_count(0)
        page.get_by_role("link", name="接入与设置", exact=True).click()
        page.wait_for_load_state("networkidle")
        server = page.locator(".server-card").filter(has=page.get_by_role("heading", name="Synthetic-Live01", exact=True))
        expect(server).to_contain_text("UTC+5:45（交易时间校准）")
        server.get_by_role("button", name="服务器时间", exact=True).click()
        dialog = page.get_by_role("dialog")
        expect(dialog).to_contain_text("交易时间已校准 · UTC+5:45")
        expect(dialog).to_contain_text("已核对 3 笔新平仓 · 覆盖 2 轮采集")
        expect(dialog).to_contain_text("最近校准")
        expect(dialog.get_by_label("服务器时区")).to_be_hidden()
        expect(dialog.get_by_role("checkbox")).to_have_count(0)
        expect(dialog.get_by_role("link", name="官方时间说明")).to_have_count(0)
        expect(dialog.get_by_role("button", name="使用自动识别", exact=True)).to_have_count(0)
        no_page_overflow(page)
        page.screenshot(path=str(ARTIFACTS / "trade-clock-desktop.png"), full_page=True)

        # A manual override preserves learned evidence as an automatic option.
        dialog.get_by_text("高级：手动校正", exact=True).click()
        dialog.get_by_label("服务器时区").fill("UTC")
        dialog.get_by_role("button", name="保存手动校正", exact=True).click()
        expect(dialog).not_to_be_visible()
        expect(server).to_contain_text("UTC（手动校正）")
        server.get_by_role("button", name="服务器时间", exact=True).click()
        dialog.get_by_role("button", name="使用自动识别", exact=True).click()
        expect(dialog).not_to_be_visible()
        expect(server).to_contain_text("UTC+5:45（交易时间校准）")

        page.set_viewport_size({"width": 390, "height": 844})
        server.get_by_role("button", name="服务器时间", exact=True).click()
        expect(dialog).to_contain_text("交易时间已校准 · UTC+5:45")
        expect(dialog.get_by_label("服务器时区")).to_be_hidden()
        no_page_overflow(page)
        page.screenshot(path=str(ARTIFACTS / "trade-clock-mobile.png"), full_page=True)
        assert not errors, errors
        browser.close()
        print("PASS: unknown server event calibration, native daily totals, source/count display, manual override/restore, desktop/mobile")


if __name__ == "__main__":
    main()
