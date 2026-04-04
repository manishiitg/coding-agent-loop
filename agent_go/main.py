import json
import os
import re
import sys
import time
from html import unescape
from urllib.parse import urlparse

import requests


def call_mcp(server, tool, args, retries=3, backoff=2):
    url = os.environ["MCP_API_URL"] + f"/tools/mcp/{server}/{tool}"
    headers = {
        "Authorization": f"Bearer {os.environ['MCP_API_TOKEN']}",
        "Content-Type": "application/json",
    }
    last_err = None
    for attempt in range(retries):
        try:
            resp = requests.post(url, json=args, headers=headers, timeout=120)
            resp.raise_for_status()
            result = resp.json()
            if not result.get("success"):
                err = str(result.get("error", ""))
                if (
                    "broken pipe" in err.lower()
                    or "connection reset" in err.lower()
                    or "transport closed" in err.lower()
                ):
                    last_err = RuntimeError(f"MCP broken pipe: {err}")
                    if attempt < retries - 1:
                        time.sleep(backoff * (attempt + 1))
                        continue
                raise RuntimeError(f"MCP error: {err}")
            return result["result"]
        except (requests.exceptions.ConnectionError, requests.exceptions.Timeout) as e:
            last_err = e
            if attempt < retries - 1:
                time.sleep(backoff * (attempt + 1))
    raise last_err


def as_text(value):
    if isinstance(value, str):
        return value
    try:
        return json.dumps(value, ensure_ascii=True)
    except Exception:
        return str(value)


def extract_url(text):
    if text is None:
        return ""
    raw = as_text(text)
    raw = unescape(raw)
    m = re.search(r"https?://[^\s\"'<>()]+", raw)
    if not m:
        return ""
    url = m.group(0).rstrip("\\").rstrip('"').rstrip("'")
    return url


def clean_url(url):
    if not url:
        return ""
    parsed = urlparse(url)
    cleaned = f"{parsed.scheme}://{parsed.netloc}{parsed.path}"
    return cleaned.rstrip("/")


def current_url():
    result = call_mcp("playwright", "browser_evaluate", {"function": "() => window.location.href"})
    url = extract_url(result)
    if not url:
        url = as_text(result).strip().strip('"').strip("'")
    return clean_url(url)


def page_snapshot():
    return as_text(call_mcp("playwright", "browser_snapshot", {"depth": 8}))


def find_ref(snapshot_text, roles, keywords):
    roles = [r.lower() for r in roles or []]
    keywords = [k.lower() for k in keywords or []]
    for raw in snapshot_text.splitlines():
        line = raw.strip()
        low = line.lower()
        if "[ref=" not in line:
            continue
        if roles and not any(role in low for role in roles):
            continue
        if keywords and not any(keyword in low for keyword in keywords):
            continue
        m = re.search(r"\[ref=([^\]]+)\]", line)
        if m:
            return m.group(1), line
    return None, None


def press_enter():
    try:
        call_mcp("playwright", "browser_press_key", {"key": "Enter"})
    except Exception:
        pass


def write_output(output_dir, data):
    os.makedirs(output_dir, exist_ok=True)
    output_path = os.path.join(output_dir, "step_3_login_status.json")
    with open(output_path, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=2)
    return output_path


def fill_user_id(user_id):
    js = f"""() => {{
        try {{
            const frames = [window.frames[0], window];
            const selectors = [
                "input[name='fldLoginUserId']",
                "#fldLoginUserId",
                "input[id*='fldLoginUserId']",
            ];
            for (const ctx of frames) {{
                if (!ctx || !ctx.document) continue;
                for (const sel of selectors) {{
                    const el = ctx.document.querySelector(sel);
                    if (el) {{
                        try {{ el.removeAttribute('disabled'); }} catch (e) {{}}
                        try {{ el.removeAttribute('readonly'); }} catch (e) {{}}
                        el.focus();
                        el.value = {json.dumps(user_id)};
                        el.dispatchEvent(new Event('input', {{ bubbles: true }}));
                        el.dispatchEvent(new Event('change', {{ bubbles: true }}));
                        return 'filled user id via ' + sel;
                    }}
                }}
            }}
            return 'user id field not found';
        }} catch (e) {{
            return 'error: ' + e;
        }}
    }}"""
    return call_mcp("playwright", "browser_evaluate", {"function": js})


