"""Chinese/English acceptance against an isolated synthetic fixture only."""

from pathlib import Path
import re

from playwright.sync_api import expect, sync_playwright

from dashboard import ORIGIN, PASSWORD, no_page_overflow


ARTIFACTS = Path(".local/browser/i18n")


def language(page, value):
    page.locator(".language-switch select").select_option(value)
    expect(page.locator("html")).to_have_attribute("lang", value)


def screenshot(page, name):
    page.screenshot(path=str(ARTIFACTS / name), full_page=True)
    no_page_overflow(page)


def preference_checks(browser):
    context = browser.new_context(locale="fr-FR")
    page = context.new_page()
    page.goto(ORIGIN)
    expect(page.get_by_role("heading", name="进入你的工作台")).to_be_visible()
    language(page, "en")
    page.reload()
    expect(page.get_by_role("heading", name="Enter your workspace")).to_be_visible()
    assert page.evaluate("localStorage.getItem('portexa.locale')") == "en"
    page.evaluate("localStorage.setItem('portexa.locale', 'unsupported')")
    page.reload()
    expect(page.get_by_role("heading", name="进入你的工作台")).to_be_visible()
    context.close()

    context = browser.new_context(locale="en-GB")
    page = context.new_page()
    page.goto(ORIGIN)
    expect(page.get_by_role("heading", name="Enter your workspace")).to_be_visible()
    language(page, "zh-CN")
    page.reload()
    expect(page.get_by_role("heading", name="进入你的工作台")).to_be_visible()
    context.close()

    context = browser.new_context(locale="en-US")
    context.add_init_script("""
        for (const method of ['getItem', 'setItem']) {
            Storage.prototype[method] = () => { throw new Error('storage disabled'); };
        }
    """)
    page = context.new_page()
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    page.goto(ORIGIN)
    expect(page.get_by_role("heading", name="Enter your workspace")).to_be_visible()
    language(page, "zh-CN")
    expect(page.get_by_role("heading", name="进入你的工作台")).to_be_visible()
    page.reload()
    expect(page.get_by_role("heading", name="Enter your workspace")).to_be_visible()
    assert not errors, errors
    context.close()


