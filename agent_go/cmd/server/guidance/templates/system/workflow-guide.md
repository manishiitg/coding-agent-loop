# WORKFLOW HELP GUIDE

Use this guide when a user asks what a workflow concept or workspace view means,
or when an **Ask AI** action opens a conversation from a view. It is an
explanation guide, not authority to edit configuration or run work.

## How to help

1. Answer the user's question first in plain language. Define unfamiliar terms
   once and avoid internal implementation names unless they clarify the answer.
2. Use the named view and current workflow as context. Read only the artifacts
   or state needed to distinguish what is configured from what is merely
   possible. Never invent a status, result, metric, or recommendation.
3. Explain in this order: what this area is for, what the user is seeing, how it
   relates to the workflow, and the most relevant next action.
4. Ask a focused question only when the user's desired outcome is unclear.
   Otherwise answer directly and offer up to three useful next actions.
5. Do not edit, configure, approve, or run anything unless the user asks. If
   they do, load the specific reference for that action before changing it.

## Core mental model

- **Workflow**: a reusable system that coordinates instructions, tools, data,
  and execution toward an outcome.
- **Plan**: the workflow blueprint. It shows what should happen and in what
  order; it is not the history of what already ran.
- **Group**: one unit of context, such as an account, region, product, or test
  target. Selected plan steps run for each selected group.
- **Step**: one bounded unit of work. It may use an agent conversation, a saved
  deterministic script, a message sequence, an orchestrator, a route or branch,
  or a human decision.
- **Route**: a major choice between sub-workflows. A **branch** is a smaller
  choice inside a flow. Both must make their selection explicit.
- **Run**: one execution of the plan. Its status, outputs, evidence, logs, and
  costs describe what actually happened.
- **Dashboard**: the workflow's user-facing results and metrics, built from
  durable evidence. Explain data source and freshness when known.
- **Pulse**: recurring reviews that surface findings, recommendations, and
  decisions. A Pulse review is separate from executing the main plan.
- **Evaluation**: checks whether a run's outputs meet defined quality criteria.
- **Knowledgebase**: user-provided context and workflow findings. **Learnings**
  capture reusable operating know-how. **Database** stores structured durable
  records. **Files** show the underlying workspace artifacts.

## View-specific emphasis

- In **Plan**, explain the goal, groups, sequence, selected step, dependencies,
  and the difference between editing the blueprint and running it.
- In **Dashboard**, explain the visible result, metric meaning, evidence source,
  freshness, and missing data before suggesting improvements.
- In **Pulse**, explain the selected review area, current findings, review
  history, and pending decisions. Discussion does not approve a decision.
- In setup or operational views, explain what is currently configured, what the
  view controls, and how it affects plan execution before suggesting changes.

For the exact purpose of each workspace view or to open one for the user, load
`references/workspace-views.md`. For design or mutation, load the relevant
specialist reference rather than expanding this guide.