def click_continue():
    js = """() => {
        try {
            const frame = window.frames[0];
            const doc = frame && frame.document ? frame.document : document;
            const candidates = doc.querySelectorAll("a, button, input[type='submit'], input[type='button']");
            for (const btn of candidates) {
                const txt = (btn.textContent || btn.value || "").toLowerCase();
                const id = (btn.id || "").toLowerCase();
                const cls = (btn.className || "").toLowerCase();
                if (txt.includes("continue") || id.includes("continue") || cls.includes("continue")) {
                    btn.click();
                    return "clicked continue: " + (btn.textContent || btn.value || btn.id);
                }
            }
            const form = doc.querySelector("form");
            if (form) {
                form.submit();
                return "submitted form";
            }
            return "continue not found";
        } catch (e) {
            return "error: " + e;
        }
    }"""
    return call_mcp("playwright", "browser_evaluate", {"function": js})


def wait_for_url_change(before_url, timeout=25):
    end = time.time() + timeout
    last = before_url
    while time.time() < end:
        try:
            last = current_url()
        except Exception:
            pass
        if last and last != before_url:
            return last
        time.sleep(1)
    return last


def keycloak_password_flow(password):
    deadline = time.time() + 30
    while time.time() < deadline:
        snap = page_snapshot()
        ref, line = find_ref(snap, ["textbox"], ["password"])
        if ref:
            call_mcp("playwright", "browser_click", {"ref": ref, "element": line})
            call_mcp(
                "playwright",
                "browser_type",
                {
                    "ref": ref,
                    "element": line,
                    "text": password,
                    "slowly": True,
                    "submit": False,
                },
            )
            press_enter()
            time.sleep(1)
            login_snap = page_snapshot()
            login_ref, login_line = find_ref(login_snap, ["button"], ["login"])
            if login_ref:
                call_mcp("playwright", "browser_click", {"ref": login_ref, "element": login_line})
                time.sleep(1)
            return True
        time.sleep(1)
    return False


def extract_message_ids(search_result):
    ids = []
    if isinstance(search_result, list):
        for item in search_result:
            if isinstance(item, dict) and item.get("id"):
                ids.append(item["id"])
    elif isinstance(search_result, dict):
        for key in ("messages", "data", "results", "emails"):
            value = search_result.get(key)
            if isinstance(value, list):
                for item in value:
                    if isinstance(item, dict) and item.get("id"):
                        ids.append(item["id"])
    return ids


def text_from_email_payload(payload):
    if payload is None:
        return ""
    if isinstance(payload, str):
        return payload
    if isinstance(payload, dict):
        parts = []
        for key in ("snippet", "subject", "body", "text", "html"):
            val = payload.get(key)
            if val:
                parts.append(as_text(val))
        for key in ("payload", "message", "data"):
            val = payload.get(key)
            if val:
                parts.append(text_from_email_payload(val))
        for val in payload.values():
            if isinstance(val, (dict, list)):
                parts.append(text_from_email_payload(val))
            elif isinstance(val, str):
                parts.append(val)
        return "\n".join(parts)
    if isinstance(payload, list):
        return "\n".join(text_from_email_payload(item) for item in payload)
    return str(payload)


def find_otp_code():
    queries = [
        'from:hdfcbank OTP newer_than:30m',
        'from:hdfcbank "One Time Password" newer_than:30m',
        'hdfc OTP newer_than:30m',
        'subject:OTP newer_than:30m',
        'OTP newer_than:30m',
    ]
    for query in queries:
        try:
            search_result = call_mcp("gmail", "search_emails", {"query": query, "maxResults": 10})
        except Exception:
            continue
        ids = extract_message_ids(search_result)
        if not ids and isinstance(search_result, (dict, list)):
            payload_text = text_from_email_payload(search_result)
            m = re.search(r"\b(\d{6})\b", payload_text)
            if m:
                return m.group(1)
        for msg_id in ids:
            try:
                email = call_mcp("gmail", "read_email", {"messageId": msg_id})
            except Exception:
                continue
            body = text_from_email_payload(email)
            m = re.search(r"\b(\d{6})\b", body)
            if m:
                return m.group(1)
    return None


