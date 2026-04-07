import json
import os
import time
import traceback
from pathlib import Path

import requests


MCP_URL = os.environ["MCP_API_URL"].rstrip("/")
TOKEN = os.environ["MCP_API_TOKEN"]
BASE = f"{MCP_URL}/tools/custom/call_sub_agent"
HEADERS = {
    "Authorization": f"Bearer {TOKEN}",
    "Content-Type": "application/json",
}

ROOT = Path("/app/workspace-docs/Workflow/check-form-26as-xspaces/runs/iteration-0/xspaces/execution/check-form-26as-automation")
LOG_PATH = ROOT / "it_portal_orchestrator.log"
STATUS_PATH = ROOT / "it_portal_orchestrator_status.json"


def log(message: str) -> None:
    line = f"{time.strftime('%Y-%m-%dT%H:%M:%S%z')} {message}"
    print(line, flush=True)
    ROOT.mkdir(parents=True, exist_ok=True)
    with LOG_PATH.open("a", encoding="utf-8") as fh:
        fh.write(line + "\n")


def write_status(data: dict) -> None:
    ROOT.mkdir(parents=True, exist_ok=True)
    STATUS_PATH.write_text(json.dumps(data, indent=2, ensure_ascii=True) + "\n", encoding="utf-8")


def call_route(route_id: str, todo_id: str, instructions: str, preferred_tier: int = 2) -> dict:
    payload = {
        "route_id": route_id,
        "todo_id": todo_id,
        "instructions": instructions,
        "share_browser": True,
        "preferred_tier": preferred_tier,
    }
    log(f"START route={route_id} todo_id={todo_id}")
    resp = requests.post(BASE, json=payload, headers=HEADERS, timeout=5400)
    try:
        data = resp.json()
    except Exception:
        data = {"success": False, "error": "non-json response", "raw": resp.text}
    log(f"END route={route_id} http={resp.status_code} success={data.get('success')}")
    return {"http_status": resp.status_code, "response": data}


def parse_result_payload(response: dict) -> dict:
    result = response.get("response", {}).get("result")
    if isinstance(result, dict):
        return result
    if isinstance(result, str):
        try:
            return json.loads(result)
        except Exception:
            return {"raw_result": result}
    return {"raw_result": result}


