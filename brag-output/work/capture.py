"""Drive the real demo console through the walkthrough and capture each step.

Saves shots/<name>.png (full page, 2x) and shots/<name>.json (element rects in
CSS px, document coordinates). Passwords are read from demo.log, never printed.
"""
import json, re, pathlib, sys
from playwright.sync_api import sync_playwright

W = pathlib.Path(__file__).parent
BASE = "http://127.0.0.1:18080"
OUT = W / "shots"; OUT.mkdir(exist_ok=True)
EV = W / "ev"
log = (W / "demo.log").read_text(encoding="utf-8", errors="ignore")
pw = dict(re.findall(r"^\s+(\S+@demo\.invalid)\s+\S+\s+password:\s+(\S+)", log, re.M))
assert "owner@demo.invalid" in pw, "no passwords in demo.log"
sums = dict((n, h) for h, n in re.findall(r"^([0-9a-f]{64})\s+(\S+)$", (EV / "SHA256SUMS").read_text(), re.M))

RECT_JS = """sels => { const o = {}; for (const [k, s] of Object.entries(sels)) {
  let el; try { el = typeof s === 'string' ? document.querySelector(s) : null; } catch (e) {}
  if (!el && s && s.text) { el = [...document.querySelectorAll(s.tag || '*')].find(e => e.textContent.trim().startsWith(s.text)); }
  if (el) { const r = el.getBoundingClientRect(); o[k] = [r.left + scrollX, r.top + scrollY, r.width, r.height]; } }
  o.__page = [document.documentElement.scrollWidth, document.documentElement.scrollHeight]; return o; }"""

MASK_JS = """from => { const w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  let n; while ((n = w.nextNode())) if (n.nodeValue.includes(from)) n.nodeValue = n.nodeValue.split(from).join('file:///demo/evidence/');
  for (const i of document.querySelectorAll('input')) if (i.value.includes(from)) i.value = i.value.split(from).join('file:///demo/evidence/'); }"""

def shot(page, name, sels=None):
    page.wait_for_load_state("networkidle")
    page.evaluate(MASK_JS, EV.resolve().as_uri() + "/")
    rects = page.evaluate(RECT_JS, sels or {})
    page.screenshot(path=str(OUT / f"{name}.png"), full_page=True)
    (OUT / f"{name}.json").write_text(json.dumps(rects, indent=1))
    print("shot", name, rects.get("__page"))

def signin(page, email, name=None):
    page.goto(BASE + "/console/signin")
    if page.locator("#tenant").count():
        page.fill("#tenant", "demo")
    page.fill("#email", email)
    page.fill("#password", pw[email])
    if name:
        page.fill("#password", "x" * 14)  # never show the real one on screen
        shot(page, name, {"email": "#email", "password": "#password", "button": "button[type=submit]", "card": "form.card"})
        page.fill("#password", pw[email])
    page.click("button[type=submit]")
    page.wait_for_load_state("networkidle")
    if page.locator("form[action='/console/workspace'] button").count():
        page.locator("form[action='/console/workspace'] button").first.click()

CTRL = "/console/controls/dora/dora-roi-provider-identification"

