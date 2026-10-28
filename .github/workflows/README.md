# GitHub Actions Workflows

## CI (`ci.yml`)

Runs on push/PR to `main` and `develop`, plus manual `workflow_dispatch`.

### Concurrency

Superseded runs on the same PR/branch are **cancelled** so the queue stays lean.

### Required jobs (merge gate)

| Job | Purpose |
|-----|---------|
| **Lint & Format** | `gofmt`, `go mod tidy` drift, golangci-lint, trailing whitespace |
| **Unit Tests** | `tests/unit` + `internal/...` with `-race` (websocket soft-excluded) |
| **P2P Replication E2E** | `TestReplicationStoreAGetB` — store on A, get on B |
| **API Smoke (REST)** | Fast httptest REST integration |
| **govulncheck** | Known dependency vulnerabilities fail the build |
| **Build** | Linux / Windows / macOS core binaries |
| **Docker** | Compose validate + node/demo images + smoke |

`CI Success` fails unless every required job is `success`.

### Soft jobs (`continue-on-error`)

| Job | Purpose |
|-----|---------|
| Integration (soft) | gRPC/GraphQL/multi-node/full e2e/websocket — warn only |
| Fuzz | Short fuzz budgets on crypto/transport/storage |
| Security static | staticcheck + Semgrep (full scan in `security.yml`) |
| Benchmarks | Artifact upload only |
| Docs sanity | Required files present |

Promote soft suites to required once they are stable. Soft jobs are omitted from the **CI Success** `needs` list so warn-only suites do not delay the merge gate.

### Shared setup

[`.github/actions/setup-go`](../actions/setup-go/action.yml) installs Go, enables module cache, runs `go mod download` + `go mod verify`.

### Local equivalents

```bash
gofmt -s -l .
go mod tidy
golangci-lint run --config=config/.golangci.yml
go test -race ./tests/unit/... ./internal/...
go test -count=1 -timeout=3m ./tests/integration/end-to-end/ -run TestReplicationStoreAGetB
go test ./tests/integration/rest/...
govulncheck ./...
```

## Security (`security.yml`)

Deeper vuln/compliance scans (govulncheck, semgrep, secrets, Trivy, schedule). See [SECURITY.md](SECURITY.md).

## CodeQL (`codeql.yml`)

Advanced CodeQL for Go, JavaScript/TypeScript, and Actions. Go uses `build-mode: manual` and `go build ./...` so analysis is not skipped the way default autobuild was on this multi-`cmd` module.

After the first successful run, switch the repo from **default** to **advanced** CodeQL under Settings → Advanced Security (disable default setup if it is still on). Otherwise both setups can fight each other.

## Development security (`security-dev.yml`)

Lighter checks when security-related paths change.

## Branch protection (recommended)

Require status check: **CI Success** (or list each required job). Do not require soft jobs.
