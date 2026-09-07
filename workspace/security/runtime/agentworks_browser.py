"""Platform Playwright context manager, supplied by the sandbox launcher.

Use with browser_session(record_video=True) as (context, artifacts): ...
Install Playwright through the workflow's persistent package environment first.
Artifacts survive scratch cleanup under STEP_OUTPUT_DIR/browser/<unique-id>.
"""
from contextlib import contextmanager
import os
from pathlib import Path
import uuid


@contextmanager
def browser_session(*, record_video=False, trace=True, **context_options):
    # Playwright's video encoder is versioned with the installed package.
    # Keep it in persistent tooling storage, never the command scratch cache.
    persistent = os.environ.get("SANDBOX_PERSISTENT_DIR")
    if persistent:
        os.environ.setdefault("PLAYWRIGHT_BROWSERS_PATH", str(Path(persistent) / "ms-playwright"))
    from playwright.sync_api import sync_playwright

    executable = os.environ.get("AGENT_BROWSER_EXECUTABLE_PATH")
    if not executable or not Path(executable).is_file():
        raise RuntimeError("Platform browser executable is unavailable; check AGENT_BROWSER_EXECUTABLE_PATH")
    artifacts = Path(os.environ["STEP_OUTPUT_DIR"]) / "browser" / uuid.uuid4().hex
    artifacts.mkdir(parents=True, exist_ok=False)
    if record_video:
        context_options["record_video_dir"] = str(artifacts)
    print(f"[browser] executable={executable} tmp={os.environ.get('TMPDIR')} artifacts={artifacts}", flush=True)
    with sync_playwright() as pw:
        browser = pw.chromium.launch(headless=True, executable_path=executable,
                                    args=["--disable-dev-shm-usage"])
        context = None
        try:
            context = browser.new_context(**context_options)
            if trace:
                context.tracing.start(screenshots=True, snapshots=True, sources=True)
            yield context, artifacts
        finally:
            # Diagnostics must not hide the original test failure. Closing the
            # context flushes Playwright recordings before the browser exits.
            if context is not None:
                try:
                    for index, page in enumerate(context.pages):
                        page.screenshot(path=str(artifacts / f"page-{index}.png"), timeout=5000)
                    if trace:
                        context.tracing.stop(path=str(artifacts / "trace.zip"))
                except Exception as error:
                    print(f"[browser] diagnostic capture failed: {error}", flush=True)
                try:
                    context.close()
                finally:
                    browser.close()
            else:
                browser.close()
