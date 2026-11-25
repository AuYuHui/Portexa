"""Standalone order pages on a local synthetic MT4 fixture."""

from datetime import datetime, timedelta, timezone
from decimal import Decimal
from pathlib import Path
from urllib.parse import parse_qs, urlencode, urlparse

from playwright.sync_api import expect, sync_playwright

from dashboard import PASSWORD, no_page_overflow
from trends import synthetic_server


ROOT = Path(__file__).resolve().parents[2]
ARTIFACTS = ROOT / ".local/browser/order-pages"


def history_response(response):
    return "/api/accounts/" in response.url and "/order-history" in response.url


def check_order_pages(browser, origin, english):
    locale = "en" if english else "zh-CN"
    context = browser.new_context(locale=locale, timezone_id="America/New_York", viewport={"width": 1440, "height": 980})
    page = context.new_page()
    issues = []
    page.on("pageerror", lambda err: issues.append(str(err)))
    page.goto(origin)
    page.get_by_label("System password" if english else "系统密码", exact=True).fill(PASSWORD)
    page.get_by_role("button", name="Sign in securely" if english else "安全登录", exact=True).click()
    expect(page.locator(".accounts-table tbody tr")).to_have_count(5)
    accounts = {a["login"]: a for a in context.request.get(origin + "/api/accounts").json()}
    account = accounts[100101]["id"]
    # An account omitted from the analytics seed retains its original open position.
    page.goto(origin + "/accounts/" + accounts[100103]["id"])
    page.get_by_role("link", name="View live positions" if english else "查看实时持仓", exact=True).click()
    expect(page.locator("[data-live-position]")).to_have_count(1)
    expect(page.locator("[data-live-position]")).to_contain_text("EURUSD")
    expect(page.locator("[data-live-total]")).to_contain_text("55.20")
    expect(page.locator("[data-live-observed]")).to_be_visible()
    for width in (390, 320):
        page.set_viewport_size({"width": width, "height": 844})
        no_page_overflow(page)
    page.screenshot(path=str(ARTIFACTS / f"positions-{locale}-320.png"))
    page.set_viewport_size({"width": 1440, "height": 980})
    beijing = timezone(timedelta(hours=8))
    today = datetime.fromtimestamp(accounts[100101]["snapshot"]["observed_to_ms"] / 1000, beijing).date()
    # Beijing has crossed midnight while the browser in New York is still on yesterday.
    page.clock.set_fixed_time(datetime(today.year, today.month, today.day, 0, 5, tzinfo=beijing))
    with page.expect_response(history_response) as response:
        page.get_by_role("navigation").get_by_role("link", name="Order history" if english else "订单历史", exact=True).click()
    expect(page.locator(".order-account-select select")).to_have_value(accounts[100103]["id"])
    params = parse_qs(urlparse(response.value.url).query)
    assert params["from"] == params["to"] == [today.isoformat()]
    expect(page.locator("[data-history-filter=from]")).to_have_value(today.isoformat())
    expect(page.locator("[data-history-filter=to]")).to_have_value(today.isoformat())
    all_dates = page.get_by_role("button", name="All history" if english else "全部历史", exact=True)
    with page.expect_response(history_response):
        all_dates.click()
    with page.expect_response(history_response) as response:
        page.reload()
    params = parse_qs(urlparse(response.value.url).query)
    assert "from" not in params and "to" not in params
    expect(all_dates).to_have_attribute("aria-pressed", "true")
    with page.expect_response(history_response) as response:
        page.get_by_role("button", name="Reset filters" if english else "重置筛选", exact=True).click()
    params = parse_qs(urlparse(response.value.url).query)
    assert params["from"] == params["to"] == [today.isoformat()]
    expect(page.get_by_role("button", name="Today" if english else "今天", exact=True)).to_have_attribute("aria-pressed", "true")

    query = {"account": account, "from": (today - timedelta(days=28)).isoformat(), "to": (today - timedelta(days=1)).isoformat()}
    with page.expect_response(history_response) as response:
        page.goto(origin + "/order-history?" + urlencode(query))
    assert response.value.ok, response.value.text()
    first = response.value.json()
    assert 100 < first["total"] <= 150 and first["page_size"] == 50 and first["complete"], first
    rows = page.locator("[data-history-row]")
    expect(rows).to_have_count(50)
    expect(page.locator(".history-results > :first-child").get_by_role("table")).to_be_visible()
    rows.first.get_by_role("button").click()
    dialog = page.get_by_role("dialog")
    expect(dialog).to_contain_text(first["items"][0]["order_ticket"])
    dialog.get_by_role("button", name="Close" if english else "关闭", exact=True).click()
    with page.expect_response(history_response) as response:
        page.locator("[data-history-next]").click()
    second = response.value.json()
    assert second["page"] == 2
    expect(rows).to_have_count(50)
    expect(page.locator(".history-results")).to_be_focused()
    expect(rows.first).to_be_in_viewport()
    page.locator("[data-history-page]").fill("3")
    with page.expect_response(history_response) as response:
        page.locator("[data-history-jump]").click()
    third = response.value.json()
    assert third["page"] == 3
    expect(rows).to_have_count(first["total"] - 100)
    assert first["net_profit"] == second["net_profit"] == third["net_profit"]
    items = first["items"] + second["items"] + third["items"]
    assert len({i["order_ticket"] for i in items}) == first["total"]
    assert sum(Decimal(i["net_profit"]) for i in items) == Decimal(first["net_profit"])
    assert all(a["close_time_ms"] >= b["close_time_ms"] for a, b in zip(items, items[1:]))

    page.locator("[data-history-filter=result]").select_option("loss")
    with page.expect_response(history_response) as response:
        page.locator("[data-history-apply]").click()
    filtered = response.value.json()
    assert filtered["page"] == 1 and 0 < filtered["total"] < 50
    assert all(Decimal(item["net_profit"]) < 0 for item in filtered["items"])
    expect(rows).to_have_count(filtered["total"])
    page.screenshot(path=str(ARTIFACTS / f"history-{locale}.png"))
    for width in (390, 320):
        page.set_viewport_size({"width": width, "height": 844})
        no_page_overflow(page)
        expect(rows.first).to_be_visible()
        rows.first.get_by_role("button").click()
        expect(dialog).to_contain_text(filtered["items"][0]["order_ticket"])
        dialog.get_by_role("button", name="Close" if english else "关闭", exact=True).click()
    page.screenshot(path=str(ARTIFACTS / f"history-{locale}-320.png"))
    assert not issues, issues
    context.close()


if __name__ == "__main__":
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    with synthetic_server(ROOT / ".local/bin/e2e-server", ROOT / ".local/web", ("-analytics",), startup_timeout=90) as origin, sync_playwright() as pw:
        browser = pw.chromium.launch()
        try:
            for english in (False, True):
                check_order_pages(browser, origin, english)
        finally:
            browser.close()
    print("PASS: MT4 positions, account navigation, three 50-row history pages, query totals, filters, bilingual desktop and mobile")
