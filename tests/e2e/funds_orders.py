"""Check native USC and order rendering with synthetic API prices."""

import re

from playwright.sync_api import expect, sync_playwright

from dashboard import ARTIFACTS, ORIGIN, PASSWORD, no_page_overflow


# command, digits, raw open/current prices, visible open/current prices, type
ORDERS = [
    (0, 2, "4321.670000000001", "4320.589999999999", "4321.67", "4320.59", "buy"),
    (1, 5, "1.2345700000000002", "1.2000000000000002", "1.23457", "1.20000", "sell"),
    (2, 3, "123.45599999999999", "123.40000000000001", "123.456", "123.400", "buy limit"),
    (3, 0, "4321.670000000001", "4319.2", "4322", "4319", "sell limit"),
    (4, 16, "0.12345678901234567", "0", "0.1234567890123457", "0.0000000000000000", "buy stop"),
    (5, 2, "99.99999999999999", "100", "100.00", "100.00", "sell stop"),
]


def inject_prices(route):
    response = route.fetch()
    detail = response.json()
    template = detail["positions"][0]
    detail["positions"] = [
        dict(template, ticket=90001 + command, symbol=f"SYNTH{digits}", command=command,
             digits=digits, open_price=opened, current_price=current)
        for command, digits, opened, current, *_ in ORDERS
    ]
    route.fulfill(response=response, json=detail)


def check_orders(page, orders):
    expect(page.locator(".positions-table tbody tr")).to_have_count(len(orders))
    for command, _, _, _, opened, current, label in orders:
        row = page.locator(".positions-table tbody tr").filter(has_text=f"#{90001 + command}")
        # Check prices first so the original long-decimal rendering is reproduced.
        expect(row.locator("td").nth(3)).to_have_text(opened)
        expect(row.locator("td").nth(4)).to_have_text(current)
        expect(row.locator("td").nth(1)).to_have_text(label)
        if command > 1:
            expect(row.locator("td").nth(5)).to_have_text("—")


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
        page.wait_for_load_state("networkidle")
        expect(page.locator(".hero-value")).to_contain_text("27,049.11")
        usc = page.locator(".currency-row").filter(has=page.get_by_text("USC", exact=True))
        expect(usc).to_contain_text("827,650.53")
        row = page.locator(".accounts-table tbody tr").filter(has_text="100102")
        expect(row.locator("td").nth(2)).to_have_text("824,500.00")
        expect(row.locator("td").nth(3)).to_have_text("827,650.53")

        page.route(re.compile(r"/api/accounts/[^/?]+(?:\?.*)?$"), inject_prices)
        row.get_by_role("link").first.click()
        page.wait_for_load_state("networkidle")
        expect(page.locator(".unit-label")).to_contain_text("USC")
        expect(page.locator(".detail-metrics .metric-value").first).to_have_text("827,650.53 USC")
        check_orders(page, ORDERS[:2])
        expect(page.get_by_role("columnheader", name="类型", exact=True)).to_be_visible()
        no_page_overflow(page)
        page.screenshot(path=str(ARTIFACTS / "order-types-prices-desktop.png"), full_page=True)

        page.get_by_role("button", name=re.compile("挂单")).click()
        check_orders(page, ORDERS[2:])
        page.get_by_label("订单排序").select_option("open_time_wall")
        check_orders(page, ORDERS[2:])
        page.screenshot(path=str(ARTIFACTS / "pending-types-prices-desktop.png"), full_page=True)

        page.set_viewport_size({"width": 390, "height": 844})
        no_page_overflow(page)
        check_orders(page, ORDERS[2:])
        page.screenshot(path=str(ARTIFACTS / "order-types-prices-mobile.png"), full_page=True)
        page.get_by_role("link", name="账户总览", exact=True).click()
        page.wait_for_load_state("networkidle")
        usc = page.locator(".currency-row").filter(has=page.get_by_text("USC", exact=True))
        expect(usc.locator(".text-right strong")).to_have_text("827,650.53 USC")
        expect(usc.locator(".text-right small")).to_have_text("最近已知净值 · 未换算")
        no_page_overflow(page)
        page.screenshot(path=str(ARTIFACTS / "native-usc-mobile.png"), full_page=True)
        assert not errors, errors
        browser.close()
        print("PASS: native USC, USD subtotal, six order types, symbol price precision, sorting and mobile layout")


if __name__ == "__main__":
    main()
