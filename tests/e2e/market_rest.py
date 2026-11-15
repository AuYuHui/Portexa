"""Weekend policy/settings and certified valuations using synthetic data only."""

from pathlib import Path
from urllib.parse import urlencode

from playwright.sync_api import expect, sync_playwright

from dashboard import PASSWORD, no_page_overflow
from trends import synthetic_server

ROOT = Path(__file__).resolve().parents[2]


def check_locale(browser, origin, locale, artifacts):
    english = locale == "en"
    context = browser.new_context(viewport={"width": 1440, "height": 1100}, locale=locale)
    context.add_init_script(f"localStorage.setItem('portexa.locale', '{locale}');")
    page = context.new_page()
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    page.goto(origin)
    page.wait_for_load_state("networkidle")
    page.get_by_label("System password" if english else "系统密码", exact=True).fill(PASSWORD)
    page.get_by_role("button", name="Sign in securely" if english else "安全登录", exact=True).click()
    expect(page.get_by_role("link", name="Connections & settings" if english else "接入与设置", exact=True)).to_be_visible()
    response = context.request.get(origin + "/api/accounts")
    assert response.status == 200
    account = next(item for item in response.json() if item["label"] == "Market rest synthetic")
    end = account["snapshot"]["observed_to_ms"]
    start = end - 2 * 60 * 60 * 1000

    page.get_by_role("link", name="Connections & settings" if english else "接入与设置", exact=True).click()
    region = page.get_by_role("region", name="Collection settings" if english else "采集设置")
    template = region.get_by_role("combobox", name="Built-in market rest template" if english else "内置休市模板", exact=True)
    save = region.get_by_role("button", name="Save collection settings" if english else "保存采集设置", exact=True)
    expect(template).to_have_value("fx_metals_weekend_v1")
    learning = region.get_by_label("Learn weekend market rest hours automatically" if english else "自动学习周末休市时段", exact=True)
    expect(learning).to_be_checked()
    groups = context.request.get(origin + "/api/market-rest/learning")
    assert groups.status == 200 and all(item["valid_weekends"] == 0 for item in groups.json())
    assert all(item.get("active") is None for item in groups.json())
    expect(region.locator("[data-rest-learning]")).to_be_visible()
    # Render an adopted model in the real component without pretending the
    # two-hour HTTP fixture has four weeks of evidence. Backend adoption is
    # covered by TestRestLearningAdoptionSchedulingFinancialProofAndConflict.
    model = {"id": "synthetic-model", "close_minute": 1050, "open_minute": 3870, "expires_at_ms": 1}
    payload = [{"id": "synthetic-learning-view", "server_id": "synthetic", "server_name": "Synthetic learning", "symbol": "XAUUSD.c", "state": "adopted", "reason": "validated", "valid_weekends": 4, "candidate": model, "active": model}]
    def model_view(route):
        route.fulfill(json=payload)
    page.route("**/api/market-rest/learning", model_view)
    page.reload()
    status = region.locator("[data-rest-learning]")
    expect(status).to_contain_text("Adopted" if english else "已采用")
    expect(status).to_contain_text("4/4")
    expect(status).to_contain_text("America/New_York")
    expect(status).to_contain_text("17:30")
    for width in (1440, 390, 320):
        page.set_viewport_size({"width": width, "height": 1100 if width == 1440 else 844})
        no_page_overflow(page)
        status.screenshot(path=str(artifacts / f"learning-{locale}-{width}.png"))
    page.unroute("**/api/market-rest/learning", model_view)
    page.set_viewport_size({"width": 1440, "height": 1100})
    page.reload()
    expect(learning).to_be_checked()
    learning.uncheck()
    save.click()
    expect(save).to_be_disabled()
    assert context.request.get(origin + "/api/settings/collection").json()["market_rest_learning_enabled"] is False
    learning.check()
    save.click()
    expect(save).to_be_disabled()
    assert context.request.get(origin + "/api/settings/collection").json()["market_rest_learning_enabled"] is True
    template.select_option("off")
    save.click()
    expect(region.get_by_role("status")).to_be_visible()
    assert context.request.get(origin + "/api/settings/collection").json()["market_rest_template"] == "off"
    template.select_option("fx_metals_weekend_v1")
    region.locator("details summary").click()
    exception = region.get_by_label("Market rest synthetic", exact=True)
    exception.check()
    save.click()
    expect(region.get_by_role("status")).to_be_visible()
    expect(save).to_be_disabled()
    assert account["id"] in context.request.get(origin + "/api/settings/collection").json()["market_rest_excluded_account_ids"]
    page.reload()
    expect(template).to_have_value("fx_metals_weekend_v1")
    region.locator("details summary").click()
    expect(exception).to_be_checked()
    exception.uncheck()
    save.click()
    expect(region.get_by_role("status")).to_be_visible()
    expect(save).to_be_disabled()
    region.screenshot(path=str(artifacts / f"settings-{locale}.png"))
    for width in (390, 320):
        page.set_viewport_size({"width": width, "height": 844})
        no_page_overflow(page)
        expect(learning).to_be_visible()
        region.screenshot(path=str(artifacts / f"settings-{locale}-{width}.png"))
    page.set_viewport_size({"width": 1440, "height": 1100})

    query = urlencode({"range": "custom", "account": account["id"], "unit": "USD", "from": start, "to": end})
    with page.expect_response("**/api/analytics?*") as result:
        page.goto(origin + "/analytics?" + query)
    report = result.value.json()
    assert result.value.status == 200, report
    data = report["result"]
    assert data["quality"]["rest_carried_points"] > 0, data["quality"]
    assert data["quality"]["valid_points"] == data["quality"]["expected_points"]
    assert data["metrics"]["net_profit"]["value"] == "0"
    assert data["metrics"]["return_pct"]["value"] == "0"
    expect(page.locator("[data-rest-evidence]")).to_be_visible()
    day = data["days"][0]
    page.locator(f'[data-calendar-date="{day["date"]}"]').click()
    dialog = page.get_by_role("dialog")
    expect(dialog.locator("[data-rest-evidence]")).to_be_visible()
    expect(dialog.locator(".performance-breakdown > div").first.locator("dd")).to_contain_text("0.00")
    dialog.screenshot(path=str(artifacts / f"day-{locale}.png"))
    for width in (390, 320):
        page.set_viewport_size({"width": width, "height": 844})
        no_page_overflow(page)
        expect(dialog.locator("[data-rest-evidence]")).to_be_visible()
        dialog.screenshot(path=str(artifacts / f"day-{locale}-{width}.png"))
    dialog.get_by_role("button", name="Close" if english else "关闭", exact=True).click()
    no_page_overflow(page)
    trend = context.request.get(origin + f"/api/portfolio/equity-history?from_ms={start}&to_ms={end}")
    assert trend.status == 200, trend.text()
    assert trend.json()["rest_carried_points"] > 0 and trend.json()["gap_points"] == 0
    assert not errors, errors
    context.close()


def main():
    # Keep the server origin scoped to this run and always close browser/server.
    artifacts = ROOT / ".local/browser/market-rest"
    artifacts.mkdir(parents=True, exist_ok=True)
    with synthetic_server(ROOT / ".local/bin/e2e-server", ROOT / ".local/web", flags=("-market-rest",)) as origin, sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        try:
            for locale in ("zh-CN", "en"):
                check_locale(browser, origin, locale, artifacts)
        finally:
            browser.close()
    print("PASS: built-in policy/exceptions persistence, historical proof survives changes, certified net/return/trend, bilingual desktop/390/320; synthetic only")


if __name__ == "__main__":
    main()
