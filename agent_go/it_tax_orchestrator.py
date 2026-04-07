import json
import os
from pathlib import Path

import requests


BASE = "http://host.docker.internal:8000/s/session-group-group-3-1775456138222730000"
HEADERS = {
    "Authorization": f"Bearer {os.environ['MCP_API_TOKEN']}",
    "Content-Type": "application/json",
}
ROOT = Path("/app/workspace-docs/Workflow/check-form-26as-xspaces/runs/iteration-0/saurabh/execution")
LOGIN_STATUS = ROOT / "it-portal-login" / "login_status.json"
SUMMARY_PATH = Path("/tmp/it_tax_orchestrator.summary.json")
LOG_PATH = Path("/tmp/it_tax_orchestrator.steps.log")


def log(message: str) -> None:
    line = message.rstrip()
    print(line, flush=True)
    with LOG_PATH.open("a", encoding="utf-8") as fh:
        fh.write(line + "\n")


def read_json(path: Path):
    with path.open("r", encoding="utf-8") as fh:
        return json.load(fh)


def call_route(route_id: str, todo_id: str, instructions: str, preferred_tier: int = 2):
    payload = {
        "route_id": route_id,
        "todo_id": todo_id,
        "instructions": instructions,
        "preferred_tier": preferred_tier,
        "share_browser": True,
    }
    log(f"[CALL] {route_id} ({todo_id})")
    resp = requests.post(
        f"{BASE}/tools/custom/call_sub_agent",
        headers=HEADERS,
        json=payload,
        timeout=3600,
    )
    resp.raise_for_status()
    data = resp.json()
    log(f"[RESP] {route_id}: {json.dumps(data, ensure_ascii=True)}")
    return data