def main():
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        preference_checks(browser)
        context = browser.new_context(locale="en-GB", viewport={"width": 1440, "height": 1000})
        page = context.new_page()
        errors = []
        warnings = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.on("console", lambda message: warnings.append(message.text) if "[intlify]" in message.text else None)
        page.goto(ORIGIN)
        expect(page.get_by_role("heading", name="Enter your workspace")).to_be_visible()
        expect(page.get_by_label("Language")).to_have_value("en")
        screenshot(page, "login-desktop-en.png")

        # Server errors and typed input both survive a locale change.
        page.get_by_label("System password", exact=True).fill("wrong-synthetic-password")
        language(page, "zh-CN")
        expect(page.get_by_label("系统密码", exact=True)).to_have_value("wrong-synthetic-password")
        page.get_by_role("button", name="安全登录", exact=True).click()
        expect(page.get_by_role("alert")).to_contain_text("登录已失效或密码错误")
        language(page, "en")
        expect(page.get_by_role("alert")).to_contain_text("The session has expired or the password is incorrect.")
        page.get_by_label("System password", exact=True).fill(PASSWORD)
        page.get_by_role("button", name="Sign in securely", exact=True).click()
        expect(page.get_by_role("heading", name="Account overview")).to_be_visible()
        expect(page).to_have_title("Portexa · Account overview")
        expect(page.locator(".hero-value")).to_contain_text("27,049.11")
        expect(page.get_by_role("columnheader", name="Balance", exact=True)).to_be_visible()
        expect(page.get_by_text("Paused · Stale", exact=True)).to_be_visible()

        # Headers and status labels update without refetching or remounting.
        language(page, "zh-CN")
        expect(page.get_by_role("columnheader", name="余额", exact=True)).to_be_visible()
        expect(page.get_by_text("已暂停 · 过期", exact=True)).to_be_visible()
        language(page, "en")
        expect(page.get_by_role("columnheader", name="Balance", exact=True)).to_be_visible()
        page.get_by_label("Search accounts", exact=True).fill("100102")
        expect(page.locator(".accounts-table tbody tr")).to_have_count(1)
        row = page.locator(".accounts-table tbody tr")
        expect(row).to_contain_text("美分账户 · 多策略")
        page.get_by_label("Search accounts", exact=True).fill("")
        screenshot(page, "overview-desktop-en.png")
        row = page.locator(".accounts-table tbody tr").filter(has_text="100102")
        row.get_by_role("link").first.click()
        expect(page).to_have_title("Portexa · Account details")
        daily = page.get_by_role("region", name="Today's profit and balance change", exact=True)
        expect(daily.locator(".daily-metrics .metric-value").nth(0)).to_have_text("9.50", timeout=20_000)
        expect(daily).to_contain_text("Net winning trades: 1")
        page.get_by_role("button", name="Refresh today", exact=True).click()
        expect(daily.get_by_role("status")).to_contain_text("Daily history collection queued.")
        language(page, "zh-CN")
        expect(page.locator(".daily-panel [role=status]")).to_contain_text("当日历史采集已加入队列")
        language(page, "en")

        funds = page.get_by_role("region", name="Total deposits, withdrawals, and fund entries", exact=True)
        expect(funds).to_contain_text("History backfilled", timeout=20_000)
        expect(funds.locator('[data-fund-total="deposits"]')).to_have_text("1,100.00")
        automatic = funds.locator("tbody tr").filter(has_text="#69901")
        expect(automatic).to_contain_text("Comment contains a broker deposit reference")
        expect(automatic).to_contain_text("D(DIN100000000000000001)")
        expect(page.locator(".positions-table tbody tr").first).to_contain_text("Server wall clock")

        # Category options react immediately; manually entered evidence remains literal.
        funds.get_by_role("button", name="Classify entry 70003", exact=True).click()
        dialog = page.get_by_role("dialog")
        expect(dialog.locator("#fund-category option").first).to_have_text("External deposit")
        language(page, "zh-CN")
        expect(dialog.locator("#fund-category option").first).to_have_text("外部入金")
        dialog.get_by_label("校正原因", exact=True).fill("备注含入金标识")
        language(page, "en")
        expect(dialog.get_by_label("Correction reason", exact=True)).to_have_value("备注含入金标识")
        dialog.get_by_role("button", name="Save correction", exact=True).click()
        expect(dialog).not_to_be_visible()
        expect(funds.locator("tbody tr").filter(has_text="#70003")).to_contain_text("备注含入金标识")

        page.get_by_role("button", name="Account settings", exact=True).click()
        dialog = page.get_by_role("dialog")
        dialog.get_by_label("Account name", exact=True).fill("原币")
        dialog.get_by_label("Investor password", exact=False).fill("中文中文中文")
        dialog.get_by_role("button", name="Save account settings", exact=True).click()
        expect(dialog.get_by_role("alert")).to_contain_text("1 to 16 UTF-8 bytes")
        language(page, "zh-CN")
        expect(dialog.get_by_label("账户名称", exact=True)).to_have_value("原币")
        expect(dialog.get_by_role("alert")).to_contain_text("投资者密码需要 1 至 16 个 UTF-8 字节")
        dialog.get_by_role("button", name="取消", exact=True).click()
        language(page, "en")
        screenshot(page, "account-desktop-en.png")
        page.reload()
        expect(page.get_by_role("button", name="Account settings", exact=True)).to_be_visible()
        expect(page.locator("html")).to_have_attribute("lang", "en")

        page.get_by_role("link", name="Connections & settings", exact=True).click()
        expect(page).to_have_title("Portexa · Connections & settings")
        expect(page.get_by_role("heading", name="Collection settings", exact=True)).to_be_visible()
        page.get_by_role("button", name="Server time", exact=True).click()
        dialog = page.get_by_role("dialog")
        expect(dialog).to_contain_text("D Prime's official rules")
        expect(dialog.get_by_text("Advanced: manual correction", exact=True)).to_be_visible()
        dialog.get_by_role("button", name="Close", exact=True).last.click()
        screenshot(page, "settings-desktop-en.png")

        for width in (390, 320):
            page.set_viewport_size({"width": width, "height": 844})
            screenshot(page, f"settings-mobile-{width}-en.png")
            page.get_by_role("link", name="Account overview", exact=True).click()
            expect(page.get_by_role("heading", name="Account overview")).to_be_visible()
            screenshot(page, f"overview-mobile-{width}-en.png")
            page.locator(".accounts-table tbody tr").filter(has_text="100102").get_by_role("link").first.click()
            expect(page.get_by_role("heading", name="Today's trading statistics", exact=True)).to_be_visible()
            screenshot(page, f"account-mobile-{width}-en.png")
            page.get_by_role("link", name="Connections & settings", exact=True).click()

        page.get_by_role("button", name="Sign out", exact=True).click()
        expect(page.get_by_role("heading", name="Enter your workspace")).to_be_visible()
        expect(page.locator(".login-story h1")).to_have_text("Every account, at a glance.")
        screenshot(page, "login-mobile-en.png")
        assert not errors, errors
        assert not warnings, warnings
        assert not re.search(r"\b(?:accountTable|fundingSummary|serverMessages)\.[a-zA-Z]", page.inner_text("body"))
        browser.close()
        print("PASS: browser language, persistence/fallback/storage denial, live headers/status/errors/categories, native data, system descriptions, desktop and 390/320px layouts")


if __name__ == "__main__":
    main()
