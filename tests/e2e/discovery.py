"""Exercise the server-directory flow with a synthetic provider, never trading accounts."""

from pathlib import Path
import re

from playwright.sync_api import expect, sync_playwright


ORIGIN = "http://127.0.0.1:18080"
ARTIFACTS = Path(".local/browser")


def main():
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1440, "height": 1000})
        page = context.new_page()
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        assert context.request.get(ORIGIN + "/api/server-directory?q=Directory").status == 401
        page.goto(ORIGIN)
        page.wait_for_load_state("networkidle")
        page.get_by_label("系统密码", exact=True).fill("synthetic-login-password-2026")
        page.get_by_role("button", name="安全登录").click()
        page.get_by_role("link", name="接入与设置", exact=True).click()
        page.get_by_role("button", name="登记服务器", exact=True).click()
        dialog = page.get_by_role("dialog")
        expect(dialog.get_by_label("固定 IP 地址")).to_have_count(0)
        query = dialog.get_by_label("搜索经纪商或服务器", exact=True)
        query.fill("Unknown company")
        expect(dialog.get_by_text("没有找到匹配的 MT4 服务器")).to_be_visible()
        query.fill("Synthetic Directory")
        result = dialog.get_by_role("button", name="选择服务器 Synthetic-DirectoryLive", exact=True)
        expect(result).to_be_visible()
        page.screenshot(path=str(ARTIFACTS / "directory-search-desktop.png"), full_page=True)
        result.click()
        expect(dialog.get_by_text("已取得 2 个候选接入点")).to_be_visible()
        expect(dialog.get_by_role("button", name="登记并确认接入")).to_be_disabled()
        expect(dialog.get_by_label("证据 SHA-256")).to_have_count(0)
        dialog.get_by_role("checkbox", name=re.compile("我已核对经纪商和服务器")).check()
        page.screenshot(path=str(ARTIFACTS / "directory-confirm-desktop.png"), full_page=True)
        dialog.get_by_role("button", name="登记并确认接入").click()
        expect(dialog).not_to_be_visible()
        server = page.locator(".server-card").filter(has=page.get_by_role("heading", name="Synthetic-DirectoryLive", exact=True))
        expect(server.get_by_text("已核验", exact=True)).to_be_visible()
        expect(server.get_by_role("button", name="核验接入点")).to_have_count(0)

        # A new revision also discovers candidates and records a fresh explicit confirmation.
        server.get_by_role("button", name="新建接入修订").click()
        dialog = page.get_by_role("dialog")
        expect(dialog.get_by_label("搜索经纪商或服务器", exact=True)).to_have_value("Synthetic-DirectoryLive")
        dialog.get_by_role("button", name="选择服务器 Synthetic-DirectoryLive", exact=True).click()
        expect(dialog.get_by_role("checkbox")).not_to_be_checked()
        dialog.get_by_text("查看接入地址与来源", exact=True).click()
        dialog.get_by_label("目录候选接入点").select_option("1")
        dialog.get_by_role("checkbox").check()
        dialog.get_by_role("button", name="确认新接入修订并暂停账户").click()
        expect(dialog).not_to_be_visible()
        expect(server.get_by_text("已停用", exact=True)).to_be_visible()
        expect(server.get_by_text("203.0.113.10:443", exact=True)).to_be_visible()

        # Manual configuration is an explicit advanced path; discovery is the default.
        page.set_viewport_size({"width": 390, "height": 844})
        page.get_by_role("button", name="登记服务器", exact=True).click()
        dialog = page.get_by_role("dialog")
        query = dialog.get_by_label("搜索经纪商或服务器", exact=True)
        query.fill("Synthetic Directory")
        expect(dialog.get_by_role("button", name="选择服务器 Synthetic-DirectoryLive", exact=True)).to_be_visible()
        assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
        page.screenshot(path=str(ARTIFACTS / "directory-search-mobile.png"), full_page=True)
        dialog.get_by_role("button", name="手动填写（高级）", exact=True).click()
        expect(dialog.get_by_label("固定 IP 地址")).to_be_visible()
        dialog.get_by_role("button", name="搜索服务器", exact=True).click()
        expect(dialog.get_by_label("固定 IP 地址")).to_have_count(0)
        dialog.get_by_role("button", name="关闭", exact=True).click()
        assert not errors, errors
        browser.close()
        print("PASS: authenticated directory search, no-result state, automatic addresses/provenance, explicit confirmation, revision discovery, mobile layout and advanced manual fallback; no browser errors")


if __name__ == "__main__":
    main()
