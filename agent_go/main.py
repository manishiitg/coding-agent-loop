import json
import os
import re
import time
from dataclasses import dataclass
from datetime import datetime, timezone, timedelta
from pathlib import Path
from typing import Any, Callable, Dict, List, Optional

import requests


VERBOSE = os.environ.get("SCRIPT_VERBOSE", "") == "1"

STEP_OUTPUT_DIR = Path(os.environ["STEP_OUTPUT_DIR"])
KB_DIR = Path("/app/workspace-docs/Workflow/rts-website/knowledgebase")
AUTH_RESULTS_PATH = STEP_OUTPUT_DIR / "auth_results.json"
BUGS_PATH = KB_DIR / "bugs.json"
COVERAGE_PATH = KB_DIR / "test_coverage.json"

BASE_URL = os.environ["VAR_RTS_URL"].rstrip("/")
EMAIL = os.environ["VAR_RTS_EMAIL"]
PASSWORD = os.environ["VAR_RTS_PASSWORD"]
MCP_API_URL = os.environ["MCP_API_URL"].rstrip("/")
MCP_API_TOKEN = os.environ["MCP_API_TOKEN"]
BROWSER_SESSION = os.environ["MCP_SESSION_ID"]

LOCAL_TZ = timezone(timedelta(hours=5, minutes=30))
RUN_NUMBER = 1


class TestFailure(Exception):
    def __init__(self, message: str, severity: str = "High", bug_description: Optional[str] = None):
        super().__init__(message)
        self.severity = severity
        self.bug_description = bug_description or message


class SkipTest(Exception):
    pass


def now_iso() -> str:
    return datetime.now(LOCAL_TZ).isoformat()


def log(msg: str) -> None:
    print(msg)


def vlog(msg: str) -> None:
    if VERBOSE:
        print(msg)


def mcp_request(command: str, args: Optional[List[str]] = None, retries: int = 3) -> Dict[str, Any]:
    url = MCP_API_URL + "/tools/custom/agent_browser"
    headers = {
        "Authorization": f"Bearer {MCP_API_TOKEN}",
        "Content-Type": "application/json",
    }
    payload = {
        "session": BROWSER_SESSION,
        "command": command,
        "args": args or [],
    }
    last_error: Optional[Exception] = None
    for attempt in range(retries):
        try:
            if VERBOSE:
                vlog(f"[BROWSER] >> {command} {json.dumps(payload['args'])}")
            resp = requests.post(url, json=payload, headers=headers, timeout=120)
            resp.raise_for_status()
            body = resp.json()
            if not body.get("success"):
                error = body.get("error", "unknown error")
                if VERBOSE:
                    vlog(f"[BROWSER] !! {command} failed: {error}")
                lowered = str(error).lower()
                if any(token in lowered for token in ("broken pipe", "connection reset", "transport closed")):
                    last_error = RuntimeError(str(error))
                    if attempt < retries - 1:
                        time.sleep(2 * (attempt + 1))
                        continue
                raise RuntimeError(error)
            data = body.get("data", {})
            if VERBOSE:
                vlog(f"[BROWSER] << {command} ok")
            return data
        except (requests.exceptions.ConnectionError, requests.exceptions.Timeout) as exc:
            last_error = exc
            if VERBOSE:
                vlog(f"[BROWSER] !! {command} attempt {attempt + 1} error: {exc}")
            if attempt < retries - 1:
                time.sleep(2 * (attempt + 1))
    if last_error:
        raise last_error
    raise RuntimeError(f"browser command failed: {command}")


def browser_open(url: str) -> Dict[str, Any]:
    return mcp_request("open", [url])


def browser_close() -> None:
    try:
        mcp_request("close", [])
    except Exception as exc:
        vlog(f"[BROWSER] close ignored: {exc}")


def browser_wait(ms: int) -> Dict[str, Any]:
    return mcp_request("wait", [str(ms)])


def browser_reload() -> Dict[str, Any]:
    return mcp_request("reload", [])


def browser_get(what: str) -> Any:
    return mcp_request("get", [what])


def browser_snapshot() -> Dict[str, Any]:
    data = mcp_request("snapshot", ["-i"])
    if VERBOSE:
        vlog("[SNAPSHOT]\n" + data.get("snapshot", "")[:4000])
    return data


