---
name: QA sign-off
about: Test checklist for a shipped, user-visible feature
title: "QA: <area> — <what changed>"
labels: qa
---

QA sign-off for <one line: what shipped and who it affects>.
Commits: <hash> (<short what>), <hash> (<short what>)
Needs: <server restart / browser refresh / a workflow with X>, or "nothing".
Check boxes as you verify; post failures as comments with repro steps.

## A. <First area>

- [ ] A1. <Action the tester takes>. Expected: <what they should see>.
- [ ] A2. <Action>. Expected: <result>.

## B. <Second area>

- [ ] B1. <Action>. Expected: <result>.

## Regression

- [ ] R1. <Existing behaviour that must still work>. Expected: unchanged.

## Sign-off

- [ ] All boxes checked, or failures filed as issues with repro steps.
- [ ] Tester name + date: _______________
