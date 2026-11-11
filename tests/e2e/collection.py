"""Collection settings acceptance using the synthetic browser fixture only."""

from pathlib import Path

from playwright.sync_api import expect, sync_playwright

from dashboard import ORIGIN, PASSWORD, no_page_overflow


ARTIFACTS = Path(".local/browser")


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
        expect(page.locator(".panel-footer")).to_contain_text("后台每 60 秒计划采集")

        # The existing dashboard remains usable before the backend is upgraded.
        def legacy_overview(route):
            response = route.fetch()
            body = response.json()
            body.pop("collection_settings", None)
            route.fulfill(response=response, json=body)

        page.route("**/api/overview", legacy_overview)
        page.reload()
        expect(page.locator(".panel-footer")).to_contain_text("后台每 60 秒计划采集")
        page.unroute("**/api/overview", legacy_overview)
        page.get_by_role("link", name="接入与设置", exact=True).click()
        region = page.get_by_role("region", name="采集设置")
        cycle = region.get_by_label("后台采集周期")
        save = region.get_by_role("button", name="保存采集设置", exact=True)
        expect(cycle).to_have_value("60")
        expect(cycle.locator("option")).to_have_count(4)
        expect(save).to_be_disabled()

        for seconds in (30, 120, 300):
            cycle.select_option(str(seconds))
            expect(save).to_be_enabled()
            save.click()
            expect(region.get_by_role("status")).to_contain_text("已保存")
            expect(save).to_be_disabled()
            response = context.request.get(ORIGIN + "/api/settings/collection")
            assert response.status == 200
            assert response.json()["snapshot_interval_seconds"] == seconds
            page.get_by_role("link", name="账户总览", exact=True).click()
            expect(page.locator(".panel-footer")).to_contain_text(f"后台每 {seconds} 秒计划采集")
            expect(page.locator(".panel-footer")).to_contain_text("页面每 10 秒刷新")
            page.get_by_role("link", name="接入与设置", exact=True).click()
            expect(cycle).to_have_value(str(seconds))

        page.reload()
        page.wait_for_load_state("networkidle")
        expect(cycle).to_have_value("300")
        region.scroll_into_view_if_needed()
        no_page_overflow(page)
        page.screenshot(path=str(ARTIFACTS / "collection-desktop.png"), full_page=True)
        region.screenshot(path=str(ARTIFACTS / "collection-desktop-panel.png"))

        # A rejected save keeps the draft and the existing persisted cycle.
        def reject_save(route):
            if route.request.method == "PUT":
                route.fulfill(status=500, json={"error": "合成保存失败，请重试"})
            else:
                route.continue_()

        page.route("**/api/settings/collection", reject_save)
        cycle.select_option("60")
        save.click()
        expect(region.get_by_role("alert")).to_contain_text("合成保存失败")
        expect(cycle).to_have_value("60")
        expect(save).to_be_enabled()
        assert context.request.get(ORIGIN + "/api/settings/collection").json()["snapshot_interval_seconds"] == 300
        page.unroute("**/api/settings/collection", reject_save)
        save.click()
        expect(region.get_by_role("status")).to_contain_text("已保存")

        # A failed initial load exposes retry and does not offer an editable default.
        def reject_load(route):
            route.fulfill(status=503, json={"error": "合成设置读取失败"})

        page.route("**/api/settings/collection", reject_load)
        page.reload()
        expect(region.get_by_role("alert")).to_contain_text("合成设置读取失败")
        expect(save).to_have_count(0)
        page.unroute("**/api/settings/collection", reject_load)
        region.get_by_role("button", name="重试", exact=True).click()
        expect(cycle).to_have_value("60")

        page.set_viewport_size({"width": 390, "height": 844})
        region.scroll_into_view_if_needed()
        no_page_overflow(page)
        expect(cycle).to_be_visible()
        expect(save).to_be_visible()
        page.screenshot(path=str(ARTIFACTS / "collection-mobile.png"), full_page=True)
        region.screenshot(path=str(ARTIFACTS / "collection-mobile-panel.png"))
        assert not errors, errors
        browser.close()
        print("PASS: collection presets, persistence, overview labels, rejected save/retry, failed load/retry, desktop/mobile layouts; synthetic data only")


if __name__ == "__main__":
    main()