def handle_otp_if_needed():
    snap = page_snapshot().lower()
    otp_markers = [
        "otp",
        "one time",
        "verify",
        "authentication",
        "secure access",
        "transaction password",
        "send otp",
        "email otp",
        "registered mobile",
    ]
    if not any(marker in snap for marker in otp_markers):
        return False

    snap_raw = page_snapshot()
    email_ref, email_line = find_ref(snap_raw, ["button", "link"], ["email", "send otp"])
    if email_ref:
        try:
            call_mcp("playwright", "browser_click", {"ref": email_ref, "element": email_line})
            time.sleep(2)
        except Exception:
            pass

    otp_code = find_otp_code()
    if not otp_code:
        return False

    snap_raw = page_snapshot()
    otp_ref, otp_line = find_ref(snap_raw, ["textbox"], ["otp", "code", "passcode", "verification"])
    if not otp_ref:
        otp_ref, otp_line = find_ref(snap_raw, ["textbox"], [])
    if otp_ref:
        call_mcp("playwright", "browser_click", {"ref": otp_ref, "element": otp_line})
        call_mcp(
            "playwright",
            "browser_type",
            {
                "ref": otp_ref,
                "element": otp_line,
                "text": otp_code,
                "slowly": True,
                "submit": False,
            },
        )
        press_enter()
        time.sleep(1)

    submit_snap = page_snapshot()
    submit_ref, submit_line = find_ref(submit_snap, ["button"], ["verify", "continue", "submit", "login", "next"])
    if submit_ref:
        try:
            call_mcp("playwright", "browser_click", {"ref": submit_ref, "element": submit_line})
        except Exception:
            pass
    time.sleep(4)
    return True


def url_indicates_success(url):
    if not url:
        return False
    return (
        "now.hdfc.bank.in/retail-app" in url
        or "now.hdfc.bank.in/accounts" in url
    ) and "openid-connect/auth" not in url


def dashboard_marker_present(snapshot_text):
    low = snapshot_text.lower()
    markers = [
        "accounts",
        "account summary",
        "my accounts",
        "select context",
        "dashboard",
        "available balance",
        "transactions",
    ]
    return any(marker in low for marker in markers)


def final_dashboard_url():
    raw_url = current_url()
    if raw_url:
        return raw_url
    snap = page_snapshot()
    return clean_url(extract_url(snap))


def main():
    input_file = sys.argv[1]
    with open(input_file, "r", encoding="utf-8") as f:
        creds = json.load(f)

    password = creds["password"]
    user_id = os.environ["VAR_USER_ID"]
    output_dir = os.environ["STEP_OUTPUT_DIR"]

    login_status = "failed"
    dashboard_url = ""
    current = ""

    try:
        call_mcp("playwright", "browser_navigate", {"url": "https://netbanking.hdfcbank.com/netbanking/"})
        time.sleep(3)

        fill_user_id(user_id)
        time.sleep(1)
        click_continue()
        before = current_url()
        current = wait_for_url_change(before, timeout=20)

        if "now.hdfc.bank.in" not in current:
            snap = page_snapshot()
            found_login = "sign in to retail" in snap.lower() or "password" in snap.lower()
            if not found_login:
                current = current_url()

        if "now.hdfc.bank.in" in current or "signin" in current.lower() or "retail" in current.lower():
            keycloak_password_flow(password)

        for _ in range(6):
            time.sleep(3)
            current = current_url()
            snap = page_snapshot()
            if url_indicates_success(current):
                if dashboard_marker_present(snap):
                    login_status = "success"
                    dashboard_url = current
                    break
                # URL is already on the dashboard path; treat this as success if it is stable.
                if "retail-app" in current or "accounts" in current:
                    login_status = "success"
                    dashboard_url = current
                    break

            if handle_otp_if_needed():
                current = current_url()
                if url_indicates_success(current):
                    login_status = "success"
                    dashboard_url = current
                    break

        if login_status != "success":
            # One last recovery pass: if we're on the correct HDFC path, accept it.
            current = current_url()
            if url_indicates_success(current):
                login_status = "success"
                dashboard_url = current

        if login_status != "success":
            write_output(
                output_dir,
                {
                    "login_status": "failed",
                    "user_id": user_id,
                    "final_url": current,
                },
            )
            sys.exit(1)

        # Final confirmation snapshot for the output path.
        if not dashboard_url:
            dashboard_url = final_dashboard_url()

        write_output(
            output_dir,
            {
                "login_status": "success",
                "user_id": user_id,
                "dashboard_url": dashboard_url,
            },
        )
    except SystemExit:
        raise
    except Exception:
        write_output(
            output_dir,
            {
                "login_status": "failed",
                "user_id": user_id,
                "final_url": current or "",
            },
        )
        raise


if __name__ == "__main__":
    main()
