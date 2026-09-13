# Browser performance workflow

## Define comparable measurements

Performance results are meaningful only with stable provenance. Give every target a page or journey ID, start/end condition, browser/device profile, viewport, network/CPU profile when supported, cache state, fixture state, warmup count, measured sample count, metrics, aggregation rule, budget/baseline revision, and tested build identity.

Prefer customer-supplied budgets. When none exist, record observations and trends as `unrated` until a policy is approved; do not turn a generic recommendation into a release threshold. Keep functional assertions active so a fast error page cannot pass.

## Adapt the plan

Use scripted steps for preflight, controlled sampling, deterministic metric parsing, aggregation, budget comparison, and persistence. Initialize the expected target/sample set before browsing. Mark invalid samples with reasons and do not replace them silently. Separate cold and warm cache, browser/device, or network profiles instead of averaging unlike conditions.

Use an investigation message sequence only after a comparable regression exists. It reads raw samples and redacted network/trace/console evidence, identifies likely contributors and uncertainty, and proposes targeted follow-up. It does not modify budgets. Apply the shared [performance measurement contract](../../references/performance-measurement.md) and Browser QA [evidence capture contract](../../../browser-qa/references/evidence-capture.md).

## Persist and report

Declare budget, target, run, sample, metric, comparison, finding, and artifact tables. Store raw samples as rows, not only aggregates. Preserve environment, build, runner/browser versions, selected profile, timestamps, cache state, validity, and evidence references.

The report shows current values against budgets/baselines, sample distribution and variance, invalid/missing samples, trends only across comparable profiles, network/resource diagnostics, and functional status. Label simulated profiles and uncontrolled environments accurately.

## Acceptance cases

Verify within-budget samples, a real budget breach, insufficient valid samples, a functional failure, unknown build identity, changed device/network profile, cold/warm cache separation, a large outlier, missing network/trace evidence, redaction failure, and two runs whose incompatible profiles never share one trend line or gate decision.
