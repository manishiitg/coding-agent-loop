# FinOps provider-adapter contract

Provider adapters map live customer sources into the playbook's common evidence model. They contain source mappings and query guidance, while the playbook keeps decision rules provider-neutral.

## Required source mappings

Map these source classes for each installed environment:

- billing export/API: account hierarchy, resource, service/SKU, interval, quantity, unit, rate, currency, credits, commitments, refunds, and source revision;
- inventory/configuration: canonical resource ID, type, region, tags, lifecycle, dependencies, and current configuration;
- monitoring: utilization, demand, capacity, health/SLO, aggregation, sampling, and retention;
- pricing: current provider price source, retrieval time, region, unit, tier, and effective conditions;
- ownership/IaC: owner, environment, repository, module/resource address, state/workspace, and deployed revision.

AWS-style accounts, Azure-style subscriptions/resource groups, GCP-style organizations/folders/projects, private clouds, and SaaS tenants map to the same fields. Keep provider-native identifiers alongside normalized values.

## Adapter behavior

Use customer-approved AgentWorks MCPs, CLIs, APIs, warehouses, or exports. Resolve current command and schema details at installation time; recommendations in the manifest are optional. Use scoped read-only credentials for discovery and analysis, paginate completely, record query/window/freshness, and never place secrets in plans, reports, artifacts, or the knowledge base.

Validate billing totals against the customer's source for a sampled period. Handle late corrections, time zones, duplicate rows, shared costs, tax, refunds, credits, commitments, currency conversion, missing tags, and resources without direct cost IDs explicitly. Version the adapter mapping. A mapping or reconciliation failure marks dependent conclusions `insufficient_data`.
