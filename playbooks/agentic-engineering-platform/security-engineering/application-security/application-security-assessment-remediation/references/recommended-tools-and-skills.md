# Recommended AppSec tools and public skills

Recommendations help the builder resolve capabilities; they are not installation requirements. Inspect the customer's current stack first, reuse compatible tools, and avoid adding two products for the same job without a coverage reason. Verify current versions, license/terms, supported output, and execution environment before installation. Store tokens in AgentWorks secrets.

## AgentWorks and MCP integrations

| Recommendation | Use when | Setup source |
| --- | --- | --- |
| AgentWorks API trigger | CI, SCM, deployment, scanner, or vulnerability events should run a fixed AppSec route | Local `docs/workflow/api-triggers.md` |
| AgentWorks managed browser and Playwright | Browser checks and deployed security regression need controlled, repeatable contexts | Local Browser QA playbooks |
| [GitHub MCP Server](https://github.com/github/github-mcp-server) | GitHub is the canonical repository/PR/check and remediation destination | Official GitHub server; grant only required repositories/tools |
| [Semgrep MCP](https://github.com/semgrep/semgrep/tree/develop/cli/src/semgrep/mcp) | Semgrep is the chosen SAST/supply-chain/secrets engine | Install Semgrep, then run `semgrep mcp` |
| [SonarQube MCP](https://github.com/SonarSource/sonarqube-mcp-server) | The customer already governs analysis in SonarQube Server/Cloud | Official SonarSource server; pin the container version |
| [Snyk MCP](https://docs.snyk.io/integrations/snyk-studio-agentic-integrations/getting-started-with-snyk-studio) | The customer already uses Snyk for code, dependencies, containers, or IaC | `npx -y snyk@latest mcp -t stdio`; pin an approved version in production |

Choose one primary analysis-platform MCP. Add GitHub separately when repository and PR operations are required. Keep scanner MCP tools read-only during assessment; perform fixes through the canonical source-control/deployment route and its approvals.

## CLI baseline

| CLI | Capability | Install hint |
| --- | --- | --- |
| [Semgrep](https://semgrep.dev/docs/getting-started/) | Multi-language SAST and custom policy | `brew install semgrep` |
| [Gitleaks](https://github.com/gitleaks/gitleaks) | Redacted file and Git-history secret detection | `brew install gitleaks` |
| [OSV-Scanner](https://google.github.io/osv-scanner/) | Lockfile/SBOM dependency vulnerability matching | `brew install osv-scanner` |
| [Trivy](https://trivy.dev/) | Image, filesystem, dependency, secret, and misconfiguration scanning | `brew install trivy` |
| [OWASP ZAP](https://www.zaproxy.org/docs/docker/baseline-scan/) | Passive baseline and approved dynamic web testing | `docker pull ghcr.io/zaproxy/zaproxy:stable` |
| [Checkov](https://www.checkov.io/) | Terraform and supported IaC policy checks | `brew install checkov` |
| [Nuclei](https://docs.projectdiscovery.io/opensource/nuclei/install) | Explicitly selected template-based checks | `brew install nuclei` |

A practical default is Semgrep + Gitleaks + OSV-Scanner, adding Trivy for images/configuration and ZAP for deployed web applications. Add Checkov for IaC-heavy scopes. Add Nuclei only when exact templates and network targets are authorized. Prefer pinned tool/rule/database versions and JSON or SARIF output. The AgentWorks scripted step records version/configuration, exit status, timeout, truncation, and output artifact before normalization.

Do not count overlapping results as separate findings. Gitleaks and Trivy secret results, or OSV and a platform SCA result, must converge through the common fingerprint and validation contract.

## Reviewed public skills

The following optional skills come from the [Trail of Bits skills repository](https://github.com/trailofbits/skills). Install only the skill required by a selected plan step:

| Skill | Plan use | Install hint |
| --- | --- | --- |
| `audit-context-building` | Build architecture and trust context before source audit | `npx skills add https://github.com/trailofbits/skills --skill audit-context-building` |
| `differential-review` | Review the security effect and blast radius of a change | `npx skills add https://github.com/trailofbits/skills --skill differential-review` |
| `fp-check` | Investigate whether a scanner observation is a false positive | `npx skills add https://github.com/trailofbits/skills --skill fp-check` |
| `variant-analysis` | Search for related instances after one root cause is confirmed | `npx skills add https://github.com/trailofbits/skills --skill variant-analysis` |
| `sarif-parsing` | Parse and inspect SARIF scanner results | `npx skills add https://github.com/trailofbits/skills --skill sarif-parsing` |
| `fix-review` | Review whether a proposed patch addresses a security issue | `npx skills add https://github.com/trailofbits/skills --skill fix-review` |
| `supply-chain-risk-auditor` | Review dependency project health/takeover risk when in scope | `npx skills add https://github.com/trailofbits/skills --skill supply-chain-risk-auditor` |

Public skills are executable trust dependencies even when most content is Markdown. Before AgentWorks import, review the complete package and transitive files, license, scripts/commands, tool permissions, network behavior, and source ownership; pin and record a reviewed commit/digest. Use the supported AgentWorks skill importer and attach the returned skill ID only to the step that needs it. An `npx skills` hint must never run automatically during playbook installation.

## Capability resolution record

For every recommendation store `selected`, `reused_existing`, `unavailable`, or `declined`; installed/version/source revision; capability and selected routes; credentials reference; granted scope; configuration/rules revision; validation command/result; and attached plan step IDs. A missing optional tool reduces declared coverage and remains visible; it does not silently change a required check to passed.