def browser_click_ref(ref: str) -> Dict[str, Any]:
    return mcp_request("click", [f"@{ref}"])


def browser_fill_ref(ref: str, value: str) -> Dict[str, Any]:
    return mcp_request("fill", [f"@{ref}", value])


def browser_eval(js: str) -> Any:
    return mcp_request("eval", [js])


def snapshot_ref(snapshot: Dict[str, Any], name: str, role: Optional[str] = None) -> str:
    refs = snapshot.get("refs", {})
    for ref, meta in refs.items():
        if meta.get("name") == name and (role is None or meta.get("role") == role):
            return ref
    raise KeyError(f"Could not find ref for name={name!r} role={role!r}")


def page_url() -> str:
    return browser_get("url")["url"]


def page_text() -> str:
    return browser_eval("document.body.innerText")["result"]


def alert_texts() -> List[str]:
    result = browser_eval(
        "Array.from(document.querySelectorAll('[role=\"alert\"], .mud-alert, .validation-message, .mud-input-helper-text, .mud-snackbar'))"
        ".map(el => (el.textContent || '').trim()).filter(Boolean)"
    )["result"]
    return result if isinstance(result, list) else []


def input_types() -> List[str]:
    result = browser_eval("Array.from(document.querySelectorAll('input')).map(el => el.type)")["result"]
    return result if isinstance(result, list) else []


def wait_for(predicate: Callable[[], bool], timeout_s: int, label: str) -> None:
    deadline = time.time() + timeout_s
    last_state = None
    while time.time() < deadline:
        try:
            if predicate():
                return
        except Exception as exc:
            last_state = exc
        browser_wait(1000)
    raise RuntimeError(f"Timed out waiting for {label}. Last state: {last_state}")


def wait_for_login_page() -> None:
    def _ready() -> bool:
        url = page_url()
        if not url.endswith("/login"):
            return False
        text = page_text()
        return "Login" in text and "LOGIN" in text and "Forgot Password" in text

    wait_for(_ready, 90, "login page")


def wait_for_forgot_password_page() -> None:
    def _ready() -> bool:
        url = page_url()
        if not url.endswith("/forgot-password"):
            return False
        text = page_text()
        return "Forgot Password" in text and "SEND RECOVERY EMAIL" in text

    wait_for(_ready, 60, "forgot password page")


def wait_for_home_page() -> None:
    def _ready() -> bool:
        url = page_url()
        if url.rstrip("/") != BASE_URL.rstrip("/"):
            return False
        text = page_text()
        return "What would you like to do today" in text and "JOIN SESSION" in text and "CREATE SESSION" in text

    wait_for(_ready, 90, "home page")


def wait_for_intro_page() -> None:
    def _ready() -> bool:
        url = page_url()
        if not url.endswith("/intro"):
            return False
        text = page_text()
        return "Login" in text and "Register" in text

    wait_for(_ready, 60, "intro page")


def prepare_login_page() -> Dict[str, Any]:
    browser_open(BASE_URL + "/login")
    wait_for_login_page()
    snap = browser_snapshot()
    vlog(f"[STATE] login page url={page_url()}")
    return snap


def submit_login(email: str, password: str) -> None:
    snap = browser_snapshot()
    email_ref = snapshot_ref(snap, "Email", "textbox")
    password_ref = snapshot_ref(snap, "Password", "textbox")
    login_ref = snapshot_ref(snap, "LOGIN", "button")
    browser_fill_ref(email_ref, email)
    browser_fill_ref(password_ref, password)
    try:
        browser_click_ref(login_ref)
    except Exception as exc:
        vlog(f"[LOGIN] native click failed, falling back to DOM click: {exc}")
        browser_eval(
            "(() => {"
            " const btn = Array.from(document.querySelectorAll('button')).find(el => (el.textContent || '').trim() === 'LOGIN');"
            " if (!btn) return 'missing';"
            " btn.click();"
            " return 'clicked';"
            "})()"
        )


def record(test_name: str, steps: List[str], expected: str, actual: str, status: str, severity: Optional[str] = None, bug_description: Optional[str] = None) -> Dict[str, Any]:
    entry: Dict[str, Any] = {
        "test_name": test_name,
        "steps_taken": steps,
        "expected_behavior": expected,
        "actual_behavior": actual,
        "status": status,
    }
    if status == "FAIL":
        entry["severity"] = severity or "High"
        entry["bug_description"] = bug_description or actual
    else:
        entry["severity"] = None
        entry["bug_description"] = None
    return entry


