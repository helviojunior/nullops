"""Captures README screenshots of NullOps with Playwright (headless Chromium)."""
import asyncio
import base64
import html
import json
import os
from playwright.async_api import async_playwright

BASE = "https://127.0.0.1:8443"
OUT = "/work/out"
VIEW = {"width": 1440, "height": 900}

PAD_TEXT = """Incident 2026-11-13 - API latency (sev-2)

14:02  alert: p95 latency > 2 s on api-02
14:05  checking DB connection pool on db-primary
14:09  rollback of release 2.4.1 started
14:15  latency back to normal, monitoring

Next steps:
- postmortem draft
- alert on connection pool exhaustion
"""


async def new_page(browser, theme="dark"):
    ctx = await browser.new_context(viewport=VIEW, ignore_https_errors=True, color_scheme=theme)
    await ctx.add_init_script(
        "try{localStorage.setItem('nullops.lang','en');"
        f"localStorage.setItem('nullops.theme','{theme}');"
        "localStorage.setItem('nullops.tree', JSON.stringify({open:true,width:280,tab:'folders'}));}catch(e){}"
    )
    return await ctx.new_page()


async def settle(page, ms=700):
    await page.wait_for_timeout(ms)


async def login(page, user, password):
    await page.goto(BASE + "/login")
    await page.fill("#username", user)
    await page.click("button[type=submit]")
    await page.wait_for_selector("#password")
    await page.fill("#password", password)
    await page.click("button[type=submit]")
    await page.wait_for_url("**/files**")
    await settle(page)


async def shot(page, name):
    await page.screenshot(path=f"{OUT}/{name}.png")
    print("saved", name)


async def capture_api(page):
    """Records every /api request of the page (method, status, headers,
    request body, response body) exactly as it went over the wire."""
    calls = []

    async def on_finished(req):
        if "/api/" not in req.url:
            return
        try:
            resp = await req.response()
            body = await resp.text()
            calls.append({
                "action": req.url.rsplit("/api/", 1)[1],
                "method": req.method,
                "status": resp.status,
                "headers": await req.all_headers(),
                "request": req.post_data or "",
                "response": body,
            })
        except Exception:
            pass

    page.on("requestfinished", lambda req: asyncio.ensure_future(on_finished(req)))
    return calls


def big_file(name, mib):
    """A sparse file of mib MiB (Playwright only takes up to 50 MB in memory)."""
    os.makedirs("/tmp/shots", exist_ok=True)
    path = os.path.join("/tmp/shots", name)
    with open(path, "wb") as f:
        f.truncate(mib * 1024 * 1024)
    return path


def pretty(raw):
    try:
        return json.dumps(json.loads(raw), indent=2)
    except Exception:
        return raw


