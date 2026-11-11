"""Synthetic browser acceptance; run with tests/e2e/server and a built web app."""

from pathlib import Path
import os
import re

from playwright.sync_api import expect, sync_playwright


ORIGIN = os.environ.get("PORTEXA_E2E_ORIGIN", "http://127.0.0.1:18080")
PASSWORD = "synthetic-login-password-2026"
ARTIFACTS = Path(".local/browser")


def no_page_overflow(page):
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth"), "Page overflows viewport"


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
        expect(page.get_by_role("heading", name="进入你的工作台")).to_be_visible()
        page.screenshot(path=str(ARTIFACTS / "login-desktop.png"), full_page=True)
        assert context.request.get(ORIGIN + "/api/overview").status == 401
        page.get_by_label("系统密码", exact=True).fill(PASSWORD)
        page.get_by_role("button", name="安全登录").click()
        expect(page.get_by_role("heading", name="账户总览")).to_be_visible()
        page.wait_for_load_state("networkidle")
        expect(page.locator(".accounts-table tbody tr")).to_have_count(5)
        expect(page.locator(".hero-value")).to_contain_text("27,049.11")
        expect(page.get_by_text("已暂停 · 过期",exact=True)).to_be_visible()
        page.get_by_label("搜索账户").fill("100102")
        expect(page.locator(".accounts-table tbody tr")).to_have_count(1)
        page.get_by_label("搜索账户").fill("")
        page.screenshot(path=str(ARTIFACTS / "overview-desktop.png"), full_page=True)
        no_page_overflow(page)

        # Create a target, independently review it, and explicitly accept risk.
        page.get_by_role("link", name="接入与设置", exact=True).click()
        page.get_by_role("button", name="登记服务器", exact=True).click()
        dialog = page.get_by_role("dialog")
        dialog.get_by_role("button", name="手动填写（高级）", exact=True).click()
        dialog.get_by_label("经纪商", exact=True).fill("Synthetic UI Broker")
        dialog.get_by_label("准确的 MT4 服务器名称").fill("Synthetic-UI")
        dialog.get_by_label("固定 IP 地址").fill("203.0.113.9")
        dialog.get_by_role("button", name="登记服务器，继续核验").click()
        expect(dialog).not_to_be_visible()
        server = page.locator(".server-card").filter(has=page.get_by_role("heading", name="Synthetic-UI", exact=True))
        server.get_by_role("button", name="核验接入点").click()
        dialog = page.get_by_role("dialog")
        dialog.get_by_label("独立核验依据").fill("synthetic-browser-evidence")
        dialog.get_by_label("证据 SHA-256").fill("ab" * 32)
        dialog.get_by_role("checkbox").check()
        page.screenshot(path=str(ARTIFACTS / "endpoint-review.png"), full_page=True)
        dialog.get_by_role("button", name="保存核验与风险确认").click()
        expect(dialog).not_to_be_visible()
        expect(server.get_by_text("已核验", exact=True)).to_be_visible()

        page.get_by_role("button", name="接入账户", exact=True).click()
        dialog = page.get_by_role("dialog")
        dialog.get_by_label("账户名称", exact=True).fill("浏览器合成账户")
        dialog.get_by_label("服务器", exact=True).select_option(label="Synthetic UI Broker / Synthetic-UI")
        dialog.get_by_label("MT4 账号", exact=True).fill("888001")
        dialog.get_by_label("投资者密码").fill("investor-demo")
        dialog.get_by_role("checkbox", name=re.compile("我已核对该账户")).check()
        dialog.get_by_role("button", name="保存并接入账户").click()
        expect(dialog).not_to_be_visible()
        page.get_by_role("link", name="账户总览", exact=True).click()
        page.get_by_role("link", name=re.compile("浏览器合成账户 888001")).click()
        expect(page.get_by_role("heading", name="浏览器合成账户.")).to_be_visible()
        expect(page.get_by_text("1,062.13", exact=True).first).to_be_visible(timeout=15_000)
        expect(page.locator(".positions-table tbody tr")).to_have_count(1)
        expect(page.get_by_text("12.12", exact=True).first).to_be_visible()
        page.get_by_label("订单排序").select_option("open_time_wall")
        expect(page.locator(".positions-table tbody tr")).to_have_count(1)
        page.get_by_role("button", name=re.compile("挂单")).click()
        expect(page.locator(".positions-table tbody tr")).to_have_count(0)
        page.get_by_role("button", name=re.compile("当前持仓")).click()
        page.get_by_role("button", name="立即采集").click()
        expect(page.get_by_role("status")).to_contain_text("已加入队列")
        page.get_by_role("button", name="暂停采集").click()
        expect(page.get_by_role("button", name="恢复采集")).to_be_visible()
        page.get_by_role("button", name="恢复采集").click()
        expect(page.get_by_role("button", name="暂停采集")).to_be_visible()
        page.screenshot(path=str(ARTIFACTS / "account-desktop.png"), full_page=True)
        page.get_by_role("button", name="账户配置").click()
        dialog = page.get_by_role("dialog")
        expect(dialog.get_by_label("投资者密码")).to_have_value("")
        dialog.get_by_label("账户名称", exact=True).fill("浏览器合成账户 · 已修改")
        dialog.get_by_role("button", name="保存账户配置").click()
        expect(dialog).not_to_be_visible()
        expect(page.get_by_role("heading", name="浏览器合成账户 · 已修改.")).to_be_visible()

        # Replacing the address must pause accounts and require a new approval.
        page.get_by_role("link", name="接入与设置", exact=True).click()
        server = page.locator(".server-card").filter(has=page.get_by_role("heading", name="Synthetic-UI", exact=True))
        server.get_by_role("button", name="新建接入修订").click()
        dialog = page.get_by_role("dialog")
        dialog.get_by_role("button", name="手动填写（高级）", exact=True).click()
        dialog.get_by_label("固定 IP 地址").fill("203.0.113.10")
        dialog.get_by_role("button", name="建立新修订并暂停账户").click()
        expect(dialog).not_to_be_visible()
        server.get_by_role("button", name="核验接入点").click()
        dialog = page.get_by_role("dialog")
        dialog.get_by_label("独立核验依据").fill("synthetic-revision-2")
        dialog.get_by_label("证据 SHA-256").fill("cd" * 32)
        dialog.get_by_role("checkbox").check()
        dialog.get_by_role("button", name="保存核验与风险确认").click()
        expect(dialog).not_to_be_visible()
        page.screenshot(path=str(ARTIFACTS / "settings-desktop.png"), full_page=True)
        page.get_by_role("link", name="账户总览", exact=True).click()
        page.get_by_role("link", name=re.compile("浏览器合成账户 · 已修改 888001")).click()
        expect(page.get_by_role("button", name="恢复采集")).to_be_visible()
        page.get_by_role("button", name="账户配置").click()
        dialog = page.get_by_role("dialog")
        dialog.get_by_label("固定接入点").select_option(label="修订 2 · 203.0.113.10:443")
        dialog.get_by_role("checkbox", name="启用自动快照采集").check()
        dialog.get_by_role("button", name="保存账户配置").click()
        expect(dialog).not_to_be_visible()
        expect(page.get_by_role("button", name="暂停采集")).to_be_visible()

        # Narrow layout: the page fits; only the data table scrolls horizontally.
        page.set_viewport_size({"width": 390, "height": 844})
        page.get_by_role("link", name="账户总览", exact=True).click()
        page.wait_for_load_state("networkidle")
        no_page_overflow(page)
        page.screenshot(path=str(ARTIFACTS / "overview-mobile.png"), full_page=True)
        page.get_by_role("link", name="接入与设置", exact=True).click()
        page.wait_for_load_state("networkidle")
        no_page_overflow(page)
        page.screenshot(path=str(ARTIFACTS / "settings-mobile.png"), full_page=True)

        # Password changes revoke this session; the next login uses the new password.
        if not page.get_by_label("当前密码", exact=True).count():
            page.get_by_role("button", name="修改密码", exact=True).first.click()
        page.get_by_label("当前密码", exact=True).fill(PASSWORD)
        page.get_by_label("新密码", exact=True).fill("new-synthetic-browser-password")
        page.get_by_label("再次输入新密码", exact=True).fill("new-synthetic-browser-password")
        page.get_by_role("button", name=re.compile("修改密码并退出登录|确认修改并重新登录")).click()
        expect(page.get_by_role("heading", name="进入你的工作台")).to_be_visible()
        assert context.request.get(ORIGIN + "/api/accounts").status == 401
        no_page_overflow(page)
        page.screenshot(path=str(ARTIFACTS / "login-mobile.png"), full_page=True)
        page.get_by_label("系统密码", exact=True).fill("new-synthetic-browser-password")
        page.get_by_role("button", name="安全登录").click()
        expect(page.get_by_role("heading", name="接入与设置.")).to_be_visible()
        page.get_by_role("button", name="退出登录", exact=True).click()
        expect(page.get_by_role("heading", name="进入你的工作台")).to_be_visible()
        assert not errors, errors
        print("PASS: login, endpoint review/revision, account onboarding/edit/rebind, snapshots, pause/resume, filters, mobile, password revocation, logout; no browser errors")
        browser.close()


if __name__ == "__main__":
    main()
