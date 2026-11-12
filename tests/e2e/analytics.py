"""Financial and UI acceptance against an isolated synthetic analytics server."""

import argparse
from datetime import datetime, timedelta, timezone
from decimal import Decimal
import json
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

from dashboard import PASSWORD, no_page_overflow
from trends import synthetic_server


ROOT = Path(__file__).resolve().parents[2]


def analysis_response(response):
    return "/api/analytics?" in response.url and response.request.method == "GET"


def check_locale(browser, origin, locale, artifacts):
    english = locale == "en"
    context = browser.new_context(viewport={"width": 1440, "height": 1100}, locale=locale, timezone_id="America/New_York")
    context.add_init_script(f"localStorage.setItem('portexa.locale', {json.dumps(locale)});")
    page = context.new_page()
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    page.goto(origin + "/analytics")
    page.wait_for_load_state("networkidle")
    page.get_by_label("System password" if english else "系统密码", exact=True).fill(PASSWORD)
    with page.expect_response(analysis_response, timeout=40_000) as response:
        page.get_by_role("button", name="Sign in securely" if english else "安全登录", exact=True).click()
    body = response.value.json()
    assert response.value.status == 200, body
    result = body["result"]
    assert result["quality"]["reconciliation"] == "passed", result["quality"]
    for field in ("net_profit", "return_pct", "realized", "max_drawdown_pct"):
        assert result["metrics"][field]["value"] is not None, (field, result["quality"])
    metrics = result["metrics"]
    bridge = sum(Decimal(metrics[field]["value"]) for field in ("realized", "floating_change", "other_income", "costs"))
    assert bridge == Decimal(metrics["net_profit"]["value"])
    assert sum(Decimal(account["metrics"]["net_profit"]["value"]) for account in body["accounts"] if account["metrics"]["net_profit"]["value"] is not None) == bridge
    assert Decimal(result["quality"]["balance_residual"]) == 0
    assert len(result["evidence_id"]) == 64
    for month in result["months"]:
        values = [Decimal(day["net_profit"]["value"]) for day in result["days"] if day["date"].startswith(month["month"])]
        assert sum(values) == Decimal(month["net_profit"]["value"])
    expect(page.locator(".performance-plot canvas")).to_be_visible()
    expect(page.locator(".performance-kpis article")).to_have_count(4)
    expect(page.locator("[data-analysis-coverage]")).to_contain_text("not calculation progress" if english else "不是计算进度")
    expect(page.get_by_role("heading", name="Account contribution" if english else "账户贡献", exact=True)).to_be_visible()
    no_page_overflow(page)
    page.screenshot(path=str(artifacts / f"analytics-{locale}-desktop.png"), full_page=True)
    page.screenshot(path=str(artifacts / f"analytics-{locale}-preview.png"), full_page=False)

    # Pick a fully covered day in the calendar's currently displayed month.
    month = result["days"][-1]["date"][:7]
    selected = next(day for day in result["days"] if day["date"].startswith(month) and not day["partial"])
    with page.expect_response("**/api/analytics/activity?*") as activity:
        page.locator(f'[data-calendar-date="{selected["date"]}"]').click()
    activity_body = activity.value.json()
    assert activity.value.status == 200, activity_body
    assert activity_body["total"] == selected["trades"]
    dialog = page.get_by_role("dialog")
    expect(dialog).to_be_visible()
    expect(dialog).to_contain_text("Trades and fund entries" if english else "交易与资金流水")
    dialog.screenshot(path=str(artifacts / f"day-{locale}.png"))
    dialog.get_by_role("button", name="Close" if english else "关闭", exact=True).click()

    page.get_by_role("button", name="Cumulative net profit" if english else "累计净收益", exact=True).click()
    box = page.locator(".performance-plot").bounding_box()
    page.mouse.click(box["x"] + box["width"] * 0.65, box["y"] + 100)
    expect(page.locator(".performance-selected")).to_be_visible()
    page.get_by_role("button", name="Cumulative return" if english else "累计收益率", exact=True).click()
    page.locator(".performance-kpi-explanation summary").last.click()
    expect(page.locator(".performance-kpi-explanation").last).to_contain_text("Peak" if english else "峰值")

    with page.expect_response(analysis_response) as response:
        page.get_by_role("button", name="7D", exact=True).click()
    seven = response.value.json()
    assert seven["requested_to_ms"] - seven["requested_from_ms"] == 7 * 86_400_000
    expect(page.get_by_role("button", name="7D", exact=True)).to_have_attribute("aria-pressed", "true")

    # Fix a range before comparing native and converted money.
    page.get_by_role("button", name="Custom" if english else "自定义", exact=True).click()
    end = datetime.now(timezone(timedelta(hours=8))).replace(second=0, microsecond=0) - timedelta(minutes=10)
    start = end - timedelta(days=1)
    page.get_by_label("Start time (Beijing)" if english else "开始时间（北京时间）", exact=True).fill(start.strftime("%Y-%m-%dT%H:%M"))
    page.get_by_label("End time (Beijing)" if english else "结束时间（北京时间）", exact=True).fill(end.strftime("%Y-%m-%dT%H:%M"))
    with page.expect_response(analysis_response):
        page.get_by_role("button", name="Apply time range" if english else "应用时间范围", exact=True).click()
    options = page.locator("#analytics-account option").evaluate_all("items => items.map(item => ({value:item.value,text:item.textContent}))")
    usc = next(item["value"] for item in options if "100102" in item["text"])
    with page.expect_response(analysis_response) as response:
        page.locator("#analytics-account").select_option(usc)
    converted = response.value.json()["result"]
    with page.expect_response(analysis_response) as response:
        page.get_by_label("Analysis display unit" if english else "分析显示单位", exact=True).select_option("native")
    native = response.value.json()["result"]
    assert native["currency"] == "USC"
    assert Decimal(native["metrics"]["net_profit"]["value"]) / 100 == Decimal(converted["metrics"]["net_profit"]["value"])
    assert native["metrics"]["return_pct"]["value"] == converted["metrics"]["return_pct"]["value"]
    page.reload()
    expect(page.get_by_label("Analysis display unit" if english else "分析显示单位", exact=True)).to_have_value("native")
    expect(page.locator(".performance-plot canvas")).to_be_visible()

    # A pending account displays nulls, including unverified trade counts.
    pending = next(item["value"] for item in options if "100105" in item["text"])
    with page.expect_response(analysis_response) as response:
        page.locator("#analytics-account").select_option(pending)
    missing = response.value.json()["result"]
    assert missing["metrics"]["net_profit"]["value"] is None
    assert all(day["trades"] is None for day in missing["days"])
    expect(page.locator('[data-kpi="net"]')).to_contain_text("—")
    expect(page.locator(".performance-plot-empty")).to_be_visible()
    expect(page.locator("[data-analysis-reasons]")).to_contain_text("Calculation complete" if english else "计算已完成")
    expect(page.locator("[data-analysis-reasons]")).to_contain_text("No valid beginning valuation" if english else "缺少有效期初估值")
    page.screenshot(path=str(artifacts / f"missing-evidence-{locale}.png"), full_page=True)
    for width in (390, 320):
        page.set_viewport_size({"width": width, "height": 844})
        no_page_overflow(page)
        page.screenshot(path=str(artifacts / f"missing-evidence-{locale}-{width}.png"), full_page=True)
    page.set_viewport_size({"width": 1440, "height": 1100})

    with page.expect_response(analysis_response):
        page.locator("#analytics-account").select_option("")
    with page.expect_response(analysis_response):
        page.get_by_role("button", name="30D", exact=True).click()
    expect(page.locator(".performance-plot-empty")).to_have_count(0)
    for width in (390, 320):
        page.set_viewport_size({"width": width, "height": 844})
        expect(page.locator(".performance-plot canvas")).to_be_visible()
        page.screenshot(path=str(artifacts / f"analytics-{locale}-{width}.png"), full_page=True)
        no_page_overflow(page)

    def fail_analysis(route):
        route.fulfill(status=503, json={"error": "Synthetic analysis unavailable"})

    page.route("**/api/analytics?*", fail_analysis)
    page.get_by_role("button", name="Refresh analysis" if english else "刷新分析", exact=True).click()
    expect(page.get_by_role("alert")).to_contain_text("Synthetic analysis unavailable")
    page.unroute("**/api/analytics?*", fail_analysis)
    page.get_by_role("button", name="Retry" if english else "重试", exact=True).click()
    expect(page.get_by_role("alert")).to_have_count(0)
    assert not errors, errors
    context.close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--server", type=Path, default=ROOT / ".local/bin/e2e-server")
    parser.add_argument("--web", type=Path, default=ROOT / ".local/web")
    parser.add_argument("--artifacts", type=Path, default=ROOT / ".local/browser/analytics")
    args = parser.parse_args()
    args.artifacts.mkdir(parents=True, exist_ok=True)
    # Seeding 31 days across three accounts writes over 130,000 synthetic
    # snapshots before HTTP starts; measured startup can exceed 40 seconds.
    with synthetic_server(args.server.resolve(), args.web.resolve(), flags=("-analytics",), startup_timeout=90) as origin, sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        try:
            for locale in ("zh-CN", "en"):
                check_locale(browser, origin, locale, args.artifacts)
        finally:
            browser.close()
    print("PASS: reconciled KPI/bridge/months, curves, calendar activity, USD/USC, URL state, nulls, recovery, bilingual desktop/390/320; synthetic data only")


if __name__ == "__main__":
    main()
