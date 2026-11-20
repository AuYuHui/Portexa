"""Register and view a synthetic MT5 account using the browser fixture."""

from datetime import datetime, timedelta, timezone
import os
import re

from playwright.sync_api import expect, sync_playwright


ORIGIN = os.environ.get("PORTEXA_E2E_ORIGIN", "http://127.0.0.1:18080")


def main():
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        try:
            page = browser.new_page(locale="zh-CN")
            page.set_default_timeout(12_000)
            errors = []
            page.on("pageerror", lambda error: errors.append(str(error)))
            page.goto(ORIGIN)
            page.wait_for_load_state("networkidle")
            page.get_by_label("系统密码", exact=True).fill("synthetic-login-password-2026")
            page.get_by_role("button", name="安全登录").click()
            expect(page.get_by_role("heading", name="账户总览")).to_be_visible()

            page.get_by_role("link", name="接入与设置").click()
            page.get_by_role("button", name="登记服务器", exact=True).click()
            dialog = page.get_by_role("dialog")
            dialog.get_by_role("button", name="手动填写（高级）").click()
            dialog.get_by_label("交易平台").select_option("mt5")
            dialog.get_by_label("经纪商", exact=True).fill("Synthetic Browser Broker")
            dialog.get_by_label("准确的 MT5 交易服务器名称").fill("Synthetic-Browser-MT5")
            dialog.get_by_label("MT5 Web 终端域名").fill("mt5.broker.example")
            dialog.get_by_label("固定 IP 地址").fill("203.0.113.9")
            dialog.get_by_role("button", name="登记服务器，继续核验").click()
            expect(dialog).not_to_be_visible()

            card = page.locator(".server-card").filter(has=page.get_by_role("heading", name="Synthetic-Browser-MT5"))
            expect(card).to_contain_text("mt5.broker.example")
            card.get_by_role("button", name="核验接入点").click()
            expect(dialog).to_contain_text("证书是否匹配 Web 终端域名")
            dialog.get_by_label("独立核验依据").fill("synthetic-browser-evidence")
            dialog.get_by_label("证据 SHA-256").fill("ab" * 32)
            dialog.get_by_role("checkbox").check()
            dialog.get_by_role("button", name="保存核验与风险确认").click()
            expect(card.get_by_text("已核验", exact=True)).to_be_visible()

            card.get_by_role("button", name=re.compile("^服务器时间")).click()
            dialog.get_by_label("服务器时区", exact=False).fill("UTC")
            verified_from = (datetime.now(timezone.utc) - timedelta(days=2)).date().isoformat()
            dialog.get_by_label("历史生效日期").fill(verified_from)
            dialog.get_by_role("button", name="保存手动校正").click()
            expect(dialog).not_to_be_visible()
            expect(card).to_contain_text("UTC")

            page.get_by_role("button", name="接入账户", exact=True).click()
            dialog.get_by_label("账户名称", exact=True).fill("浏览器 MT5 合成账户")
            dialog.get_by_label("服务器", exact=True).select_option(label="Synthetic Browser Broker / Synthetic-Browser-MT5 · MT5")
            dialog.get_by_label("MT5 登录号").fill("7000000001")
            dialog.get_by_label("投资者密码").fill("synthetic-investor-password")
            dialog.get_by_role("checkbox", name=re.compile("我已核对该账户")).check()
            dialog.get_by_role("button", name="保存并接入账户").click()
            expect(dialog).not_to_be_visible()

            page.get_by_role("link", name="账户总览").click()
            page.get_by_role("link", name=re.compile("浏览器 MT5 合成账户 7000000001")).click()
            expect(page.get_by_role("heading", name="浏览器 MT5 合成账户.")).to_be_visible()
            expect(page.get_by_text("估算净值", exact=True).first).to_be_visible()
            expect(page.locator(".detail-metrics")).to_contain_text("2,054.70", timeout=15_000)
            expect(page.locator(".positions-table tbody")).to_contain_text("TESTUSD")
            expect(page.locator(".positions-table tbody")).to_contain_text("0.00123456")
            page.get_by_role("button", name=re.compile("挂单")).click()
            expect(page.locator(".positions-table tbody")).to_contain_text("sell stop limit")
            expect(page.locator(".positions-table tbody")).to_contain_text("0.00654321")
            expect(page.locator(".positions-table tbody")).to_contain_text("触发价 1.22990")

            history = page.get_by_role("region", name="成交历史", exact=True)
            expect(history).to_contain_text("反手", timeout=15_000)
            expect(history).to_contain_text("8000001002")
            expect(history).to_contain_text("1.23456")
            funding = page.locator(".funding-panel")
            expect(funding).to_contain_text("50.00", timeout=15_000)
            expect(funding).to_contain_text("20.00")
            expect(funding).to_contain_text("synthetic unspecified")
            expect(page.get_by_text("MT5 胜率按平仓成交笔数统计。", exact=False)).to_be_visible()

            page.goto(ORIGIN + "/analytics")
            with page.expect_response(lambda response: "/api/analytics?" in response.url and "account_id=" in response.url) as analysis:
                page.locator("#analytics-account").select_option(label="浏览器 MT5 合成账户 · 7000000001")
            assert analysis.value.status == 200, analysis.value.status
            expect(page.get_by_text("MT5 收益分析使用估算净值", exact=False)).to_be_visible()
            assert errors == [], errors
        finally:
            browser.close()


if __name__ == "__main__":
    main()
