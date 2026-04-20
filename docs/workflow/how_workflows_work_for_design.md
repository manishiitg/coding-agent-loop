# How Workflows Work For Design

This page is for product design, UX, and UI work.

It explains workflows as a user experience, not as an implementation detail.

## What A Workflow Is

A workflow is a reusable sequence of work that an AI agent can run from start to finish.

From a user’s point of view, a workflow is the combination of:

- a goal
- a set of steps
- optional decision points
- optional human approvals or clarifications
- outputs that can be reviewed later

The simplest way to think about it is:

**A workflow is a repeatable job the user can design once and run many times.**

Examples:

- review a PR and create a summary
- collect information from multiple tools and produce a report
- run a task for multiple clients or environments
- perform a browser-based operational task that sometimes needs human approval

## The User Journey

Most workflow experiences fall into four moments:

1. **Design**
   The user creates or edits the workflow in the builder.

2. **Configure**
   The user chooses tools, models, variables, groups, and run behavior.

3. **Run**
   The workflow executes step by step, sometimes automatically and sometimes with pauses.

4. **Review**
   The user inspects outputs, logs, learnings, costs, reports, and previous runs.

For design, this means the product is not just a canvas. It is a full lifecycle experience.

## The Main UX Model

A workflow has two sides:

- **Builder side**: where users define what should happen
- **Run side**: where users see what is happening and what happened

These should feel connected but clearly different.

The builder is for:

- shaping the logic
- editing steps
- setting up conditions and routing
- choosing what the workflow needs

The run experience is for:

- watching progress
- responding to blockers
- understanding success or failure
- comparing current and past runs

## The Core Mental Model For Users

Users should not need to understand files, manifests, or orchestration internals.

What they need to understand is:

- a workflow has a clear goal
- it is made of steps
- some steps can branch or ask questions
- one workflow can run in different contexts, such as different groups or environments
- every run creates a visible history
- the latest run is the main working view

That last point is important for UX:

**Users should feel like there is always one “current run” and several “past runs.”**

## The Four Core Screens Or Modes

Design-wise, workflows naturally map to four core surfaces.

### 1. Workflow Builder

This is where users define the flow.

The builder should help users answer:

- What is this workflow trying to achieve?
- What are the steps?
- Where does the workflow branch?
- Where might it need approval or input?
- What should count as success?

Good builder UX should make the structure obvious at a glance.

The user should be able to distinguish:

- normal steps
- decision steps
- human input steps
- orchestration or delegation steps
- output-producing steps

The builder should feel like planning a process, not writing raw instructions.

## Supporting Layers Designers Should Know About

A workflow is more than boxes and arrows. Several supporting layers shape how the user experiences the product.

### Skills

Skills are reusable instructions or patterns the workflow can rely on.

From a design point of view, skills matter because they help users avoid rebuilding the same guidance repeatedly. They act like reusable expertise that can be attached to a workflow or step.

UX implication:

- users should understand when a workflow is using shared expertise versus custom step instructions
- the UI should make attached skills visible without forcing users to read raw technical content

### Knowledgebase (KB)

Some workflows build on persistent knowledge over time.

This is best understood as workflow memory that can influence future runs. A workflow may read from existing knowledge, contribute new knowledge, or both.

UX implication:

- users need to understand whether a workflow is using prior knowledge
- users should be able to inspect or trust that knowledge without feeling like it is hidden magic
- KB-enabled workflows may feel more adaptive over time, so the product should surface that clearly

### Learn Code Mode

Some workflow steps do not just generate outputs once. They gradually become more repeatable by turning working behavior into reusable code.

For design, this means some steps evolve from exploratory AI work into a more stable scripted behavior.

UX implication:

- users may need to understand whether a step is still exploratory or has become more repeatable
- the product should make “getting more reliable over time” visible as a benefit, not a hidden implementation detail

### Browser-Driven Work

Some workflows interact with websites and external tools directly in the browser.

This is a different UX category from simple text generation because it involves navigation, session state, login behavior, possible delays, and sometimes human intervention.

UX implication:

- browser steps should feel like real-world operations, not generic AI output
- users need clearer feedback when the workflow is navigating, waiting, blocked by login, or asking for approval

### Reporting

Many workflows exist to produce a final deliverable.

That deliverable may be:

- a summary
- a document
- a report
- a structured output for a team or downstream tool

UX implication:

- final outputs should feel like a destination, not an afterthought
- the interface should make it obvious what the final artifact is and where to find it

### Evaluation

Evaluation is the part of the product that helps teams test workflow quality over time.

This is not the same as a normal run. It is a way to assess whether the workflow is performing well, consistently, and at an acceptable cost.

UX implication:

- evaluation should feel like measuring workflow quality, not just reviewing a single execution
- design should clearly distinguish “run the workflow” from “evaluate the workflow”
- comparison and trend visibility matter more here than step-by-step execution detail

### 2. Run Setup

Before running, users often need to choose the context.

This can include:

