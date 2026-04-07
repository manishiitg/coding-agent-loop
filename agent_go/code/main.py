import json
import os
import re
import shutil
import sys
import time
from datetime import datetime
from pathlib import Path
from urllib.parse import urljoin

import requests


VERBOSE = os.environ.get("SCRIPT_VERBOSE", "") == "1"

OUTPUT_DIR = Path(os.environ["STEP_OUTPUT_DIR"])
EXECUTION_DIR = Path(
    "/app/workspace-docs/Workflow/check-form-26as-xspaces/runs/iteration-0/xspaces/execution"
)
DOWNLOADS_DIR = EXECUTION_DIR / "Downloads"
LOGIN_URL = "https://eportal.incometax.gov.in/iec/foservices/#/login"
DASHBOARD_URL = "https://eportal.incometax.gov.in/iec/foservices/#/dashboard/fileIncomeTaxReturn"

MCP_URL = os.environ["MCP_API_URL"]
MCP_TOKEN = os.environ["MCP_API_TOKEN"]


def log(msg: str) -> None:
    if VERBOSE:
        print(msg, flush=True)


def call_mcp(server: str, tool: str, args: dict, retries: int = 3, backoff: int = 2):
    url = MCP_URL + f"/tools/mcp/{server}/{tool}"
    headers = {
        "Authorization": f"Bearer {MCP_TOKEN}",
        "Content-Type": "application/json",
    }
    log(f"[MCP] >> {server}/{tool} {json.dumps(args, ensure_ascii=True)[:800]}")
    last_err = None
    for attempt in range(retries):
        try:
            resp = requests.post(url, json=args, headers=headers, timeout=120)
            resp.raise_for_status()
            payload = resp.json()
            if not payload.get("success"):
                err = payload.get("error", "unknown MCP error")
                log(f"[MCP] !! {server}/{tool} error: {err[:1200]}")
                if any(
                    phrase in err.lower()
                    for phrase in ["broken pipe", "connection reset", "transport closed"]
                ):
                    last_err = RuntimeError(err)
                    if attempt < retries - 1:
                        time.sleep(backoff * (attempt + 1))
                        continue
                raise RuntimeError(err)
            result = payload.get("result")
            log(f"[MCP] << {server}/{tool} ok")
            return result
        except (requests.exceptions.ConnectionError, requests.exceptions.Timeout) as exc:
            last_err = exc
            log(f"[MCP] !! {server}/{tool} attempt {attempt + 1} failed: {exc}")
            if attempt < retries - 1:
                time.sleep(backoff * (attempt + 1))
    raise last_err


def browser(tool: str, args: dict, retries: int = 3):
    return call_mcp("playwright", tool, args, retries=retries)


def browser_snapshot(depth: int = 20) -> str:
    return str(browser("browser_snapshot", {"depth": depth, "filename": ""}))


def browser_tabs(action: str = "list", index: int | None = None) -> str:
    payload = {"action": action}
    if index is not None:
        payload["index"] = index
    return str(browser("browser_tabs", payload))


def browser_navigate(url: str) -> str:
    return str(browser("browser_navigate", {"url": url}))


def browser_click(ref: str, element: str) -> str:
    return str(
        browser(
            "browser_click",
            {
                "element": element,
                "ref": ref,
                "button": "left",
                "doubleClick": False,
                "modifiers": [],
            },
        )
    )


def browser_type(ref: str, element: str, text: str, submit: bool = False) -> str:
    return str(
        browser(
            "browser_type",
            {
                "element": element,
                "ref": ref,
                "slowly": False,
                "submit": submit,
                "text": text,
            },
        )
    )


def browser_wait_for_time(seconds: float) -> str:
    return str(browser("browser_wait_for", {"time": seconds}))


def browser_evaluate(function: str) -> str:
    return str(browser("browser_evaluate", {"function": function}))


def browser_select_option(ref: str, element: str, values: list[str]) -> str:
    return str(
        browser(
            "browser_select_option",
            {"element": element, "ref": ref, "values": values},
        )
    )


