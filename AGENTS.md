# AGENTS.md

This file provides guidance to coding agents (e.g. Claude Code, claude.ai/code) when working with code in this repository.

## Repository purpose

Go module `go.bytebuilders.dev/license-proxyserver` — an aggregated Kubernetes API server that **automates AppsCode product license issuance** in customer clusters. Downstream AppsCode operators (KubeDB, KubeVault, Stash, etc.) ask `license-proxyserver` for their license via the aggregated API; the proxyserver pulls/refreshes from the upstream b3 backend and serves it back.

The produced binary is `license-proxyserver`.

## Architecture

- `cmd/license-proxyserver/` — entry point.
- `pkg/cmds/` — Cobra root + run.
- `pkg/apiserver/` — aggregated apiserver lifecycle.
- `pkg/registry/` — `rest.Storage` glue for license-related resources.
- `pkg/controllers/` — controllers that drive license refresh and per-product configuration.
- `pkg/manager/` — manager bootstrap.
- `pkg/storage/` — pluggable license storage backends.
- `pkg/secretfs/` — secret-backed filesystem helpers.
- `pkg/common/` — shared types.
- `apis/` — Kubebuilder API types.
- `client/` — generated typed clientset.
- `crds/` — generated CRD YAMLs.
- `Dockerfile.in` (PROD, distroless), `Dockerfile.dbg` (debian), `Dockerfile.ubi` (Red Hat certified).
- `PROJECT` — Kubebuilder metadata.
- `Development.md` — developer guide.
- `hack/`, `Makefile` — AppsCode build harness.
- `vendor/` — checked-in deps.

## Common commands

- `make ci` — full CI pipeline.
- `make build` / `make all-build` — host or all-platform build.
- `make gen` — regenerate clientset + manifests after API type changes.
- `make fmt`, `make lint`, `make unit-tests` / `make test` — standard.
- `make verify` — codegen + module-tidy verification.
- `make container` / `make push` / `make release` — image build/publish flow.
- `make push-to-kind` / `make deploy-to-kind` — Kind dev loop.

## Conventions

- Module path is `go.bytebuilders.dev/license-proxyserver` (vanity URL); imports must use that.
- License: `LICENSE`. Sign off commits (`git commit -s`).
- Vendor directory is checked in; keep `go mod tidy && go mod vendor` clean.
- This is an **aggregated apiserver**, not a controller-runtime operator. Persistence goes through `pkg/registry/` + `pkg/storage/`; don't add parallel storage paths.
- License refresh logic lives in `pkg/controllers/` — keep token-handling and HTTP-to-b3 interaction there.
- Three Dockerfiles, one binary — keep `Dockerfile.in`, `Dockerfile.dbg`, and `Dockerfile.ubi` in sync.
- Do not hand-edit `zz_generated.*.go`, anything under `client/`, or `crds/` — change `apis/**/*_types.go` and re-run `make gen`.
