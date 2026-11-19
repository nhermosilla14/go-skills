# release

Building, testing and publishing the binary.

## Behaviours

### Targets and metadata
One build produces `windows/386`, `windows/amd64`, `linux/386`, `linux/amd64`, `linux/arm64`,
`linux/arm` (`GOARM=6`), `darwin/amd64` and `darwin/arm64`, with `CGO_ENABLED=0` and `-trimpath`.
Version, commit and date are injected at link time; `skills --version` prints all three and a plain
`go build` reports development defaults.
- Criteria: every target builds; version output matches the injected values.
- Tests: TestE2EVersionMetadata, cross-compile CI job, scripts/smoke.sh

### Artifacts
Tagged releases publish `.zip` archives for Windows and `.tar.gz` for Linux and macOS, named
`skills_<version>_<os>_<arch>` (`armv6` for arm), plus `checksums.txt` with SHA-256, through
GoReleaser driven by GitHub Actions.
- Criteria: a `v*` tag runs tests then publishes; archives contain the binary.
- Tests: release workflow, `.goreleaser.yaml` checked by `goreleaser check`

### Verification
The merge gate checks branch records and requires every spec entry to name tests or be marked
unverified; CI runs gofmt, `go vet`, `golangci-lint`, `go test -race` on Linux, macOS and Windows,
cross-compiles all targets, and runs the smoke script on the host build.
- Criteria: every job fails the pull request on failure.
- Tests: scripts/check-branch-docs.sh, .github/workflows/merge-gate.yml

## Not supported

- Code signing and Apple notarization.
- Package-manager distribution (Homebrew, Scoop, apt) and container images.
- Reproducible-build attestations.