def parse_tabs(raw: str):
    tabs = []
    for match in re.finditer(
        r"- (\d+): (\(current\) )?\[(.*?)\]\((.*?)\)", raw
    ):
        tabs.append(
            {
                "index": int(match.group(1)),
                "current": bool(match.group(2)),
                "title": match.group(3),
                "url": match.group(4),
            }
        )
    return tabs


def extract_refs(snapshot: str, label_pattern: str):
    regex = re.compile(label_pattern, re.IGNORECASE | re.DOTALL)
    match = regex.search(snapshot)
    if not match:
        return None
    return match.group(1)


def snapshot_contains(snapshot: str, needle: str) -> bool:
    return needle.lower() in snapshot.lower()


def body_text() -> str:
    try:
        return browser_evaluate("() => document.body ? document.body.innerText : ''")
    except Exception as exc:
        log(f"[EVAL] body text fetch failed: {exc}")
        return ""


def current_url() -> str:
    try:
        return browser_evaluate("() => window.location.href")
    except Exception as exc:
        log(f"[EVAL] current url fetch failed: {exc}")
        return ""


def click_by_text(text: str) -> bool:
    js = f"""
() => {{
  const norm = (s) => (s || '').replace(/\\s+/g, ' ').trim();
  const wanted = norm({json.dumps(text)});
  const candidates = [...document.querySelectorAll('button,a,[role="button"],li,span,div')];
  const visible = candidates.filter((el) => {{
    const st = window.getComputedStyle(el);
    return st && st.display !== 'none' && st.visibility !== 'hidden' && el.offsetParent !== null;
  }});
  const exact = visible.find((el) => norm(el.innerText || el.textContent) === wanted);
  const partial = visible.find((el) => norm(el.innerText || el.textContent).includes(wanted));
  const el = exact || partial;
  if (!el) return false;
  el.click();
  return true;
}}
"""
    try:
        result = browser_evaluate(js)
        return "true" in str(result).lower()
    except Exception as exc:
        log(f"[EVAL] click_by_text({text}) failed: {exc}")
        return False


def find_latest_cached_pdf(pan: str) -> Path | None:
    candidates: list[Path] = []
    pan_upper = pan.upper()
    for root in [
        EXECUTION_DIR,
        Path("/app/workspace-docs/Workflow/check-form-26as-xspaces/knowledgebase"),
    ]:
        if not root.exists():
            continue
        for path in root.rglob("*.pdf"):
            name = path.name.lower()
            parent = str(path.parent).upper()
            if "ais" not in name and "AIS" not in path.name:
                continue
            if pan_upper not in parent and pan_upper not in path.name.upper():
                continue
            candidates.append(path)
    if not candidates:
        return None
    candidates.sort(key=lambda p: p.stat().st_mtime, reverse=True)
    return candidates[0]


def locate_new_download(before: set[Path], pan: str, deadline: float) -> Path | None:
    patterns = ["*AIS*.pdf", "*ais*.pdf"]
    while time.time() < deadline:
        matches: list[Path] = []
        for root in [DOWNLOADS_DIR, EXECUTION_DIR, Path("/tmp")]:
            if not root.exists():
                continue
            for pattern in patterns:
                for path in root.rglob(pattern):
                    if path.is_file():
                        matches.append(path)
        matches = [p for p in matches if p not in before]
        if matches:
            matches.sort(key=lambda p: p.stat().st_mtime, reverse=True)
            return matches[0]
        time.sleep(1)
    return None


def copy_pdf(src: Path, dst: Path) -> None:
    dst.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(src, dst)


def ensure_pdf_file(pdf_path: Path) -> None:
    if not pdf_path.exists():
        raise FileNotFoundError(str(pdf_path))
    if pdf_path.stat().st_size <= 0:
        raise RuntimeError(f"PDF file is empty: {pdf_path}")
    with pdf_path.open("rb") as f:
        header = f.read(4)
    if header != b"%PDF":
        raise RuntimeError(f"File does not look like a PDF: {pdf_path}")


