# Clustarr source subset

Source: https://github.com/mediactl/clustarr

Pinned commit: b252cc57b9e5b86bcc057e4965742b53126f406a

GPL-3.0-or-later. Copied package closure of pkg/cardigann, including upstream tests.
No daemon or Kubernetes controller is used. See patches/ for local behavior changes.

Local changes:

- `patches/0001-render-method-and-full-month.patch`: render HTTP method templates before
  uppercasing; distinguish .NET MMMM (January) from MMM (Jan).
- `patches/0002-cap-template-output-and-scope-headers-to-site.patch`: cap one template's output at 1 MiB;
  send a definition's headers only to the site, not to an off-site download selector target.
- `patches/0003-json-root-array-index.patch`: strip the leading dot the `[n]` rewrite left, so a
  root-array selector such as `$[0].id` (The Pirate Bay's rows.count) finds its node.
- `pkg/cardigann/downloadhub_regression_test.go`, `downloadhub_headers_test.go`: offline regressions.
- `pkg/cardigann/runtime_templates.go`: reuse the exact template parser to reject unsupported syntax.
- `test/data/{cardigann,torznab,newznab}`: unchanged upstream fixtures retained for offline tests.

The engine fixes are self-contained; template validation is host integration glue.

Apply the engine-only patch upstream with `git apply --unidiff-zero -p1`.
