"""Same-page chart controls preserve scroll; route changes retain navigation semantics."""

import argparse
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

from dashboard import PASSWORD, no_page_overflow
from trends import ROOT, synthetic_server


def keep_position(page, region, action, query_key, query_value):
    page.wait_for_load_state("networkidle")
    toolbar = region.locator(".trend-toolbar")
    toolbar.evaluate("(element) => window.scrollTo(0, window.scrollY + element.getBoundingClientRect().top - 120)")
    page.wait_for_timeout(50)
    before = page.evaluate("window.scrollY")
    assert before > 100, before
    page.evaluate("""() => {
      window.scrollProbe = { samples: [], done: false };
      const start = performance.now();
      function sample() {
        window.scrollProbe.samples.push(window.scrollY);
        if (performance.now() - start < 600) requestAnimationFrame(sample);
        else window.scrollProbe.done = true;
      }
      requestAnimationFrame(sample);
    }""")
    action()
    page.wait_for_function(
        "([key, value]) => new URL(location.href).searchParams.get(key) === value",
        arg=[query_key, query_value],
    )
    expect(region).to_have_attribute("aria-busy", "false")
    page.wait_for_function("() => window.scrollProbe.done")
    after = page.evaluate("window.scrollY")
    minimum = page.evaluate("Math.min(...window.scrollProbe.samples)")
    print(f"{query_key}={query_value}: before={before:.1f}, after={after:.1f}, minimum={minimum:.1f}", flush=True)
    assert abs(after - before) <= 2 and minimum >= before - 2, "Same-page controls moved the page"


def check_viewport(browser, origin, width, artifacts):
    context = browser.new_context(viewport={"width": width, "height": 844}, locale="zh-CN")
    page = context.new_page()
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    try:
        page.goto(origin)
        page.wait_for_load_state("networkidle")
        page.get_by_label("系统密码", exact=True).fill(PASSWORD)
        page.get_by_role("button", name="安全登录", exact=True).click()
        overview = page.get_by_role("region", name="资产走势", exact=True)
        expect(overview.locator("canvas")).to_be_visible()
        keep_position(page, overview, lambda: overview.get_by_role("button", name="30D", exact=True).click(), "trange", "30d")
        keep_position(page, overview, lambda: overview.get_by_role("button", name="余额", exact=True).click(), "tmetric", "balance")
        row = page.locator(".accounts-table tbody tr").filter(has_text="100102")
        row.get_by_role("link").first.click()
        account = page.get_by_role("region", name="账户资金历史", exact=True)
        expect(account.locator("canvas")).to_be_visible()
        page.wait_for_function("() => window.scrollY === 0")
        keep_position(page, account, lambda: account.get_by_role("button", name="30D", exact=True).click(), "trange", "30d")
        keep_position(page, account, lambda: account.get_by_role("button", name="净值", exact=True).click(), "tmetric", "equity")
        keep_position(page, account, lambda: account.get_by_label("走势图显示单位", exact=True).select_option("USD"), "tunit", "USD")
        keep_position(page, account, lambda: account.get_by_role("button", name="自定义", exact=True).click(), "trange", "custom")
        keep_position(page, account, lambda: account.get_by_role("button", name="7D", exact=True).click(), "trange", "7d")
        previous = page.evaluate("window.scrollY")
        account_path = page.url
        page.get_by_role("link", name="接入与设置", exact=True).click()
        page.wait_for_function("() => location.pathname === '/settings' && window.scrollY === 0")
        page.go_back()
        page.wait_for_url(account_path)
        expect(account.locator("canvas")).to_be_visible()
        page.wait_for_function("(position) => Math.abs(window.scrollY - position) <= 2", arg=previous)
        no_page_overflow(page)
        page.screenshot(path=str(artifacts / f"scroll-{width}.png"), full_page=True)
        assert not errors, errors
    finally:
        context.close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--server", type=Path, default=ROOT / ".local/bin/e2e-server")
    parser.add_argument("--web", type=Path, default=ROOT / ".local/web")
    parser.add_argument("--artifacts", type=Path, default=ROOT / ".local/browser/trend-scroll")
    args = parser.parse_args()
    args.artifacts.mkdir(parents=True, exist_ok=True)
    with synthetic_server(args.server.resolve(), args.web.resolve(), ("-automatic-time",)) as origin, sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        try:
            for width in (1440, 390):
                check_viewport(browser, origin, width, args.artifacts)
        finally:
            browser.close()
    print("PASS: chart query controls preserve scroll; cross-page navigation resets and browser Back restores position")


if __name__ == "__main__":
    main()
