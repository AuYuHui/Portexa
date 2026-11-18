"""UI-only API fixtures; financial aggregation is covered by Go tests."""

from copy import deepcopy

from playwright.sync_api import expect

from dashboard import no_page_overflow


def check_realized_calendar(page, original, english, artifacts):
    body = deepcopy(original)
    result = body["result"]
    month = result["days"][-1]["date"][:7]
    days = [day for day in result["days"] if day["date"].startswith(month) and not day["partial"]][:3]
    assert len(days) == 3
    known = lambda value: {"value": value, "reasons": []}
    missing = {"value": None, "reasons": ["missing_end", "boundary_event"]}
    for day, realized, floating, net in zip(days, ("19.87", "-3.21", "0"), (None, "40", None), (None, "36.79", None)):
        day["realized"] = known(realized)
        day["floating_change"] = known(floating) if floating is not None else deepcopy(missing)
        day["net_profit"] = known(net) if net is not None else deepcopy(missing)
        day["trades"] = 1 if realized != "0" else 0
    result["days"] = days
    result["months"] = [{"month": month, "realized": known("16.66"), "floating_change": deepcopy(missing), "net_profit": deepcopy(missing), "partial": True}]

    def respond(route):
        route.fulfill(json=body)

    page.route("**/api/analytics?*", respond)
    refresh = page.get_by_role("button", name="Refresh analysis" if english else "刷新分析", exact=True)
    with page.expect_response("**/api/analytics?*"):
        refresh.click()
    positive, negative, flat = [page.locator(f'[data-calendar-date="{day["date"]}"]') for day in days]
    expect(positive.locator("[data-calendar-realized]")).to_have_text("+19.87")
    expect(positive).to_have_class("performance-calendar-day is-positive")
    expect(positive.locator("[data-calendar-reason]")).to_have_count(0)
    expect(positive.locator("[data-calendar-floating]")).to_contain_text("—")
    expect(negative.locator("[data-calendar-realized]")).to_have_text("−3.21")
    expect(negative).to_have_class("performance-calendar-day is-negative")
    expect(negative.locator("[data-calendar-floating]")).to_contain_text("+40.00")
    expect(flat.locator("[data-calendar-realized]")).to_have_text("0.00")
    expect(flat).to_have_class("performance-calendar-day is-flat")
    expect(page.locator("[data-calendar-month-realized]")).to_contain_text("+16.66")
    expect(page.locator("[data-calendar-month-floating]")).to_contain_text("—")

    locale = "en" if english else "zh-CN"
    for width in (1440, 390, 320):
        page.set_viewport_size({"width": width, "height": 1100 if width == 1440 else 844})
        no_page_overflow(page)
        expect(positive.locator("[data-calendar-realized]")).to_be_visible()
        expect(positive.locator("[data-calendar-floating]")).to_be_visible()
        for amount in (positive.locator("[data-calendar-realized]"), negative.locator("[data-calendar-realized]"), negative.locator("[data-calendar-floating] .num")):
            assert amount.evaluate("node => { const range = document.createRange(); range.selectNodeContents(node); return new Set([...range.getClientRects()].filter(rect => rect.width > 0).map(rect => rect.top)).size; }") == 1, "Calendar amount wrapped across lines"
        page.locator(".performance-calendar").screenshot(path=str(artifacts / f"realized-calendar-{locale}-{width}.png"))
        positive.click()
        dialog = page.get_by_role("dialog")
        expect(dialog.locator('[data-day-metric="realized"] dd')).to_contain_text("+19.87")
        expect(dialog.locator('[data-day-metric="floating"] dd')).to_contain_text("—")
        expect(dialog.locator('[data-day-metric="net"] dd')).to_contain_text("—")
        expect(dialog.locator('[data-day-metric="floating"]')).to_contain_text("No valid ending valuation" if english else "缺少有效期末估值")
        no_page_overflow(page)
        dialog.screenshot(path=str(artifacts / f"realized-day-{locale}-{width}.png"))
        dialog.get_by_role("button", name="Close" if english else "关闭", exact=True).click()

    # Missing ledger evidence remains unknown, independently of valuation gaps.
    days[2]["realized"] = {"value": None, "reasons": ["history_incomplete"]}
    days[2]["trades"] = None
    result["months"][0]["realized"] = deepcopy(days[2]["realized"])
    with page.expect_response("**/api/analytics?*"):
        refresh.click()
    expect(flat).to_have_class("performance-calendar-day is-missing")
    expect(flat.locator("[data-calendar-realized]")).to_have_text("—")
    expect(page.locator("[data-calendar-month-realized]")).to_contain_text("—")
    page.unroute("**/api/analytics?*", respond)
    page.set_viewport_size({"width": 1440, "height": 1100})
