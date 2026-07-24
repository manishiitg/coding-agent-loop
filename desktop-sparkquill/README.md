# SparkQuill desktop

The macOS app wrapper around SparkQuill. It starts the `family-server` Go binary,
waits for it to be healthy, and opens a window onto it — the UI is the same web
app the server already serves, so there's no second copy of the frontend here.

Sibling of `../desktop/` (AgentWorks), not a fork: that app has a tray, MCP
config rewriting, an auth secret and a docs-dir picker that SparkQuill doesn't
need. These two ship independently, on their own tags.

## Run it locally

```sh
./dev-setup.sh     # builds the frontend + family-server into resources/, installs deps
npm start
```

To work on the Electron chrome against a hot-reloading frontend, skip the
bundled server entirely and point at Vite:

```sh
cd ../frontend/learning-app && npm run dev     # in one terminal (:5174)
cd ../../agent_go && go run ./cmd/family-server # in another (:8010)
DEV_URL=http://127.0.0.1:5174 npm start         # here
```

## Build a .dmg

```sh
./dev-setup.sh
npm run build      # unpacked, into dist/ — fastest way to check it launches
npm run dist       # real .dmg + .zip (needs GH_TOKEN to publish)
```

CI does the same steps — see `.github/workflows/sparkquill-desktop.yml`.

## Release

Push a `sparkquill-v*` tag (namespaced so it can't collide with AgentWorks'
plain `v*` tags in this same repo):

```sh
git tag sparkquill-v0.1.0 && git push origin sparkquill-v0.1.0
```

The workflow builds the arm64 dmg, syncs `package.json`'s version to the tag,
and publishes it to that release. Users then install with:

```sh
curl -fsSL https://raw.githubusercontent.com/manishiitg/coding-agent-loop/main/install-sparkquill.sh | bash
```

## Things worth knowing

- **Not notarized.** `mac.identity` is `null` (ad-hoc signing), which is why
  `install-sparkquill.sh` has to strip the quarantine flag. Shipping outside a
  trusted circle means adding a Developer ID and a notarize step.
- **The dmg filename is load-bearing.** `productName` drives it
  (`SparkQuill-<version>-arm64.dmg`) and the installer hardcodes that shape —
  changing one means changing both.
- **Port.** Prefers 8010 and falls forward if it's taken. The frontend reads its
  API base from `window.sparkquill.apiBaseUrl()` (see `preload.js`), so a
  shifted port still works.
- **Data lives in `~/.sunlit-learning`**, not in the app bundle or `userData` —
  so it survives reinstalls, and the family can find and back it up. Logs go to
  `userData/logs/` (Help → Open Logs).
- **PATH.** A GUI-launched app gets a minimal PATH, but `family-server` shells
  out to the family's coding CLI (codex/claude/cursor/pi) and tools like `gws`.
  `main.js` imports the real login-shell environment at startup to fix that.
