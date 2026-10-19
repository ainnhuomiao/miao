# Repository Guidelines

## Project Structure & Module Organization

`miao` is a Go terminal AI coding assistant. `cmd/miao/` contains the executable entry point and setup/diagnostic commands. Implementation lives in `internal/`: `agent/` orchestrates conversations, `llm/` implements provider clients, `tools/` handles workspace operations and command policy, and `tui/`, `repl/`, and `headless/` provide interaction modes. Configuration, sessions, MCP, hooks, and subagents have separate packages. Tests sit beside source files; TUI rendering fixtures live in `internal/tui/testdata/render/`. Supporting documentation is in `docs/`; compiled binaries go into ignored `bin/`.

## Build, Test, and Development Commands

Use Go 1.26 or newer, matching `go.mod` and CI. `nix develop` provides the development tools.

- `make run`: launch the terminal app locally; configure credentials with `miao init` or `MIAO_API_KEY`.
- `make build`: compile `bin/miao`.
- `make fmt`: format Go sources; `make fmt-check` checks without rewriting.
- `make test`: run all Go tests.
- `make race`: run tests with the race detector; requires CGO and GCC, with a Nix fallback.
- `make vet` / `make lint`: run static checks. Lint uses golangci-lint v2 and skips locally if unavailable.
- `make check`: run formatting checks, build, vet, tests, race detection, and lint before submitting.

## Coding Style & Naming Conventions

Use `gofmt` formatting and tab indentation. Keep package names lowercase, exported identifiers in PascalCase, and unexported identifiers in camelCase. Follow existing descriptive snake_case filenames, such as `command_safety.go`. Keep logic in the relevant internal package. Handle errors explicitly; `.golangci.yml` enables errcheck, govet, ineffassign, staticcheck, and unused.

## Testing Guidelines

Use Go's standard `testing` package, `*_test.go` files, and `TestXxx` functions. Prefer table-driven regression cases, `t.TempDir()`, and fake clients for isolated tests. Run focused tests with `go test ./internal/tools -run TestApprovalBypassPoCs`. No numeric coverage threshold is configured. For intentional rendering changes, regenerate fixtures with `go test ./internal/tui -run TestRenderGolden -update-golden` and review the output.

## Commit & Pull Request Guidelines

History uses `type(scope): summary`, including `fix(security): ...`, `feat(tui): ...`, and `docs: ...`; Chinese summaries are common. Keep commits focused. PRs should explain the behavior change, link relevant issues, list validation performed, and include terminal screenshots for visible TUI changes. Update documentation when behavior changes.

## Security & Configuration

Never commit `.env`, credentials, or session exports. Preserve workspace boundaries, symlink protections, command approvals, and child-process secret filtering; add regression tests when changing these controls. Dependency changes must also update `flake.nix`'s `vendorHash` and pass Nix CI checks.
