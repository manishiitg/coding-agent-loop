# Confida deployment checklist

Confida uses the reusable rootless Linux deployment pipeline. Its product
settings and assets live in `deploy/rootless-linux/products/confida`; build,
activation, verification, and retention logic remain shared with SparkQuill.

Run the checks and deploy from the application repository:

```sh
python3 -m unittest discover -s deploy/rootless-linux -p 'test_*.py'
python3 -m unittest discover -s deploy/common -p 'test_*.py'
bash -n deploy/rootless-linux/deploy.sh deploy/rootless-linux/bootstrap-build.sh deploy/rootless-linux/build-and-activate.sh deploy/cf/deploy-cf.sh
DEPLOY_BRANCH=main bash deploy/rootless-linux/deploy.sh confida
```

`DEPLOY_BRANCH=main bash deploy/cf/deploy-cf.sh` remains as a compatibility
wrapper and executes that same shared command.

The deployment fails unless all of these gates pass:

- the target is Linux, the service account is `confida`, and
  `/srv/confida/.env` has exactly one expected public URL;
- the source comes from fresh remote branch clones, and exact revisions for
  all three repositories are recorded in the immutable release;
- pinned Node, required provider CLIs, Slack CLI, browser automation, native
  binaries, frontend assets, runtime config, and playbooks validate;
- the Workflow Builder chat migration runs once with a durable marker;
- MCP overlay state lives outside releases at `/srv/confida/state/mcp`;
- workspace, agent, and gateway services restart successfully and their live
  process environments contain the required product values;
- local health, public `/api/health`, and public `/login` all succeed;
- old releases are pruned only after the new release passes verification.

The shared gates can also run independently over SSH without printing secrets:

```sh
ssh -p 2299 -i ~/.ssh/confida_deploy confida@116.202.210.102 \
  'PRODUCT=confida EXPECTED_PUBLIC_URL=https://confida.agentworkshq.com python3 - preflight' \
  < deploy/rootless-linux/deployment_checks.py
ssh -p 2299 -i ~/.ssh/confida_deploy confida@116.202.210.102 \
  'PRODUCT=confida EXPECTED_PUBLIC_URL=https://confida.agentworkshq.com python3 - running' \
  < deploy/rootless-linux/deployment_checks.py
```

These checks are deterministic deployment gates. Provider CLI installers still
resolve their current releases, so this is not a byte-reproducible build.