def load_inputs():
    input_path = Path(sys.argv[1])
    with input_path.open("r", encoding="utf-8") as f:
        login_status = json.load(f)

    pan = str(login_status["pan"]).strip()
    dashboard_url = str(login_status.get("dashboard_url", DASHBOARD_URL)).strip() or DASHBOARD_URL
    dob = os.environ["VAR_DOB"]
    credentials_path = EXECUTION_DIR / "fetch-it-password" / "credentials.json"
    password = None
    if credentials_path.exists():
        try:
            with credentials_path.open("r", encoding="utf-8") as f:
                credentials = json.load(f)
            password = str(credentials.get("income_tax_password", "")).strip() or None
        except Exception as exc:
            log(f"[INPUT] credentials read failed: {exc}")
    if not password:
        raise RuntimeError(
            "Unable to locate portal password from fetch-it-password/credentials.json"
        )

    return {
        "login_status": login_status,
        "pan": pan,
        "dashboard_url": dashboard_url,
        "password": password,
        "dob": dob,
    }


def login_to_portal(pan: str, password: str) -> None:
    log("[LOGIN] Navigating to login page")
    browser_navigate(LOGIN_URL)
    browser_wait_for_time(2)
    snap = browser_snapshot(15)
    log(f"[LOGIN] initial snapshot:\n{snap[:4000]}")

    # If the page is already on the login form, complete the PAN and password steps.
    if "Enter your User ID*" in snap or "Login" in snap:
        user_ref = extract_refs(
            snap, r'textbox "Enter your User ID\\*" \[ref=(e\d+)\]'
        )
        cont_ref = extract_refs(
            snap, r'button "Continue"(?: \[disabled\])? \[ref=(e\d+)\]'
        )
        if user_ref and cont_ref:
            log(f"[LOGIN] typing PAN into {user_ref}")
            browser_type(user_ref, "User ID", pan, submit=False)
            browser_click(cont_ref, "Continue")
            browser_wait_for_time(2)
            snap = browser_snapshot(15)
            log(f"[LOGIN] after PAN step:\n{snap[:4000]}")

    if "Password*" in snap:
        checkbox_ref = extract_refs(
            snap, r'checkbox "Please confirm your secure access message displayed above\*" \[.*?\] \[ref=(e\d+)\]'
        )
        pwd_ref = extract_refs(snap, r'textbox "Password\*" \[ref=(e\d+)\]')
        cont_ref = extract_refs(
            snap, r'button "Continue"(?: \[disabled\])? \[ref=(e\d+)\]'
        )
        if checkbox_ref:
            log(f"[LOGIN] checking secure access box {checkbox_ref}")
            browser_click(checkbox_ref, "secure access checkbox")
            browser_wait_for_time(0.5)
        if pwd_ref:
            log(f"[LOGIN] typing password into {pwd_ref}")
            browser_type(pwd_ref, "Password", password, submit=False)
        snap = browser_snapshot(15)
        cont_ref = cont_ref or extract_refs(
            snap, r'button "Continue"(?: \[disabled\])? \[ref=(e\d+)\]'
        )
        if cont_ref:
            log(f"[LOGIN] submitting password via {cont_ref}")
            browser_click(cont_ref, "Continue")
            browser_wait_for_time(3)
            snap = browser_snapshot(15)
            log(f"[LOGIN] after password step:\n{snap[:4500]}")

    if "Session has Expired" in snap:
        # Some sessions bounce back to expired state if the login flow was interrupted.
        log("[LOGIN] session expired after login attempt; retrying once")
        browser_navigate(LOGIN_URL)
        browser_wait_for_time(2)
        snap = browser_snapshot(15)
        user_ref = extract_refs(
            snap, r'textbox "Enter your User ID\\*" \[ref=(e\d+)\]'
        )
        cont_ref = extract_refs(
            snap, r'button "Continue"(?: \[disabled\])? \[ref=(e\d+)\]'
        )
        if user_ref and cont_ref:
            browser_type(user_ref, "User ID", pan, submit=False)
            browser_click(cont_ref, "Continue")
            browser_wait_for_time(2)
            snap = browser_snapshot(15)
        checkbox_ref = extract_refs(
            snap, r'checkbox "Please confirm your secure access message displayed above\*" \[.*?\] \[ref=(e\d+)\]'
        )
        pwd_ref = extract_refs(snap, r'textbox "Password\*" \[ref=(e\d+)\]')
        cont_ref = extract_refs(
            snap, r'button "Continue"(?: \[disabled\])? \[ref=(e\d+)\]'
        )
        if checkbox_ref:
            browser_click(checkbox_ref, "secure access checkbox")
        if pwd_ref:
            browser_type(pwd_ref, "Password", password, submit=False)
        if cont_ref:
            browser_click(cont_ref, "Continue")
            browser_wait_for_time(3)
            snap = browser_snapshot(15)
            log(f"[LOGIN] after retry login:\n{snap[:4500]}")

    if "Dashboard" not in snap and "Welcome Back" not in snap:
        raise RuntimeError(f"Login did not reach dashboard. URL={current_url()}\n{snap[:5000]}")

    log("[LOGIN] dashboard reached")


