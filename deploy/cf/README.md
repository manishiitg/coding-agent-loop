# Confida deployment checklist

Deploy from the application repository:

```sh
python3 -m unittest discover -s deploy/cf -p 'test_*.py'
bash -n deploy/cf/deploy-cf.sh deploy/cf/server-bootstrap-build.sh deploy/cf/server-build-and-activate.sh
DEPLOY_SOURCE_MODE=remote-main DEPLOY_BRANCH=main bash deploy/cf/deploy-cf.sh
```

The deploy scripts enforce the following checks. Any required check failure exits
nonzero; do not report success or bypass it. Confida alone is in scope: the
`confida` account, `/srv/confida`, and the three `confida-*` user services.

## Before build and activation

- [ ] The target is Linux and the executing account is `confida`.
- [ ] `/srv/confida/.env` contains exactly one
  `PUBLIC_URL=https://confida.agentworkshq.com`, and the agent unit loads that file.
  The check runs before dependency installation, again from the freshly cloned
  deployment code, and immediately before release activation. Missing, empty,
  duplicate, or incorrect values stop deployment; secrets are never printed.
- [ ] Confida's pinned Node archive passes its checksum and version checks;
  required provider and browser CLIs are available in persistent Confida paths.
- [ ] The Confida deployment lock is acquired; source comes from fresh remote
  `main` clones, with all three exact revisions saved in `SOURCE_REVISIONS`.
- [ ] Native Linux binaries build successfully, including the sandbox runner.
- [ ] Frontend TypeScript, Vite build, release-asset checks, and bundle-budget
  checks pass. Runtime configuration keeps CDP disabled.

## After activation, before success or release pruning

- [ ] Workspace, agent, and gateway services are active.
- [ ] The running agent's `/proc/<pid>/environ` contains the exact `PUBLIC_URL`.
  A correct file alone is insufficient: this catches missed restarts and runtime
  overrides. Callback URL: `https://confida.agentworkshq.com/api/oauth/callback`.
- [ ] Agent/workspace processes received the expected tools PATH and Confida
  browser namespaces.
- [ ] Local agent/workspace health and public `/api/health` and `/login` succeed.
- [ ] Only after verification is the release marked complete and old releases
  pruned. Record the release ID and source revisions when reporting deployment.

The checks can also run independently over SSH, without exposing the environment:

```sh
ssh -p 2299 -i ~/.ssh/confida_deploy confida@116.202.210.102 \
  'python3 - preflight' < deploy/cf/deployment_checks.py
ssh -p 2299 -i ~/.ssh/confida_deploy confida@116.202.210.102 \
  'python3 - running' < deploy/cf/deployment_checks.py
```

These are deterministic deployment gates, not a claim of byte-reproducible
builds: CLI installers still use `latest`, and each deployment resolves current
remote branch heads. The checks validate callback configuration, not completion
of a user's OAuth consent flow. No automatic rollback is currently implemented;
a post-activation failure must be investigated before calling the release healthy.
