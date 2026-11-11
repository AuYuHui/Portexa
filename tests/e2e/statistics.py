"""Daily statistics, margin and confirmed cash-flow acceptance on synthetic data."""

import re

from playwright.sync_api import expect, sync_playwright

from dashboard import ARTIFACTS, ORIGIN, PASSWORD, no_page_overflow


DETAIL = re.compile(r"/api/accounts/[^/?]+(?:\?.*)?$")


def classify(page, panel, ticket, category):
    panel.get_by_role("button", name=f"分类流水 {ticket}", exact=True).click()
    dialog = page.get_by_role("dialog")
    dialog.get_by_label("资金分类", exact=True).select_option(category)
    dialog.get_by_label("校正原因", exact=True).fill("合成账单已核对到账来源")
    dialog.get_by_role("button", name="保存校正", exact=True).click()
    expect(dialog).not_to_be_visible()


def main():
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1440, "height": 1000})
        page = context.new_page()
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.goto(ORIGIN)
        page.wait_for_load_state("networkidle")
        page.get_by_label("系统密码", exact=True).fill(PASSWORD)
        page.get_by_role("button", name="安全登录").click()
        expect(page.get_by_role("heading", name="账户总览")).to_be_visible()
        page.locator(".accounts-table tbody tr").filter(has_text="100102").get_by_role("link").first.click()
        page.wait_for_load_state("networkidle")
        daily = page.get_by_role("region", name="当日利润与结余", exact=True)
        values = daily.locator(".daily-metrics .metric-value")
        expect(values.nth(0)).to_have_text("9.50", timeout=15_000)
        expect(values.nth(1)).to_have_text("89.50")
        expect(values.nth(2)).to_have_text(re.compile(r"2\s*笔"))
        expect(values.nth(3)).to_have_text("50.00%")
        expect(values.nth(4)).to_have_text("12.10")
        expect(values.nth(5)).to_have_text("2.60")
        expect(daily).to_contain_text("净盈利 1 笔")
        expect(daily).to_contain_text("净亏损 1 笔")
        expect(page.locator(".detail-metrics .metric-value").nth(3)).to_have_text("1,034.56%")

        panel = page.get_by_role("region", name="累计出入金与资金流水", exact=True)
        deposits = panel.locator('[data-fund-total="deposits"]')
        withdrawals = panel.locator('[data-fund-total="withdrawals"]')
        expect(panel).to_contain_text("历史已回填", timeout=20_000)
        expect(panel.locator(".funds-table tbody tr")).to_have_count(7)
        expect(deposits).to_have_text("1,100.00")
        expect(withdrawals).to_have_text("20.00")
        expect(panel).to_contain_text("自动识别 5 笔")
        expect(panel).to_contain_text("待识别 1 笔")
        expect(panel.locator(".funds-table tbody tr").filter(has_text="#69901")).to_contain_text("自动识别")
        credit = panel.locator(".funds-table tbody tr").filter(has_text="#70005")
        expect(credit).to_contain_text("信用金（另计）")
        expect(credit.get_by_role("button")).to_have_count(0)
        classify(page, panel, 69904, "rebate")
        expect(deposits).to_have_text("1,100.00")
        expect(panel).to_contain_text("人工校正 1 笔")
        expect(panel).to_contain_text("待识别 0 笔")
        panel.get_by_role("checkbox", name="仅看待识别").check()
        expect(panel.locator(".funds-table tbody tr")).to_have_count(0)
        expect(panel).to_contain_text("当前没有待识别流水")
        panel.get_by_role("checkbox", name="仅看待识别").uncheck()
        expect(panel.locator(".funds-table tbody tr")).to_have_count(7)
        classify(page, panel, 70003, "rebate")
        expect(deposits).to_have_text("1,000.00")
        classify(page, panel, 70003, "deposit")
        expect(deposits).to_have_text("1,100.00")

        panel.get_by_role("button", name="回填 / 重查历史", exact=True).click()
        expect(panel.get_by_role("status")).to_contain_text("已加入队列")
        expect(deposits).to_have_text("1,100.00")
        expect(withdrawals).to_have_text("20.00")
        no_page_overflow(page)
        page.screenshot(path=str(ARTIFACTS / "statistics-desktop.png"), full_page=True)
        page.get_by_role("link", name="账户总览", exact=True).click()
        page.wait_for_load_state("networkidle")
        row = page.locator(".accounts-table tbody tr").filter(has_text="100102")
        expect(row).to_contain_text("2 笔 · 50.00%")
        expect(row).to_contain_text("盈 1 / 亏 1 / 平 0")
        expect(row).to_contain_text("1,100.00 / 20.00")
        expect(row).to_contain_text("1,034.56%")
        no_page_overflow(page)
        row.get_by_role("link").first.click()
        page.wait_for_load_state("networkidle")
        page.set_viewport_size({"width": 390, "height": 844})
        expect(values.nth(3)).to_have_text("50.00%")
        expect(deposits).to_have_text("1,100.00")
        no_page_overflow(page)
        page.screenshot(path=str(ARTIFACTS / "statistics-mobile.png"), full_page=True)

        mode = "missing"

        def edge_state(route):
            response = route.fetch()
            body = response.json()
            account = body["account"]
            snapshot = account["snapshot"]
            if mode == "missing":
                account["daily"].update(state="idle", report=None)
                account["funding"].update(deposits=None, withdrawals=None, coverage_complete=False)
                snapshot.update(margin_available=False, margin=None, margin_level=None)
            elif mode == "empty":
                account["daily"]["report"].update(closed_trades=0, winning_trades=0, losing_trades=0,
                    breakeven_trades=0, gross_profit="0", gross_loss="0", win_rate=None)
                snapshot.update(margin_available=True, margin="0", margin_level=None)
            elif mode == "legacy":
                account.pop("funding", None)
                for key in ("winning_trades", "losing_trades", "breakeven_trades", "gross_profit", "gross_loss", "win_rate"):
                    account["daily"]["report"].pop(key, None)
                snapshot.pop("margin_level", None)
            route.fulfill(response=response, json=body)

        page.route(DETAIL, edge_state)
        page.reload()
        page.wait_for_load_state("networkidle")
        expect(values.nth(2)).to_have_text(re.compile(r"—\s*笔"))
        expect(values.nth(3)).to_have_text("—")
        expect(values.nth(4)).to_have_text("—")
        expect(deposits).to_have_text("—")
        expect(withdrawals).to_have_text("—")
        expect(page.locator(".detail-metrics .metric-value").nth(3)).to_have_text("—")
        mode = "empty"
        page.reload()
        page.wait_for_load_state("networkidle")
        expect(values.nth(2)).to_have_text(re.compile(r"0\s*笔"))
        expect(values.nth(3)).to_have_text("—")
        expect(values.nth(4)).to_have_text("0.00")
        expect(page.locator(".detail-metrics .metric-value").nth(3)).to_have_text("无占用")
        mode = "legacy"
        page.reload()
        page.wait_for_load_state("networkidle")
        expect(panel).to_have_count(0)
        expect(values.nth(0)).to_have_text("9.50")
        expect(values.nth(3)).to_have_text("—")
        expect(daily).to_contain_text("旧日报尚无胜负明细")
        no_page_overflow(page)
        assert not errors, errors
        browser.close()
        print("PASS: daily net wins/losses, margin, full backfill, automatic cash flows, exception correction, credit exclusion, totals, old API and mobile")


if __name__ == "__main__":
    main()