def run_test(test_name: str, steps: List[str], expected: str, func: Callable[[], str]) -> Dict[str, Any]:
    log(f"[TEST] {test_name}")
    try:
        actual = func()
        log(f"[TEST] {test_name}: PASS")
        return record(test_name, steps, expected, actual, "PASS")
    except SkipTest as exc:
        log(f"[TEST] {test_name}: SKIP - {exc}")
        return record(test_name, steps, expected, str(exc), "SKIP")
    except TestFailure as exc:
        log(f"[TEST] {test_name}: FAIL - {exc}")
        return record(test_name, steps, expected, str(exc), "FAIL", severity=exc.severity, bug_description=exc.bug_description)
    except Exception as exc:
        log(f"[TEST] {test_name}: FAIL - unexpected error: {exc}")
        return record(test_name, steps, expected, str(exc), "FAIL", severity="High", bug_description=str(exc))


def test_invalid_password() -> str:
    prepare_login_page()
    submit_login(EMAIL, "wrong-password")
    wait_for(lambda: page_url().endswith("/login") and "Invalid login credentials." in alert_texts(), 30, "invalid password error")
    errors = alert_texts()
    url = page_url()
    if "Invalid login credentials." not in errors:
        raise TestFailure(f"Expected invalid credentials error, got {errors}", severity="Medium")
    return f"Stayed on {url}; alerts={errors}; session remained logged out."


def test_invalid_email() -> str:
    prepare_login_page()
    submit_login("nobody@example.com", "whatever")
    wait_for(lambda: page_url().endswith("/login") and "Invalid login credentials." in alert_texts(), 30, "invalid email error")
    errors = alert_texts()
    url = page_url()
    if "Invalid login credentials." not in errors:
        raise TestFailure(f"Expected invalid credentials error, got {errors}", severity="Medium")
    return f"Stayed on {url}; alerts={errors}; session remained logged out."


def test_empty_fields() -> str:
    prepare_login_page()
    snap = browser_snapshot()
    login_ref = snapshot_ref(snap, "LOGIN", "button")
    browser_click_ref(login_ref)
    wait_for(lambda: page_url().endswith("/login") and bool(alert_texts()), 30, "blank-field validation")
    errors = alert_texts()
    url = page_url()
    if not errors:
        raise TestFailure("No validation message appeared for blank login submission.", severity="Medium")
    return f"Submitted blank form on {url}; alerts={errors}; session remained logged out."


def test_show_password_toggle() -> str:
    snap = prepare_login_page()
    before_types = input_types()
    toggle_ref = snapshot_ref(snap, "Show Password", "button")
    browser_click_ref(toggle_ref)
    wait_for(lambda: "Hide Password" in page_text(), 15, "password reveal toggle")
    after_types = input_types()
    after_snap = browser_snapshot()
    toggle_label = "Hide Password"
    if len(after_types) < 2 or after_types[1] != "text":
        raise TestFailure(f"Password field did not switch to text. Before={before_types}, after={after_types}", severity="Medium")
    try:
        snapshot_ref(after_snap, toggle_label, "button")
    except KeyError:
        raise TestFailure(f"Toggle label did not change to {toggle_label!r}. Snapshot text: {after_snap.get('snapshot', '')[:800]}", severity="Low")
    return f"Password field types changed from {before_types} to {after_types}; button label changed to Hide Password."


def test_forgot_password_link() -> str:
    snap = prepare_login_page()
    link_ref = snapshot_ref(snap, "Forgot Password", "link")
    browser_click_ref(link_ref)
    wait_for_forgot_password_page()
    url = page_url()
    text = page_text()
    if not url.endswith("/forgot-password") or "SEND RECOVERY EMAIL" not in text:
        raise TestFailure(f"Forgot password page did not load correctly. url={url}, text={text[:500]}", severity="Medium")
    return f"Clicked Forgot Password and landed on {url}; recovery form visible."