def main() -> int:
    ROOT.mkdir(parents=True, exist_ok=True)
    LOG_PATH.write_text("", encoding="utf-8")
    status = {"started_at": time.time(), "steps": []}
    write_status(status)

    steps = [
        {
            "route_id": "it-portal-login",
            "todo_id": "task-login-001",
            "preferred_tier": 1,
            "instructions": (
                'Use the shared browser session for the portal and keep it treated as session="it_portal". '
                "Read the credentials from /app/workspace-docs/Workflow/check-form-26as-xspaces/runs/iteration-0/xspaces/execution/fetch-it-password/credentials.json. "
                "Use PAN AAAFX2962N and password Etpl#2024, ignoring the date suffix in the PAN label. "
                "Log into the Income Tax portal, handling secure access checkboxes and any dual-login popup correctly. "
                "Verify you reach the dashboard URL before writing output. Capture the tab_id. "
                "Write login_status.json in the route output folder with the required fields. "
                "If you encounter an Invalid Password error, stop immediately and record login failure rather than retrying."
            ),
        },
        {
            "route_id": "extract-profile-details",
            "todo_id": "task-profile-002",
            "preferred_tier": 2,
            "instructions": (
                'Continue in the same shared browser session session="it_portal". '
                "Extract the registered Phone Number and Email Address from My Profile or the dashboard. "
                "Write profile_details.json in the route output folder."
            ),
        },
        {
            "route_id": "download-form-26as",
            "todo_id": "task-form26as-003",
            "preferred_tier": 1,
            "instructions": (
                'Continue in the same shared browser session session="it_portal". '
                "From the logged-in dashboard, navigate to e-File -> Income Tax Returns -> View Form 26AS. "
                "Accept the disclaimer in TRACES, then download the HTML view exported as PDF for the 3 most recent Assessment Years. "
                "Save each downloaded file to the step output area and produce download_status.json."
            ),
        },
        {
            "route_id": "extract-tax-data",
            "todo_id": "task-taxdata-004",
            "preferred_tier": 2,
            "instructions": (
                'Continue in the same shared browser session session="it_portal". '
                "Extract the tabular TDS/TCS data from the currently loaded Form 26AS page for the last assessment year processed by the download step. "
                "Write the structured summary JSON to tax_data_extracted.json."
            ),
        },
        {
            "route_id": "download-ais-pdf",
            "todo_id": "task-ais-005",
            "preferred_tier": 1,
            "instructions": (
                'Continue in the same shared browser session session="it_portal". '
                "Navigate from the dashboard to Services -> Annual Information Statement (AIS). "
                "If the session expires, refresh or re-navigate back to the dashboard and try again. "
                "Read all available year options, select the latest available Financial/Assessment Year, and download the AIS PDF. "
                "Save it as ais.pdf in the step output path and write ais_download_status.json."
            ),
        },
        {
            "route_id": "check-e-proceedings",
            "todo_id": "task-eproc-006",
            "preferred_tier": 2,
            "instructions": (
                'Continue in the same shared browser session session="it_portal". '
                "Navigate to Pending Actions -> e-Proceedings. "
                "List all active notices or intimations. "
                "If any item has an attached PDF or document link, download it, copy it to the step output directory with a clear name, "
                "and write eproceedings_report.md plus eproceedings_status.json."
            ),
        },
        {
            "route_id": "check-worklist",
            "todo_id": "task-worklist-007",
            "preferred_tier": 2,
            "instructions": (
                'Continue in the same shared browser session session="it_portal". '
                "Check both the Worklist and For Your Information tabs. "
                "Capture all urgent items and all informational items with dates, statuses, and assessment years. "
                "Write worklist_report.md and worklist_status.json."
            ),
        },
        {
            "route_id": "check-outstanding-demand",
            "todo_id": "task-demand-008",
            "preferred_tier": 2,
            "instructions": (
                'Continue in the same shared browser session session="it_portal". '
                "Navigate to Pending Actions -> Response to Outstanding Demand and check for any tax demands. "
                "Write demand_report.md and demand_status.json."
            ),
        },
        {
            "route_id": "check-compliance",
            "todo_id": "task-compliance-009",
            "preferred_tier": 2,
            "instructions": (
                'Continue in the same shared browser session session="it_portal". '
                "Navigate to Compliance -> View and Submit Compliance and check for any notices such as defective notices or notice u/s 148. "
                "Write compliance_report.md and compliance_status.json."
            ),
        },
        {
            "route_id": "check-e-campaigns",
            "todo_id": "task-ecampaigns-010",
            "preferred_tier": 2,
            "instructions": (
                'Continue in the same shared browser session session="it_portal". '
                "Check Pending Actions -> Compliance Portal -> e-Campaigns, or the alternate Pending Actions -> e-Campaigns path if that is the UI variant. "
                "If a new tab opens, switch to it, collect the findings, then close it and return to the dashboard. "
                "Write ecampaigns_report.md and ecampaigns_status.json."
            ),
        },
        {
            "route_id": "check-grievances",
            "todo_id": "task-grievances-011",
            "preferred_tier": 2,
            "instructions": (
                'Continue in the same shared browser session session="it_portal". '
                "Navigate to Grievances -> View Grievance Status and collect any active or historical grievances. "
                "Write grievances_report.md and grievances_status.json."
            ),
        },
        {
            "route_id": "check-filed-returns",
            "todo_id": "task-returns-012",
            "preferred_tier": 2,
            "instructions": (
                'Continue in the same shared browser session session="it_portal". '
                "Navigate to e-File -> Income Tax Returns -> View Filed Returns. "
                "Capture the latest return details, refund status, filing date, and reported income, and download any Intimation u/s 143(1) PDFs if available. "
                "Write filed_returns_report.md and returns_status.json."
            ),
        },
        {
            "route_id": "summarize-pdfs",
            "todo_id": "task-summarize-013",
            "preferred_tier": 2,
            "instructions": (
                'Continue in the same shared browser session session="it_portal". '
                "Use the global PDF extraction script referenced by the route to summarize the downloaded Form 26AS PDF, AIS PDF, any Intimation PDFs, and any e-Proceedings PDFs. "
                "Save the summaries to pdf_summaries.txt and write pdf_summaries_status.json."
            ),
        },
    ]

    step_results = []
    login_ok = False
    for step in steps:
        try:
            response = call_route(
                step["route_id"],
                step["todo_id"],
                step["instructions"],
                preferred_tier=step["preferred_tier"],
            )
            parsed = parse_result_payload(response)
            step_record = {
                "route_id": step["route_id"],
                "todo_id": step["todo_id"],
                "http_status": response["http_status"],
                "response": response["response"],
                "parsed_result": parsed,
            }
            step_results.append(step_record)
            status["steps"] = step_results
            write_status(status)

            if step["route_id"] == "it-portal-login":
                login_ok = bool(parsed.get("login_success"))
                if not login_ok:
                    log("Login failed. Calling finalize-workflow for failure report and exiting.")
                    finalize = call_route(
                        "finalize-workflow",
                        "task-finalize-login-fail-014",
                        (
                            'Finalize the workflow for a login failure. '
                            "Output it_portal_summary.json with login_success false and a clear login_error_message. "
                            "Close the browser session cleanly."
                        ),
                        preferred_tier=2,
                    )
                    step_results.append(
                        {
                            "route_id": "finalize-workflow",
                            "todo_id": "task-finalize-login-fail-014",
                            "http_status": finalize["http_status"],
                            "response": finalize["response"],
                            "parsed_result": parse_result_payload(finalize),
                        }
                    )
                    status["steps"] = step_results
                    status["finished_at"] = time.time()
                    status["login_success"] = False
                    write_status(status)
                    return 0

        except Exception as exc:
            err = {
                "route_id": step["route_id"],
                "todo_id": step["todo_id"],
                "error": str(exc),
                "traceback": traceback.format_exc(),
            }
            log(f"ERROR route={step['route_id']} {exc}")
            step_results.append(err)
            status["steps"] = step_results
            write_status(status)
            continue

    if not login_ok:
        log("Login state was not confirmed as successful; skipping finalization.")
        status["finished_at"] = time.time()
        status["login_success"] = False
        write_status(status)
        return 1

    try:
        finalize = call_route(
            "finalize-workflow",
            "task-finalize-014",
            (
                'Finalize the workflow after all portal checks completed successfully. '
                "Create the comprehensive report in the correct knowledgebase directory, move all downloaded PDFs into that directory, "
                "delete old backup directories for the PAN prefix, and write it_portal_summary.json with files_moved and backups_deleted. "
                "Close the browser session cleanly at the end."
            ),
            preferred_tier=1,
        )
        step_results.append(
            {
                "route_id": "finalize-workflow",
                "todo_id": "task-finalize-014",
                "http_status": finalize["http_status"],
                "response": finalize["response"],
                "parsed_result": parse_result_payload(finalize),
            }
        )
        status["steps"] = step_results
        status["finished_at"] = time.time()
        status["login_success"] = True
        status["finalize_response"] = finalize["response"]
        write_status(status)
        return 0
    except Exception as exc:
        log(f"ERROR finalizing workflow: {exc}")
        status["finished_at"] = time.time()
        status["login_success"] = True
        status["finalize_error"] = str(exc)
        write_status(status)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
