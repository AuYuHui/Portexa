"""UI-only API fixtures; financial aggregation is covered by Go tests."""

from copy import deepcopy
from urllib.parse import parse_qs, urlparse

from playwright.sync_api import expect

from dashboard import no_page_overflow


def check_realized_calendar(page, original, english, artifacts):
    body = deepcopy(original)
    result = body["result"]
    complete = [day for day in result["days"] if not day["partial"]]
    months = sorted({day["date"][:7] for day in complete}, reverse=True)
    month = next(m for m in months if sum(day["date"].startswith(m) for day in complete) >= 3)
    days = [day for day in complete if day["date"].startswith(month)][-3:]
    known = lambda value: {"value": value, "reasons": []}
    missing = {"value": None, "reasons": ["missing_end", "boundary_event"]}
    for day, realized, floating, net in zip(days, ("219.87", "-103.21", "0"), (None, "140", None), (None, "36.79", None)):
        day["realized"] = known(realized)
        day["floating_change"] = known(floating) if floating is not None else deepcopy(missing)
        day["net_profit"] = known(net) if net is not None else deepcopy(missing)
        day["trades"] = 1 if realized != "0" else 0
    result["days"] = days
    result["months"] = [{"month": month, "realized": known("116.66"), "floating_change": deepcopy(missing), "net_profit": deepcopy(missing), "partial": True}]

    def respond(route):
        route.fulfill(json=body)

    def orders(route):
        query = parse_qs(urlparse(route.request.url).query)
        assert query["kind"] == ["trades"]
        day = next(day for day in days if day["from_ms"] == int(query["from_ms"][0]))
        amount = day["realized"]["value"]
        rows = []
        if amount not in (None, "0"):
            rows.append({
                "account_id": body["accounts"][0]["account_id"], "time_ms": day["from_ms"] + 61_000,
                "symbol": "TESTUSD", "amount": amount, "kind": "trade",
                "order": {"order_ticket": "9007199254740993", "deal_ticket": "9007199254741001", "side": "buy", "lots": "0.12"},
            })
        route.fulfill(json={"items": rows, "total": len(rows), "offset": 0, "currency": result["currency"]})

    page.route("**/api/analytics?*", respond)
    page.route("**/api/analytics/activity?*", orders)
    refresh = page.get_by_role("button", name="Refresh analysis" if english else "刷新分析", exact=True)
    with page.expect_response("**/api/analytics?*"):
        refresh.click()
    positive, negative, flat = [page.locator(f'[data-calendar-date="{day["date"]}"]') for day in days]
    expect(positive.locator("[data-calendar-realized]")).to_have_text("+219.87")
    expect(positive).to_have_class("performance-calendar-day is-positive")
    expect(negative.locator("[data-calendar-realized]")).to_have_text("−103.21")
    expect(negative).to_have_class("performance-calendar-day is-negative")
    expect(flat.locator("[data-calendar-realized]")).to_have_text("0.00")
    expect(flat).to_have_class("performance-calendar-day is-flat")
    expect(page.locator("[data-calendar-month-realized]")).to_contain_text("+116.66")

    expect(page.locator(".performance-calendar")).not_to_contain_text("Float change" if english else "浮盈变化")
    locale = "en" if english else "zh-CN"
    for width in (1440, 390, 320):
        page.set_viewport_size({"width": width, "height": 1100 if width == 1440 else 844})
        no_page_overflow(page)
        expect(positive.locator("[data-calendar-realized]")).to_be_visible()
        for amount in (positive.locator("[data-calendar-realized]"), negative.locator("[data-calendar-realized]")):
            assert amount.evaluate("node => { const range = document.createRange(); range.selectNodeContents(node); return new Set([...range.getClientRects()].filter(rect => rect.width > 0).map(rect => rect.top)).size; }") == 1, "Calendar amount wrapped across lines"
        page.locator(".performance-calendar").screenshot(path=str(artifacts / f"realized-calendar-{locale}-{width}.png"))
        positive.click()
        dialog = page.get_by_role("dialog")
        expect(dialog.locator('[data-day-metric="realized"] dd')).to_contain_text("+219.87")
        expect(dialog.locator('[data-day-metric="realized"] dt')).to_have_text("Profit" if english else "盈利")
        expect(dialog.locator("tbody tr").first).to_contain_text("9007199254740993")
        expect(dialog.locator("tbody tr").first).to_contain_text("+219.87")
        expect(dialog).not_to_contain_text("valuation" if english else "估值")
        assert dialog.evaluate("node => node.scrollWidth <= node.clientWidth")
        no_page_overflow(page)
        dialog.screenshot(path=str(artifacts / f"realized-day-{locale}-{width}.png"))
        dialog.get_by_role("button", name="Close" if english else "关闭", exact=True).click()

    for cell, amount, outcome in ((negative, "−103.21", "Loss" if english else "亏损"), (flat, "0.00", "Flat" if english else "持平")):
        cell.click()
        expect(dialog.locator('[data-day-metric="realized"] dt')).to_have_text(outcome)
        expect(dialog.locator('[data-day-metric="realized"] dd')).to_contain_text(amount)
        if cell == flat:
            expect(dialog).to_contain_text("No trades in this period." if english else "该时段没有交易订单。")
        dialog.get_by_role("button", name="Close" if english else "关闭", exact=True).click()

    # Missing ledger evidence remains unknown, independently of valuation gaps.
    days[2]["realized"] = {"value": None, "reasons": ["history_incomplete"]}
    days[2]["trades"] = None
    result["months"][0]["realized"] = deepcopy(days[2]["realized"])
    with page.expect_response("**/api/analytics?*"):
        refresh.click()
    expect(flat).to_have_class("performance-calendar-day is-missing")
    expect(flat.locator("[data-calendar-realized]")).to_have_text("Pending" if english else "待确认")
    expect(page.locator("[data-calendar-month-realized]")).to_have_text("Pending" if english else "待确认")
    flat.click()
    expect(dialog.locator('[data-day-metric="realized"] dd')).to_have_text("Pending" if english else "待确认")
    expect(dialog).to_contain_text("No verified trades" if english else "暂无可确认的交易订单")
    dialog.get_by_role("button", name="Close" if english else "关闭", exact=True).click()
    page.unroute("**/api/analytics?*", respond)
    page.unroute("**/api/analytics/activity?*", orders)
    page.set_viewport_size({"width": 1440, "height": 1100})