def test_direct_url_logged_out() -> str:
    browser_open(BASE_URL + "/single-player-sim")
    wait_for_intro_page()
    url = page_url()
    text = page_text()
    if not url.endswith("/intro"):
        raise TestFailure(f"Expected redirect to /intro while logged out, got {url}", severity="High")
    if "Login" not in text or "Register" not in text:
        raise TestFailure(f"Intro page text missing after redirect. text={text[:500]}", severity="Medium")
    return f"Opening /single-player-sim while logged out redirected to {url}."


def test_valid_login() -> str:
    prepare_login_page()
    submit_login(EMAIL, PASSWORD)
    wait_for_home_page()
    url = page_url()
    text = page_text()
    if "What would you like to do today" not in text:
        raise TestFailure(f"Landing greeting missing after login. url={url}, text={text[:500]}", severity="High")
    if "LOGIN" in text and url.endswith("/login"):
        raise TestFailure("Still on the login page after valid credentials.", severity="High")
    return f"Landed on {url} with greeting: {text.splitlines()[0] if text else 'n/a'}."


def test_session_persistence() -> str:
    browser_reload()
    wait_for_home_page()
    url = page_url()
    text = page_text()
    if "What would you like to do today" not in text:
        raise TestFailure(f"Session did not persist after refresh. url={url}, text={text[:500]}", severity="High")
    return f"Refreshed {url} and remained authenticated with greeting still visible."


def test_logout() -> str:
    browser_eval(
        "(() => { const el = document.querySelector('[aria-label=\"Sign Out\"] .mud-nav-link');"
        " if (!el) return 'missing'; el.click(); return 'clicked'; })()"
    )
    wait_for(lambda: page_url().endswith("/login") and "Forgot Password" in page_text(), 30, "logout redirect")
    url = page_url()
    text = page_text()
    if not url.endswith("/login"):
        raise TestFailure(f"Logout did not redirect to login. url={url}", severity="High")
    if "Forgot Password" not in text or "LOGIN" not in text:
        raise TestFailure(f"Login page not visible after logout. text={text[:500]}", severity="Medium")
    return f"Logged out to {url}; login form visible and session state cleared."


def read_existing_bugs() -> Dict[str, Any]:
    if not BUGS_PATH.exists():
        return {"bugs": []}
    with BUGS_PATH.open("r", encoding="utf-8") as f:
        data = json.load(f)
    if not isinstance(data, dict) or "bugs" not in data or not isinstance(data["bugs"], list):
        return {"bugs": []}
    return data