- which group or environment to run
- whether to use the current run or start fresh
- whether the run should happen now or on a schedule

This is the bridge between design and execution.

For UI/UX, the main job here is reducing ambiguity. Users should feel confident about:

- what will run
- where it will run
- whether this affects the latest run
- whether one group or many groups are being run

### 3. Live Execution

This is the “workflow in motion” experience.

The user needs to understand:

- which step is running now
- which steps already completed
- what is waiting
- whether the workflow is blocked
- whether the system needs input from them

The most important design requirement here is **state clarity**.

A live run should make it easy to spot the difference between:

- running
- waiting
- needs input
- succeeded
- failed
- skipped

If a workflow pauses for human input, that moment must feel intentional, not like a crash.

The user should understand:

- why it paused
- what information is needed
- how to respond
- what happens after they respond

### 4. Review And History

After a run, users need a reliable place to inspect what happened.

That includes:

- final outputs
- past runs
- run-by-run differences
- costs and usage
- evaluation results
- learnings or improvements over time

This is where workflows become trustworthy. If users cannot inspect the history, they will not trust automation.

There are really two kinds of review:

- **Run review**: What happened in this specific execution?
- **Workflow review**: Is this workflow getting better, more reliable, and more useful over time?

That distinction matters because reporting, evaluation, learnings, and knowledgebase behavior all live closer to workflow review than simple run review.

## Variable Groups: Why They Matter To Design

A single workflow can often run for different groups, environments, or accounts.

For design, this means the workflow is not just “run” or “not run.” It may be run:

- for one selected group
- for multiple groups
- repeatedly on a schedule

This has several UX implications:

- users need strong group selection patterns
- users need clear labeling of which group a run belongs to
- history views need to separate “workflow” from “workflow run for group X”
- batch execution should not feel like one opaque blob

If group context is not visible enough, users will misread the results.

## Human-In-The-Loop Moments

Some workflows need a person to step in briefly.

Examples:

- approve a decision
- answer a clarifying question
- provide a 2FA or OTP code
- choose between routes

These moments are central to the UX because they turn the workflow from “fully automatic” into “collaborative automation.”

The key design principle:

**A pause should feel like a handoff, not a failure.**

The UI should make these moments feel:

- timely
- easy to act on
- contextual
- recoverable

Notifications matter here. If the workflow needs a human, the user should be able to notice and respond without having to hunt for the blocked run.

This becomes even more important for browser-based workflows, where the user may need to intervene because of login, verification, or a decision point in an external system.

## Scheduling: Another Entry Point, Not A Different Product

Scheduling should be treated as another way to launch the same workflow, not as a separate concept.

From the user’s point of view:

- the workflow is the thing they designed
- scheduling is just how often it should run

That means scheduled-run UX should stay closely tied to the workflow itself:

- users should see schedule status in workflow context
- users should be able to review scheduled runs in the same mental model as manual runs
- scheduled history should still feel like workflow history

## What Matters Most For UI/UX

If the design team remembers only a few things, they should be these:

### 1. Workflows are long-lived objects

They are designed once, run many times, reviewed over time, and often improved after each run.

### 2. Users need orientation at every stage

At all times, the UI should answer:

- What workflow am I looking at?
- What run am I looking at?
- What group or context is this for?
- What is happening right now?
- What should I do next?

### 3. The current run and run history are both first-class

Users need a strong “current/latest run” view and an equally strong way to inspect past runs.

### 4. Blocked states are part of the product

Waiting for approval or missing input is normal behavior, not an edge case.

### 5. Trust comes from visibility

Users trust workflows when they can see:

- what the workflow is supposed to do
- what happened during execution
- what output it produced
- what changed between runs
- whether it is using shared skills or prior knowledge
- whether it has become more reliable through learn-code behavior
- how evaluation is trending over time

## A Good Design Summary

A good workflow UX should make the system feel like this:

- easy to design
- clear to run
- easy to monitor
- safe to interrupt
- easy to review
- trustworthy over repeated use

In one sentence:

**The workflow product should feel like designing and operating a reliable teammate, not launching a one-off AI prompt.**

## Suggested Design Checklist

- Can a new user tell what the workflow does without opening every step?
- Can they tell the difference between editing the workflow and reviewing a run?
- Can they tell which run is current and which are historical?
- Can they tell which group or environment a run belongs to?
- Can they see where the workflow is blocked and why?
- Can they respond quickly when human input is required?
- Can they review outputs and past runs without confusion?
- Can they understand scheduling as part of workflow behavior, not a separate subsystem?
- Can they tell when a workflow depends on skills, prior knowledge, or browser interaction?
- Can they tell the difference between a one-off run result and broader evaluation/reporting quality?

## Related Docs

- [how_workflows_work.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/how_workflows_work.md)
- [workflow_builder_interactive.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/workflow_builder_interactive.md)
- [workflow_monitoring.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/workflow_monitoring.md)
- [human_feedback_system.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/human_feedback_system.md)
- [workflow_scheduling.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/workflow_scheduling.md)
