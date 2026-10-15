- read README.md and style_guide.md at the beginning of every session.
- on first init, run `just setup` — this installs dependencies, the pre-commit hooks, and the playwright chromium browser.
- the app is Go, with no cgo. python (via uv) is only for the container tests in `tests/`: `uv run pytest`.
- this is an OpenHost app. `openhost.toml` is the app manifest. it consumes the messaging service
  (github.com/cloud-in-a-bottle/messaging-service-spec), whose Go types and client this app uses.
- the UI builds on the messaging apps' shared UI (`ui.js`, `app.css` in bottled-bridge-core's `web/static` package,
  served here under /static/core/). change shared pieces there, then `just update-core <ref>` here.
- tests use the OpenHost test harness: each run builds the Dockerfile and runs it under podman beside a real messaging
  app (bottled-instagram, from GitHub), fronted by the real OpenHost router. run them with the workbench's OPENHOST_*
  variables unset.
- if you run into any cases where the app test harness doesn't match the expected/real behavior of openhost, stop and
  mention this so that we can fix the test harness - don't just make some workaround to the issue.