def inspector_html(app_png, calls, selected):
    """App screenshot on top, a network-inspector style panel below with the
    real captured traffic (the selected call's request and response)."""
    esc = html.escape
    rows = []
    for c in calls[-8:]:
        sel = c is selected
        rows.append(
            f'<tr class="{"sel" if sel else ""}"><td><span class="st">{c["status"]}</span></td>'
            f'<td>{esc(c["method"])}</td><td>{esc(c["action"])}</td><td>json</td>'
            f'<td class="r">{len(c["response"])} B</td></tr>')
    sid = selected["headers"].get("x-session-id", "")
    return f"""<!doctype html><html><head><meta charset="utf-8"><style>
body{{margin:0;background:#18181b;color:#d4d4d8;font:12px -apple-system,Segoe UI,Roboto,sans-serif}}
img{{display:block;width:1440px}}
.dt{{display:flex;height:430px;border-top:1px solid #3f3f46;background:#1f1f23}}
.tabs{{height:30px;display:flex;gap:18px;align-items:center;padding:0 12px;border-bottom:1px solid #3f3f46;background:#27272a}}
.tabs span.on{{color:#fff;border-bottom:2px solid #3b82f6;padding-bottom:6px}}
.list{{width:46%;border-right:1px solid #3f3f46}}
table{{width:100%;border-collapse:collapse}}
th,td{{text-align:left;padding:5px 10px;border-bottom:1px solid #2e2e33;white-space:nowrap}}
th{{color:#a1a1aa;font-weight:500}} td.r,th.r{{text-align:right}}
tr.sel td{{background:#1e3a8a;color:#fff}}
.st{{background:#16a34a;color:#fff;border-radius:3px;padding:0 4px;font-size:11px}}
.detail{{flex:1;overflow:hidden}}
.sec{{padding:8px 12px}}
.sec h4{{margin:0 0 6px;color:#a1a1aa;font-weight:600;font-size:11px;text-transform:uppercase;letter-spacing:.04em}}
pre{{margin:0;font:11.5px ui-monospace,Menlo,Consolas,monospace;white-space:pre-wrap;word-break:break-all;color:#e879f9}}
.hdr{{font:11.5px ui-monospace,Menlo,monospace;color:#a5b4fc}}
.note{{position:absolute;right:16px;bottom:8px;color:#71717a;font-size:11px}}
</style></head><body>
<img src="data:image/png;base64,{app_png}">
<div class="tabs"><span>Inspector</span><span>Console</span><span class="on">Network</span><span>Storage</span></div>
<div class="dt">
  <div class="list"><table><tr><th>Status</th><th>Method</th><th>File</th><th>Type</th><th class="r">Size</th></tr>
  {''.join(rows)}</table></div>
  <div class="detail">
    <div class="tabs"><span>Headers</span><span class="on">Request / Response</span><span>Timings</span></div>
    <div class="sec"><h4>Request headers</h4><div class="hdr">POST /api/{esc(selected["action"])}<br>X-Session-Id: {esc(sid)}</div></div>
    <div class="sec"><h4>Request payload (encrypted)</h4><pre>{esc(pretty(selected["request"]))}</pre></div>
    <div class="sec"><h4>Response (encrypted)</h4><pre>{esc(pretty(selected["response"]))}</pre></div>
  </div>
</div>
</body></html>"""