def main() -> int:
    if LOG_PATH.exists():
        LOG_PATH.unlink()

    if not LOGIN_STATUS.exists():
        log("[ABORT] login_status.json is missing")
        SUMMARY_PATH.write_text(json.dumps({"login_success": False, "error": "missing login_status.json"}, indent=2), encoding="utf-8")
        return 1

    login = read_json(LOGIN_STATUS)
    if not login.get("login_success"):
        log("[ABORT] login_status.json reports login_success=false; calling finalize-workflow only")
        finalize = call_route(
            "finalize-workflow",
            "finalize-workflow-20260406",
            "Login failed. Produce the failure summary only, using the same shared browser session if still available, and write it_portal_summary.json.",
        )
        SUMMARY_PATH.write_text(json.dumps({"login": login, "finalize": finalize}, indent=2, ensure_ascii=True), encoding="utf-8")
        return 0

    workflow = [
        (
            "extract-profile-details",
            "extract-profile-details-20260406",
            "Using the shared browser session for this workflow (it_portal), open the logged-in Income Tax portal dashboard and extract the registered phone number, email address, and date of birth/in corporation details from My Profile or the account/profile area. If the portal does not expose date of birth/in corporation, fall back to the raw value AEXFS2030K (23/12/2022) only for the date component if needed. Write profile_details.json with the captured fields and any source URL/page notes. Handle any session-expiry state by returning to the dashboard once if necessary.",
        ),
        (
            "download-form-26as",
            "download-form-26as-20260406",
            "Using the shared browser session for this workflow (it_portal), navigate from the logged-in Income Tax dashboard to e-File -> Income Tax Returns -> View Form 26AS, then accept the TRACES disclaimer. Download the HTML view exported as PDF for the 3 most recent Assessment Years, one at a time, verifying each download before moving to the next year. Write download_status.json and ensure the PDFs are saved in the step output directory with clear names.",
        ),
        (
            "download-ais-pdf",
            "download-ais-pdf-20260406",
            "Using the shared browser session for this workflow (it_portal), navigate from the dashboard to Services -> Annual Information Statement (AIS). If the portal session is unstable or times out, refresh or navigate back to the dashboard once and continue. Read the available year options dynamically, select the latest available financial/assessment year, download the AIS PDF as ais.pdf in the step output directory, and write ais_download_status.json with download_success, financial_year, pan, and pdf_file.",
        ),
        (
            "check-e-proceedings",
            "check-e-proceedings-20260406",
            "Using the shared browser session for this workflow (it_portal), navigate to Pending Actions -> e-Proceedings and inspect all active notices or intimations. If any notice has a downloadable PDF/document link, download it and copy the file into the step output directory with a clear name. Write eproceedings_report.md and eproceedings_status.json, including any downloaded PDF paths.",
        ),
        (
            "check-worklist",
            "check-worklist-20260406",
            "Using the shared browser session for this workflow (it_portal), inspect the Worklist tab, including both For Your Action and For Your Information. Capture all available item details such as type, dates, status, and assessment years. Write worklist_report.md and worklist_status.json.",
        ),
        (
            "check-outstanding-demand",
            "check-outstanding-demand-20260406",
            "Using the shared browser session for this workflow (it_portal), inspect Pending Actions -> Response to Outstanding Demand and capture any tax demands or confirm absence of demands. Write demand_report.md and demand_status.json.",
        ),
        (
            "check-compliance",
            "check-compliance-20260406",
            "Using the shared browser session for this workflow (it_portal), navigate to Compliance -> View and Submit Compliance and inspect all notices shown. Capture any defect notices, notices u/s 148, or similar compliance items. Write compliance_report.md and compliance_status.json.",
        ),
        (
            "check-e-campaigns",
            "check-e-campaigns-20260406",
            "Using the shared browser session for this workflow (it_portal), inspect Pending Actions -> Compliance Portal -> e-Campaigns (or the equivalent e-Campaigns entry in the current UI). If a new tab opens, switch to it, read the data, then close it and return to the dashboard. Write ecampaigns_report.md and ecampaigns_status.json with issues_found and report_file.",
        ),
        (
            "check-grievances",
            "check-grievances-20260406",
            "Using the shared browser session for this workflow (it_portal), navigate to Grievances -> View Grievance Status and capture any active or historical grievances with reference number, description, and status. Write grievances_report.md and grievances_status.json with issues_found and report_file.",
        ),
        (
            "check-filed-returns",
            "check-filed-returns-20260406",
            "Using the shared browser session for this workflow (it_portal), navigate to e-File -> Income Tax Returns -> View Filed Returns and inspect the latest available returns. Capture filing date, return status, refund details, and reported income where available, and download any Intimation u/s 143(1) PDFs one at a time. Write filed_returns_report.md and returns_status.json with references and any downloaded Intimation PDF paths.",
        ),
        (
            "summarize-pdfs",
            "summarize-pdfs-20260406",
            "Using the shared browser session for this workflow (it_portal), summarize the downloaded Form 26AS PDF(s), AIS PDF, any filed-return Intimation PDFs, and any e-proceedings PDFs listed by the status files. Use the learned PDF extraction script from learnings/_global/scripts/extract_pdfs.py to handle password derivation and extraction quirks. Write clean plain text summaries to pdf_summaries.txt and write pdf_summaries_status.json.",
        ),
    ]

    results = []
    for route_id, todo_id, instructions in workflow:
        try:
            result = call_route(route_id, todo_id, instructions)
            results.append({"route_id": route_id, "todo_id": todo_id, "ok": True, "result": result})
        except Exception as exc:
            log(f"[ERROR] {route_id}: {exc}")
            results.append({"route_id": route_id, "todo_id": todo_id, "ok": False, "error": str(exc)})

    try:
        finalize = call_route(
            "finalize-workflow",
            "finalize-workflow-20260406",
            "Using the shared browser session for this workflow (it_portal), compile the full report, move all downloaded PDFs into the appropriate knowledgebase folder, and close the browser cleanly. Login succeeded for this run. Use the generated markdown reports and status JSON files plus the template in knowledgebase/report_template.md to create Comprehensive_Income_Tax_Report.md and it_portal_summary.json.",
        )
        results.append({"route_id": "finalize-workflow", "todo_id": "finalize-workflow-20260406", "ok": True, "result": finalize})
    except Exception as exc:
        log(f"[ERROR] finalize-workflow: {exc}")
        results.append({"route_id": "finalize-workflow", "todo_id": "finalize-workflow-20260406", "ok": False, "error": str(exc)})

    SUMMARY_PATH.write_text(json.dumps(results, indent=2, ensure_ascii=True), encoding="utf-8")
    log(f"[DONE] summary written to {SUMMARY_PATH}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
