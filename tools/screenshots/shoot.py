"""Captures README screenshots of NullOps with Playwright (headless Chromium)."""
import asyncio
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
        await login(page, "admin", "Demo-Pass-2026")
        await page.goto(BASE + "/files/infra")
        await page.wait_for_selector("tbody tr")
        await settle(page)
        await shot(page, "files")

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