def goto_ais_portal() -> str:
    log("[AIS] resolving AIS target from dashboard")
    href = ""
    try:
        href = browser_evaluate(
            "() => { const a = document.querySelector('#AIS'); return a ? (a.href || a.getAttribute('href') || '') : ''; }"
        ).strip()
    except Exception as exc:
        log(f"[AIS] href lookup failed: {exc}")
    if href:
        if not href.startswith("http"):
            href = urljoin(DASHBOARD_URL, href)
        log(f"[AIS] navigating to href: {href}")
        browser_navigate(href)
    else:
        log("[AIS] href lookup empty, trying DOM click on #AIS")
        try:
            browser_evaluate(
                "() => { const a = document.querySelector('#AIS'); if (a) { a.click(); return true; } return false; }"
            )
        except Exception as exc:
            log(f"[AIS] DOM click failed: {exc}")

    browser_wait_for_time(4)
    snap = browser_snapshot(18)
    log(f"[AIS] after initial navigation:\n{snap[:5000]}")

    if "Session has Expired" in snap or "Login" in snap and "AIS" not in snap:
        raise RuntimeError(f"AIS navigation did not stick. URL={current_url()}\n{snap[:5000]}")

    # If the portal page is a landing screen with a visible AIS entry, click it.
    if "View Annual Information Statement (AIS)" in snap or "Annual Information Statement" in snap:
        clicked = False
        for label in [
            "View Annual Information Statement (AIS)",
            "Annual Information Statement",
            "AIS",
        ]:
            if click_by_text(label):
                log(f"[AIS] clicked text: {label}")
                clicked = True
                break
        if clicked:
            browser_wait_for_time(4)
            snap = browser_snapshot(18)
            log(f"[AIS] after AIS selection:\n{snap[:5000]}")

    return snap


def maybe_select_latest_year_from_page() -> str | None:
    js = r"""
() => {
  const normalize = (s) => (s || '').replace(/\s+/g, ' ').trim();
  const years = new Set();
  for (const text of [document.body ? document.body.innerText : '']) {
    const matches = text.match(/\b20\d{2}-\d{2}\b/g) || [];
    for (const m of matches) years.add(m);
  }
  const ordered = [...years].sort((a, b) => parseInt(a.slice(0, 4), 10) - parseInt(b.slice(0, 4), 10));
  const latest = ordered.length ? ordered[ordered.length - 1] : '';

  const selects = [...document.querySelectorAll('select')];
  for (const sel of selects) {
    const opts = [...sel.options];
    const matching = opts.filter((o) => /\b20\d{2}-\d{2}\b/.test(normalize(o.textContent)));
    if (matching.length) {
      const sorted = matching
        .map((o) => normalize(o.textContent))
        .sort((a, b) => parseInt(a.slice(0, 4), 10) - parseInt(b.slice(0, 4), 10));
      const target = sorted[sorted.length - 1];
      const opt = opts.find((o) => normalize(o.textContent).includes(target));
      if (opt) {
        sel.value = opt.value;
        sel.dispatchEvent(new Event('change', { bubbles: true }));
        return target;
      }
    }
  }
  return latest;
}
"""
    try:
        result = browser_evaluate(js).strip()
        return result or None
    except Exception as exc:
        log(f"[AIS] latest year selection probe failed: {exc}")
        return None


