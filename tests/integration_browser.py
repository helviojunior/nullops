#!/usr/bin/env python3
"""Comprehensive automated Chrome (Playwright) test of the full UI.

Exercises every user flow and captures ALL JavaScript console output,
uncaught page errors and failed network requests. Exits non-zero if any
error-level console message, page error, or failed /api request occurs.
"""
import os
import sys
import tempfile
from playwright.sync_api import sync_playwright

BASE = 'http://127.0.0.1:8181'
console_errors = []
console_all = []
page_errors = []
failed_requests = []
PASS = []
FAIL = []


def check(name, cond):
    (PASS if cond else FAIL).append(name)
    print('  [%s] %s' % ('+' if cond else 'FAIL', name))


def run():
    with sync_playwright() as p:
        browser = p.chromium.launch()
        ctx = browser.new_context(viewport={'width': 1200, 'height': 800})
        page = ctx.new_page()

        page.on('console', lambda m: (
            console_all.append('%s: %s' % (m.type, m.text)),
            console_errors.append(m.text) if m.type == 'error' else None))
        page.on('pageerror', lambda e: page_errors.append(str(e)))
        page.on('requestfailed', lambda r: failed_requests.append(
            '%s %s' % (r.method, r.url)))
        page.on('response', lambda r: failed_requests.append(
            'HTTP %d %s' % (r.status, r.url))
            if ('/api/' in r.url and r.status >= 500) else None)

        # ---- load + login screen ----
        print('\n== load & auth ==')
        page.goto(BASE, wait_until='networkidle')
        check('login screen visible', page.is_visible('#login-screen'))
        check('app hidden before login', page.is_hidden('#app'))

        # wrong password first
        page.fill('#login-user', 'admin')
        page.fill('#login-pass', 'WRONGPW')
        page.click('#login-form button[type=submit]')
        page.wait_for_timeout(600)
        check('wrong password shows error',
              'Invalid credentials' in (page.inner_text('#login-error') or ''))
        check('app still hidden after bad login', page.is_hidden('#app'))

        # correct password
        page.fill('#login-pass', 'testpass123')
        page.click('#login-form button[type=submit]')
        page.wait_for_selector('#app:not(.hidden)', timeout=5000)
        check('app visible after login', page.is_visible('#app'))

        # ---- user identity ----
        print('\n== user identity ==')
        check('whoami shows admin', page.inner_text('#whoami') == 'admin')
        page.wait_for_selector('#filelist tr')

        def names():
            return [e.inner_text() for e in page.query_selector_all('#filelist .label')]

        check('initial listing has sample.txt', 'sample.txt' in names())
        check('initial listing has docs folder', 'docs' in names())

        # ---- new folder (prompt) ----
        print('\n== create folder (prompt dialog) ==')
        page.once('dialog', lambda d: d.accept('new_folder_1'))
        page.click('#btn-newfolder')
        page.wait_for_timeout(500)
        check('folder created via prompt', 'new_folder_1' in names())

        # cancel prompt should NOT create
        page.once('dialog', lambda d: d.dismiss())
        page.click('#btn-newfolder')
        page.wait_for_timeout(400)
        check('cancel prompt creates nothing',
              len([n for n in names() if n.startswith('new_folder')]) == 1)

        # ---- new file (prompt) ----
        print('\n== create file (prompt dialog) ==')
        page.once('dialog', lambda d: d.accept('created.txt'))
        page.click('#btn-newfile')
        page.wait_for_timeout(500)
        check('file created via prompt', 'created.txt' in names())

        # ---- navigate into folder & breadcrumb ----
        print('\n== navigation & breadcrumb ==')
        page.click('.label.dir >> text=docs')
        page.wait_for_timeout(400)
        crumbs = [c.inner_text() for c in page.query_selector_all('#breadcrumb .crumb')]
        check('breadcrumb shows docs', 'docs' in crumbs)
        check('listing shows .. parent row',
              any('..' in r.inner_text() for r in page.query_selector_all('#filelist tr')))
        # go back home via breadcrumb home icon
        page.click('#breadcrumb .crumb >> nth=0')
        page.wait_for_timeout(400)
        check('back at root', 'sample.txt' in names())

        # ---- editor: open, edit, save ----
        print('\n== text editor ==')
        page.click('.label >> text=sample.txt')
        page.wait_for_selector('#editor-modal:not(.hidden)', timeout=3000)
        check('editor opened', page.is_visible('#editor-modal'))
        check('editor loaded content', 'line1' in page.input_value('#editor-text'))
        page.fill('#editor-text', 'EDITED CONTENT\n')
        page.click('#editor-save')
        page.wait_for_timeout(500)
        check('editor closed after save', page.is_hidden('#editor-modal'))
        # reopen to confirm persistence
        page.click('.label >> text=sample.txt')
        page.wait_for_selector('#editor-modal:not(.hidden)')
        check('saved content persisted',
              'EDITED CONTENT' in page.input_value('#editor-text'))
        page.click('#editor-close')
        page.wait_for_timeout(300)

        # ---- rename (prompt) ----
        print('\n== rename ==')
        row = page.query_selector('xpath=//tr[.//span[text()="created.txt"]]')
        page.once('dialog', lambda d: d.accept('renamed.txt'))
        row.query_selector('button[title=Rename]').click()
        page.wait_for_timeout(500)
        check('file renamed', 'renamed.txt' in names() and 'created.txt' not in names())

        # ---- chmod (prompt) ----
        print('\n== chmod ==')
        row = page.query_selector('xpath=//tr[.//span[text()="renamed.txt"]]')
        page.once('dialog', lambda d: d.accept('600'))
        row.query_selector('.perms').click()
        page.wait_for_timeout(500)
        row = page.query_selector('xpath=//tr[.//span[text()="renamed.txt"]]')
        perms = row.query_selector('.perms').inner_text()
        check('chmod 600 applied (rw-------)', perms == '-rw-------')

        # ---- upload ----
        print('\n== upload ==')
        tmpf = tempfile.NamedTemporaryFile(mode='w', suffix='.txt', delete=False,
                                           prefix='uploaded_')
        tmpf.write('uploaded via browser\n'); tmpf.close()
        page.set_input_files('#fileinput', tmpf.name)
        page.wait_for_timeout(1200)
        up_name = os.path.basename(tmpf.name)
        check('uploaded file appears', up_name in names())

        # ---- download ----
        print('\n== download ==')
        try:
            with page.expect_download(timeout=5000) as dl_info:
                row = page.query_selector('xpath=//tr[.//span[text()="%s"]]' % up_name)
                row.query_selector('button[title=Download]').click()
            dl = dl_info.value
            path = dl.path()
            content = open(path, 'rb').read() if path else b''
            check('download produced correct content',
                  content == b'uploaded via browser\n')
        except Exception as e:
            check('download produced correct content (%s)' % e, False)

        # ---- search ----
        print('\n== search ==')
        page.fill('#search', 'renamed')
        page.wait_for_timeout(600)
        check('search filters to match', names() == ['renamed.txt'])
        page.fill('#search', '')
        page.wait_for_timeout(600)
        check('clearing search restores listing', 'sample.txt' in names())

        # ---- batch select + zip ----
        print('\n== batch operations ==')
        page.query_selector('#filelist tr:has-text("renamed.txt") input.sel').check()
        page.query_selector('#filelist tr:has-text("data.json") input.sel').check()
        page.wait_for_timeout(300)
        check('batch bar visible', page.is_visible('#batchbar'))
        check('batch count = 2', page.inner_text('#batchcount') == '2')
        page.click('#batch-zip')
        page.wait_for_timeout(800)
        check('zip archive created', any(n.endswith('.zip') for n in names()))

        # ---- batch delete (confirm dialog) ----
        print('\n== batch delete (confirm) ==')
        page.query_selector('#filelist tr:has-text("new_folder_1") input.sel').check()
        page.once('dialog', lambda d: d.accept())
        page.click('#batch-delete')
        page.wait_for_timeout(600)
        check('batch delete removed folder', 'new_folder_1' not in names())

        # ---- single delete (confirm) ----
        print('\n== single delete (confirm) ==')
        before = names()
        row = page.query_selector('xpath=//tr[.//span[text()="blob.bin"]]')
        page.once('dialog', lambda d: d.accept())
        row.query_selector('button[title=Delete]').click()
        page.wait_for_timeout(600)
        check('single delete removed file', 'blob.bin' not in names())

        # ---- logout ----
        print('\n== logout ==')
        page.click('#btn-logout')
        page.wait_for_timeout(800)
        check('logout returns to login screen', page.is_visible('#login-screen'))

        browser.close()


run()

print('\n================ CONSOLE / ERROR REPORT ================')
print('console messages total:', len(console_all))
for c in console_all:
    print('   ', c)
print('\nconsole ERRORS:', len(console_errors))
for c in console_errors:
    print('   !', c)
print('\npage errors (uncaught JS):', len(page_errors))
for e in page_errors:
    print('   !', e)
srv_errors = [f for f in failed_requests if f.startswith('HTTP 5')]
print('\nserver 5xx on /api:', len(srv_errors))
for f in srv_errors:
    print('   !', f)

print('\n================ RESULT ================')
print('checks passed: %d / %d' % (len(PASS), len(PASS) + len(FAIL)))
if FAIL:
    print('FAILED checks:')
    for f in FAIL:
        print('   -', f)

ok = (not FAIL and not console_errors and not page_errors and not srv_errors)
print('\n%s' % ('=== ALL BROWSER TESTS PASSED, NO JS ERRORS ===' if ok
                else '=== FAILURES DETECTED ==='))
sys.exit(0 if ok else 1)