def write_json(path: Path, data: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(data, f, indent=2, ensure_ascii=True)
        f.write("\n")


def update_coverage() -> None:
    if not COVERAGE_PATH.exists():
        vlog(f"[KB] coverage file missing: {COVERAGE_PATH}")
        return
    with COVERAGE_PATH.open("r", encoding="utf-8") as f:
        coverage = json.load(f)
    todo = coverage.get("todo_list")
    if not isinstance(todo, list):
        vlog("[KB] coverage file missing todo_list; skipping update")
        return
    for item in todo:
        if item.get("flow_id") in {"flow-intro", "flow-login", "flow-home-lobby"}:
            item["last_tested_run"] = RUN_NUMBER
    coverage["generated_at"] = now_iso()
    write_json(COVERAGE_PATH, coverage)
    vlog(f"[KB] updated coverage file at {COVERAGE_PATH}")


def append_bugs(results: List[Dict[str, Any]]) -> None:
    failing = [r for r in results if r["status"] == "FAIL"]
    if not failing:
        if not BUGS_PATH.exists():
            write_json(BUGS_PATH, {"bugs": []})
            vlog(f"[KB] created empty bugs file at {BUGS_PATH}")
        return

    existing = read_existing_bugs()
    bugs = existing.get("bugs", [])
    next_num = 1
    for bug in bugs:
        match = re.match(r"BUG-(\d+)", str(bug.get("id", "")))
        if match:
            next_num = max(next_num, int(match.group(1)) + 1)

    for result in failing:
        bugs.append(
            {
                "id": f"BUG-{next_num:03d}",
                "title": result["test_name"],
                "area": "auth",
                "severity": result.get("severity", "High"),
                "status": "open",
                "discovered_run": RUN_NUMBER,
                "steps_to_reproduce": result["steps_taken"],
                "expected": result["expected_behavior"],
                "actual": result["actual_behavior"],
            }
        )
        next_num += 1

    write_json(BUGS_PATH, {"bugs": bugs})
    vlog(f"[KB] appended {len(failing)} bug(s) to {BUGS_PATH}")


def main() -> None:
    STEP_OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    KB_DIR.mkdir(parents=True, exist_ok=True)

    log("[BOOT] starting auth QA run")
    browser_close()
    browser_open(BASE_URL + "/login")
    wait_for_login_page()
    log(f"[STATE] initial url={page_url()}")
    if VERBOSE:
        vlog(page_text()[:1000])

    tests = [
        (
            "Invalid password",
            [
                f"Open {BASE_URL}/login",
                f"Fill email with {EMAIL}",
                "Fill password with an incorrect value",
                "Submit LOGIN",
                "Verify an auth error and stay logged out",
            ],
            "The app should reject an incorrect password and keep the user logged out.",
            test_invalid_password,
        ),
        (
            "Invalid email",
            [
                f"Open {BASE_URL}/login",
                "Fill a non-existent email address",
                "Fill password with any value",
                "Submit LOGIN",
                "Verify an auth error and stay logged out",
            ],
            "The app should reject a non-existent email and keep the user logged out.",
            test_invalid_email,
        ),
        (
            "Empty fields",
            [
                f"Open {BASE_URL}/login",
                "Leave email and password blank",
                "Submit LOGIN",
                "Verify validation or rejection of blank credentials",
            ],
            "The app should prevent blank credential submission and show validation feedback.",
            test_empty_fields,
        ),
        (
            "Show password toggle",
            [
                f"Open {BASE_URL}/login",
                "Click Show Password",
                "Confirm the password field reveals text and the label changes to Hide Password",
            ],
            "The password reveal control should toggle the password input between masked and plain text.",
            test_show_password_toggle,
        ),
        (
            "Forgot Password link",
            [
                f"Open {BASE_URL}/login",
                "Click Forgot Password",
                "Verify the recovery page loads",
            ],
            "The forgot-password link should be present and open the recovery flow.",
            test_forgot_password_link,
        ),
        (
            "Direct URL while logged out",
            [
                f"Ensure the user is logged out",
                f"Open {BASE_URL}/single-player-sim",
                "Verify the redirect destination",
            ],
            "Direct access to the simulator should redirect away when the user is not authenticated.",
            test_direct_url_logged_out,
        ),
        (
            "Valid login",
            [
                f"Open {BASE_URL}/login",
                f"Fill email with {EMAIL}",
                "Fill password with the injected RTS password",
                "Submit LOGIN",
                "Wait for the home lobby greeting",
            ],
            "Successful authentication should land on the home lobby and show a username greeting.",
            test_valid_login,
        ),
        (
            "Session persistence",
            [
                "Refresh the page after a successful login",
                "Verify the home lobby and greeting remain visible",
            ],
            "A valid login should survive a page refresh in the same browser session.",
            test_session_persistence,
        ),
        (
            "Logout",
            [
                "Click Sign Out from the account menu",
                "Wait for the login page to reappear",
                "Verify the session is cleared",
            ],
            "Logout should end the session and return the browser to the login surface.",
            test_logout,
        ),
    ]

    results: List[Dict[str, Any]] = []
    for name, steps, expected, func in tests:
        result = run_test(name, steps, expected, func)
        results.append(result)
        if name == "Direct URL while logged out":
            browser_open(BASE_URL + "/login")
            wait_for_login_page()

    summary = {
        "generated_at": now_iso(),
        "area": "auth",
        "focus": "valid and invalid login, empty fields, show password toggle, forgot password, logout, session persistence, and logged-out redirect behavior",
        "base_url": BASE_URL,
        "tests": results,
        "summary": {
            "pass": sum(1 for r in results if r["status"] == "PASS"),
            "fail": sum(1 for r in results if r["status"] == "FAIL"),
            "skip": sum(1 for r in results if r["status"] == "SKIP"),
        },
    }

    write_json(AUTH_RESULTS_PATH, summary)
    update_coverage()
    append_bugs(results)
    browser_close()
    log(f"[DONE] wrote {AUTH_RESULTS_PATH}")


if __name__ == "__main__":
    main()
