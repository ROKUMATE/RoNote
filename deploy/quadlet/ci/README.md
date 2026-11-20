# CI and prod-local drop-ins

The production units with the fakes swapped in (docs/adr/0014-production-topology.md):
`deploy/install.sh --drop-ins deploy/quadlet/ci` installs these beside the
units, and `config/` as the deployment's configuration.

- `ronote-ollama`, `ronote-docling`, `ronote-pdftext` run `cmd/fakeembed` and
  `cmd/fakedocling` from the `ronote-fakes` image instead of their real images.
- `ronote-fakellm` and `ronote-fakeidp` are the model and GitHub; the fake
  sign-in is behind Caddy as `idp.ronote.test`, since the browser goes there.
- `RONOTE_ENV=test`: staging and prod refuse a fake sign-in.
- Caddy issues its own certificates (`local_certs`) for `ronote.test`,
  `ronote-run.test` and `idp.ronote.test`, and serves the pages the smoke
  suite imports to the worker at `http://ronote-caddy:8080`.