async def main():
    async with async_playwright() as p:
        browser = await p.chromium.launch()

        # 1. sign-in (user step) and the MFA step of a user with TOTP enabled
        page = await new_page(browser)
        await page.goto(BASE + "/login")
        await page.wait_for_selector("#username")
        await settle(page)
        await shot(page, "login")
        await page.fill("#username", "carol")
        await page.click("button[type=submit]")
        await page.wait_for_selector("#code")
        await page.fill("#code", "482")
        await settle(page)
        await shot(page, "login-mfa")

        # 2. file browser with the folder tree
        calls = await capture_api(page)
        await login(page, "admin", "Demo-Pass-2026")
        await page.goto(BASE + "/files/infra")
        await page.wait_for_selector("tbody tr")
        await settle(page)
        await shot(page, "files")

        # 2b. the same screen with the real encrypted traffic of that moment,
        # shown in a network-inspector style panel (headless browsers cannot
        # capture the real DevTools window)
        lists = [c for c in calls if c["action"] == "list" and '"iv"' in c["response"]]
        if lists:
            app_png = base64.b64encode(await page.screenshot()).decode()
            comp = await browser.new_page(viewport={"width": 1440, "height": 900 + 460})
            await comp.set_content(inspector_html(app_png, calls, lists[-1]))
            await comp.wait_for_timeout(300)
            await comp.screenshot(path=f"{OUT}/encrypted-traffic.png", full_page=True)
            await comp.close()
            print("saved encrypted-traffic")

        # 3. action menu and the folder picker (move)
        row = page.locator("tbody tr", has_text="backup.sh")
        await row.get_by_role("button", name="Actions").click()
        await settle(page, 300)
        await shot(page, "actions-menu")
        await page.get_by_role("menuitem", name="Move").click()
        await page.wait_for_selector("text=Choose the destination folder")
        await page.get_by_role("button", name="releases").last.click()
        await settle(page)
        await shot(page, "move-picker")
        await page.keyboard.press("Escape")

        # 4. editor (CodeMirror, PowerShell highlighting)
        await page.goto(BASE + "/edit/infra/deploy.ps1")
        await page.wait_for_selector(".cm-content")
        await settle(page, 1200)
        await shot(page, "editor")

        # 5. preview
        await page.goto(BASE + "/view/media/nullops-logo.svg")
        await page.wait_for_selector("img[alt='nullops-logo.svg']")
        await settle(page)
        await shot(page, "preview")

        # 5b. upload link for a folder ("Request upload")
        await page.goto(BASE + "/files/releases")
        await page.wait_for_selector("tbody tr")
        await page.get_by_role("button", name="Request upload").click()
        await page.wait_for_selector("[role=dialog] code")
        await settle(page)
        await shot(page, "request-upload")
        link = (await page.locator("[role=dialog] code").first.inner_text()).strip()
        await page.keyboard.press("Escape")

        # 5c. the public page of that link, with no account: one file sent,
        # a big one in progress
        guest = await browser.new_page(viewport=VIEW, ignore_https_errors=True, color_scheme="dark")
        await guest.add_init_script("try{localStorage.setItem('nullops.lang','en')}catch(e){}")
        await guest.goto(link.replace("https://127.0.0.1:8443", BASE))
        await guest.wait_for_selector("text=Drop files here")
        await guest.set_input_files("input[type=file]", [
            {"name": "invoice-2026-11.pdf", "mimeType": "application/pdf", "buffer": b"%PDF-1.4\n" + b"0" * 180_000},
        ])
        await guest.wait_for_selector("text=saved as")
        await guest.set_input_files("input[type=file]", [
            big_file("vm-snapshot-2026-11-13.vmdk", 320),
        ])
        await guest.wait_for_selector("text=vm-snapshot-2026-11-13.vmdk", timeout=15000)
        await guest.wait_for_timeout(3000)
        await shot(guest, "public-upload")
        await guest.close()

        # 5d. a signed-in upload in progress: header ring + Transfers page
        await page.goto(BASE + "/files/releases")
        await page.wait_for_selector("tbody tr")
        await page.set_input_files("input[type=file]", [
            big_file("nullops_1.4.0_backup.tar.gz", 400),
        ])
        await page.wait_for_selector("a[href='/transfers'] svg circle", timeout=15000)
        await page.wait_for_timeout(2000)
        await page.locator("a[href='/transfers']").click()
        await page.wait_for_selector("text=Cancel all")
        await page.wait_for_timeout(1500)
        await shot(page, "transfers")
        await page.get_by_role("button", name="Cancel all").click()

        # 6. PastePad: two users editing the same pad
        await page.goto(BASE + "/files")
        await page.get_by_role("tab", name="PastePad").click()
        await page.get_by_role("button", name="New pad").click()
        await page.fill("#dialog-input", "incident-2026-11-13")
        await page.get_by_role("button", name="Create").click()
        await page.wait_for_selector(".cm-content")
        await page.click(".cm-content")
        await page.keyboard.type(PAD_TEXT, delay=2)
        await page.keyboard.press("Control+s")
        await settle(page, 1500)

        other = await new_page(browser)
        await login(other, "ana", "Demo-Pass-2026")
        await other.goto(BASE + "/pad/incident-2026-11-13")
        await other.wait_for_selector(".cm-content")
        await other.click(".cm-content")
        await other.keyboard.press("Control+End")
        await other.keyboard.type("- check pool size on db-replica (ana)\n", delay=10)
        await other.keyboard.press("Control+s")
        await page.wait_for_selector("text=db-replica", timeout=15000)
        # the pad list refreshes every 10 s: wait until it shows both editors
        item = page.locator("aside a", has_text="incident-2026-11-13")
        for _ in range(60):
            if (await item.inner_text()).strip().endswith("2"):
                break
            await page.wait_for_timeout(500)
        await settle(page, 800)
        await shot(page, "pastepad")

        # 7. account security: MFA enrollment
        await page.goto(BASE + "/account")
        await page.get_by_role("button", name="Enable MFA").click()
        await page.wait_for_selector("img[alt='QR code']")
        await settle(page)
        await shot(page, "account-mfa")

        # 8. light theme
        light = await new_page(browser, theme="light")
        await login(light, "admin", "Demo-Pass-2026")
        await light.goto(BASE + "/files/docs")
        await light.wait_for_selector("tbody tr")
        await settle(light)
        await shot(light, "files-light")

        await browser.close()


asyncio.run(main())