with sync_playwright() as p:
    b = p.chromium.launch(channel="chrome")
    ctx = b.new_context(viewport={"width": 1440, "height": 900}, device_scale_factor=2)
    page = ctx.new_page()

    signin(page, "owner@demo.invalid", "01-signin")
    page.goto(BASE + "/console/import")
    shot(page, "02-import", {"validate": {"tag": "h2", "text": "Validate a file"}, "file": "#file", "button": {"tag": "button", "text": "Validate"}, "sample": "a[href$='sample-register.xlsx']"})
    page.set_input_files("#file", str(W / "src" / "demo" / "sample-register.xlsx"))
    page.click("form[action='/console/import'] button[type=submit]")
    shot(page, "03-import-report", {"errors": {"tag": "h2", "text": "Errors"}, "warnings": {"tag": "h2", "text": "Warnings"}, "missing": {"tag": "h2", "text": "Missing export"}, "commit": "form[action='/console/import/commit'] button"})
    page.click("form[action='/console/import/commit'] button")
    shot(page, "04-committed", {"ok": "p.ok", "table": "table"})
    page.goto(BASE + "/console/records")
    shot(page, "05-records", {"h1": "h1", "table": "table"})
    page.goto(BASE + "/console/completeness")
    shot(page, "06-completeness", {"h1": "h1", "table": "table"})
    page.goto(BASE + "/console/controls")
    shot(page, "07-controls", {"overall": "section.card", "dora": {"tag": "h2", "text": "DORA"}, "metric": "p.metric",
                               "na": ".st-not-assessed", "target": f"a[href='{CTRL}']"})
    page.goto(BASE + CTRL)
    shot(page, "08-control", {"status": {"tag": "h2", "text": "Status"}, "rule": {"tag": "h2", "text": "Rule"}, "evidence": {"tag": "h2", "text": "Evidence"}, "wf": {"tag": "h2", "text": "Review workflow"}})

    page.fill("#wf-owner", "owner@demo.invalid")
    page.fill("#wf-reviewer", "approver@demo.invalid")
    page.fill("#wf-notes", "Provider identification evidenced by the 2026 provider audit summary.")
    page.click(f"form[action='{CTRL}/assign'] button")
    page.wait_for_load_state("networkidle")
    page.click("details.card summary")
    page.fill("#ev-title", "Provider audit summary")
    page.fill("#ev-source", "SharePoint")
    page.fill("#ev-uri", (EV / "provider-audit-summary.md").resolve().as_uri())
    page.fill("#ev-sum", "sha256:" + sums["provider-audit-summary.md"])
    shot(page, "09-evidence-form", {"title": "#ev-title", "uri": "#ev-uri", "sum": "#ev-sum", "attach": {"tag": "button", "text": "Attach"}, "evidence": {"tag": "h2", "text": "Evidence"}})
    page.fill("#ev-uri", (EV / "provider-audit-summary.md").resolve().as_uri())  # undo the on-screen mask
    page.click(f"form[action='{CTRL}/evidence'] button")
    page.wait_for_load_state("networkidle")
    v = page.locator("form[action$='/verify'] button")
    if v.count():
        v.first.click(); page.wait_for_load_state("networkidle")
    shot(page, "10-evidence-verified", {"integrity": {"tag": "td", "text": "verified"}, "evidence": {"tag": "h2", "text": "Evidence"}, "table": "table", "ok": "p.ok"})
    page.click(f"form[action='{CTRL}/submit'] button")
    shot(page, "11-submitted", {"status": {"tag": "h2", "text": "Status"}, "ok": "p.ok", "wf": {"tag": "h2", "text": "Review workflow"}})

    ctx.clear_cookies()
    signin(page, "approver@demo.invalid")
    page.goto(BASE + CTRL)
    if page.locator("#wf-note").count():
        page.fill("#wf-note", "Evidence verified against the checksum; provider data complete.")
        page.click(f"form[action='{CTRL}/recommend'] button"); page.wait_for_load_state("networkidle")
    page.goto(BASE + CTRL)
    shot(page, "12-approve", {"approve": {"tag": "button", "text": "Approve"}, "wf": {"tag": "h2", "text": "Review workflow"}, "status": {"tag": "h2", "text": "Status"}})
    page.click(f"form[action='{CTRL}/approve'] button")
    shot(page, "13-approved", {"status": {"tag": "h2", "text": "Status"}, "ok": "p.ok", "badge": ".badge", "approval": {"tag": "dt", "text": "Approval"}, "history": {"tag": "h2", "text": "History"}})
    page.goto(BASE + "/console/controls")
    shot(page, "14-controls-after", {"metric": "p.metric", "target": f"a[href='{CTRL}']", "ready": ".st-ready"})

    ctx.clear_cookies()
    signin(page, "owner@demo.invalid")
    page.goto(BASE + "/console/reports")
    shot(page, "15-reports", {"generate": {"tag": "h2", "text": "Generate"}, "button": {"tag": "button", "text": "Generate"}})
    for box in page.locator("input[type=checkbox][name=format]").all():
        box.check()
    inc = page.locator("input[type=checkbox][id^=incomplete-]")
    for box in inc.all():
        box.check()
    page.click("form[action='/console/reports'] button[type=submit]")
    shot(page, "16-report", {"h1": "h1", "ok": "p.ok", "table": "table"})
    print("url", page.url)
    pdf = page.locator("a[href*='.pdf'], a[href*='pdf']")
    if pdf.count():
        with page.expect_download() as d:
            pdf.first.click()
        d.value.save_as(str(W / "report.pdf")); print("pdf saved")

    ctx.clear_cookies()
    signin(page, "auditor@demo.invalid")
    page.goto(BASE + "/console/audit")
    shot(page, "17-audit", {"h1": "h1", "table": "table", "ok": "p.ok"})
    b.close()