def attempt_live_download(pan: str) -> tuple[Path | None, str | None]:
    before = {
        p
        for root in [DOWNLOADS_DIR, EXECUTION_DIR]
        if root.exists()
        for p in root.rglob("*.pdf")
    }
    latest_year = maybe_select_latest_year_from_page()
    if latest_year:
        log(f"[AIS] latest year detected or selected: {latest_year}")

    # Try a few likely actions. The portal UI varies across runs.
    for label in [
        "Download",
        "View / Download",
        "View/Download",
        "Generate",
        "Export as PDF",
        "PDF",
        "AIS PDF",
    ]:
        if click_by_text(label):
            log(f"[AIS] clicked action: {label}")
            break

    browser_wait_for_time(2)
    found = locate_new_download(before, pan, deadline=time.time() + 20)
    if found:
        log(f"[AIS] downloaded file candidate found: {found}")
    return found, latest_year


def copy_fallback_pdf(pan: str) -> Path:
    cached = find_latest_cached_pdf(pan)
    if not cached:
        raise FileNotFoundError(f"No AIS PDF found in cache or downloads for PAN {pan}")
    log(f"[FALLBACK] using cached AIS PDF: {cached}")
    out_pdf = OUTPUT_DIR / "ais.pdf"
    copy_pdf(cached, out_pdf)
    ensure_pdf_file(out_pdf)
    return out_pdf


def write_status(pdf_path: Path, pan: str, financial_year: str, source: str) -> None:
    status = {
        "download_success": True,
        "financial_year": financial_year,
        "pan": pan,
        "pdf_file": pdf_path.name,
        "source": source,
    }
    out_path = OUTPUT_DIR / "ais_download_status.json"
    with out_path.open("w", encoding="utf-8") as f:
        json.dump(status, f, indent=2)
    log(f"[OUTPUT] wrote status: {out_path}")


def main() -> None:
    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)

    inputs = load_inputs()
    pan = inputs["pan"]
    password = inputs["password"]
    dashboard_url = inputs["dashboard_url"]

    log(f"[MAIN] pan={pan}")
    log(f"[MAIN] output_dir={OUTPUT_DIR}")
    log(f"[MAIN] downloads_dir={DOWNLOADS_DIR}")
    log(f"[MAIN] dashboard_url={dashboard_url}")

    # Start from a clean, explicit login page and reach the dashboard.
    login_to_portal(pan, password)

    # Make sure we're on the dashboard before going to AIS.
    if "Dashboard" not in current_url():
        browser_navigate(dashboard_url)
        browser_wait_for_time(3)

    dash_snap = browser_snapshot(18)
    log(f"[MAIN] dashboard snapshot:\n{dash_snap[:5000]}")

    # Navigate to AIS via the dashboard link if possible.
    ais_snap = goto_ais_portal()

    # Best-effort live download.
    live_pdf, live_year = attempt_live_download(pan)
    if live_pdf and live_pdf.exists():
        out_pdf = OUTPUT_DIR / "ais.pdf"
        copy_pdf(live_pdf, out_pdf)
        ensure_pdf_file(out_pdf)
        fin_year = live_year or "2025-26"
        write_status(out_pdf, pan, fin_year, "live-download")
        log(f"[DONE] live AIS download saved to {out_pdf}")
        return

    # If we got here, the live portal path did not yield a new PDF.
    # Fall back to the most recent cached AIS PDF for the same PAN.
    out_pdf = copy_fallback_pdf(pan)
    fin_year = live_year or "2025-26"
    write_status(out_pdf, pan, fin_year, "cached-fallback")
    log(f"[DONE] cached AIS PDF saved to {out_pdf}")


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        log(f"[FATAL] {type(exc).__name__}: {exc}")
        # Make the failure visible to the retry/fix loop.
        status_path = OUTPUT_DIR / "ais_download_status.json"
        try:
            OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
            with status_path.open("w", encoding="utf-8") as f:
                json.dump(
                    {
                        "download_success": False,
                        "pdf_file": "",
                        "error": f"{type(exc).__name__}: {exc}",
                    },
                    f,
                    indent=2,
                )
        except Exception as write_exc:
            print(f"[FATAL] failed to write status file: {write_exc}", flush=True)
        raise
