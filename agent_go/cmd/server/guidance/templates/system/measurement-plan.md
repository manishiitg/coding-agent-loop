## MEASUREMENT — run-scoped, evidence-backed outcomes

Outcome measurement is produced by ordinary workflow steps, not a mandated
route or table. Any normal step or scheduled collector may produce a
run-scoped, evidence-backed measurement. Pulse does not depend on which step
or route produced it.

### Placing measurement

- Reuse an existing step's DB output when it already measures the outcome.
  Existing workflow tables stay the source of truth; do not copy their values
  into a second metrics store.
- Add `record_goal_observations` to that producer only when the value should
  enter Pulse's generic goal-progress history (definitions via
  `configure_goal_metrics`, reads via `get_goal_metrics`).
- Put route-specific measurements in existing route-local steps.
- Use a shared convergence step only when multiple routes genuinely require
  the same calculation.
- Add a dedicated measurement step only when no existing step can safely own
  it. It is an ordinary step with no special topology.
- Make no topology change when existing measurement is already sufficient.
- Flag ambiguous cases for manual migration rather than inventing steps or
  routes.

### Run scope and evidence

- Scope every measurement to the run that produced it; never report "latest"
  as the current run's outcome. Interactive runs reuse `iteration-0`, so a
  run folder alone is not a stable measurement identity across runs.
- Steps receive database-wide read-write access: there is no table-scoped
  authorization. A measurement reader must therefore re-verify the producer's
  rows (run scope, filters, freshness) instead of assuming a privileged write
  path kept them clean.
- A measurement failure is recorded as a measurement failure. It must never
  erase or invalidate the successful business run it measured.

### Legacy evaluation artifacts

Old `evaluation/` plans and `evaluation_report.json` files are read-only history,
as are retired `costs/evaluation/` ledgers and retired `eval_results` rows,
unless a separate cleanup migration is approved. Never write to them; never let
a new measurement depend on them.
