# Releasing

A Go module has no publish step. Pushing a tag **is** the release: the module
proxy (`proxy.golang.org`) and `pkg.go.dev` fetch the tagged commit from GitHub
the first time anyone asks for it, and the checksum database records it for
good. A tag can never be reused, so everything below is arranged to check the
commit before the tag becomes public knowledge, and to make the version visible
right away.

## What the maintainer does

1. Run the CI workflow on `main` and wait for green: Actions → CI → Run
   workflow, or `gh workflow run CI --ref main && gh run watch`. It does not
   run on its own; the release run repeats the same checks, but a red
   release run leaves a tag behind that has to be moved.
2. In `CHANGELOG.md`, move the entries under `## [Unreleased]` to a new heading
   with today's date, and add the compare links at the bottom:

   ```
   ## [Unreleased]

   ## [0.2.0] - 2026-10-01
   ### Added
   - ...

   [Unreleased]: https://github.com/nayan9229/jsonflat/compare/v0.2.0...HEAD
   [0.2.0]: https://github.com/nayan9229/jsonflat/compare/v0.1.0...v0.2.0
   ```

   The release workflow refuses a tag whose version has no dated section.
3. Commit that to `main` (through a pull request if branch protection requires
   it) and wait for CI.
4. Tag the merge commit and push the tag:

   ```
   git checkout main && git pull
   git tag v0.2.0
   git push origin v0.2.0
   ```

   Plain `vMAJOR.MINOR.PATCH` only. `v0.2`, `0.2.0`, `v0.2.0-rc1` and
   `v0.2.0+build` do not start the workflow, and if one slipped through the
   glob the first job would reject it. Check a tag before pushing with
   `scripts/validate-tag.sh v0.2.0`.
5. Watch **Actions → Release**. When it is green, the release page, pkg.go.dev
   and the docs site are all up to date. The job summary has the links.

## What the workflow does

`.github/workflows/release.yml`, one trigger: a push of a tag matching
`v[0-9]+.[0-9]+.[0-9]+`. Runs for the same tag never overlap.

| Job | Checks |
|---|---|
| **validate** | Tag matches `^v(0\|[1-9][0-9]*)\.(0\|[1-9][0-9]*)\.(0\|[1-9][0-9]*)$`; its major version matches the module path (`/vN` suffix, or none for 0 and 1); the tagged commit is on the default branch; `CHANGELOG.md` has exactly one `## [X.Y.Z] - YYYY-MM-DD` heading with content, which becomes the release notes; the `go` directive is not newer than the oldest Go in the test matrix. |
| **lint** | `gofmt`, `go vet`, tidy `go.mod`/`go.sum`, staticcheck, govulncheck, actionlint, doc-comment audit (`scripts/doc-check.sh`). |
| **test** | `go test` on Go 1.24 and stable (Linux), stable on macOS and Windows; race detector on Linux and macOS. |
| **bench** | Every `BenchmarkAppend*`, `BenchmarkRudderEach` and `BenchmarkEachParallel` line must say `0 allocs/op` (`scripts/check-allocs.sh`). |
| **fuzz** | 30 s each of `FuzzAppend` and `FuzzEach`. |
| **build** | `go build` plus cross-compilation for linux/amd64, linux/arm64, darwin/arm64, windows/amd64. |
| **release** | Creates the GitHub Release (`jsonflat vX.Y.Z`, marked latest) from the changelog section plus an install line and the pkg.go.dev link; then `go list -m github.com/nayan9229/jsonflat@vX.Y.Z` against `proxy.golang.org` and a `sum.golang.org/lookup` request, so the version is indexed within minutes rather than when the first user asks for it. |
| **docs** | Generates `docs/changelog.md` and `docs/_data/release.yml`, builds `docs/` with Jekyll and deploys it to GitHub Pages. Runs only after **release** succeeded, so a failed release never publishes docs. |

Every job after **validate** depends on it. **release** depends on all checks.
The same five check jobs make up `ci.yml`, which runs only when started by
hand (`workflow_dispatch`); the commands live in `scripts/` and the
`Makefile`, and `scripts/doc-check.sh` fails when the pinned tool versions
differ between the two workflows and the Makefile.

## Fixing a bad release

You cannot delete or reuse a tag once the proxy has served it: the checksum
database has recorded `vX.Y.Z` with that content, and a different commit under
the same tag would fail every `go get` with a checksum mismatch.

- **The workflow failed before the release job.** Nothing was published. Fix
  `main`, then either move the tag (`git tag -f vX.Y.Z && git push -f origin
  vX.Y.Z`) **only if** the proxy has not seen it yet (check
  `https://proxy.golang.org/github.com/nayan9229/jsonflat/@v/vX.Y.Z.info`
  returns 404), or skip the number and tag the next patch.
- **The release is out and it is wrong.** Publish a fix as the next patch
  version, and retract the bad one so `go get` and `go list -m -u` steer users
  away from it. Add to `go.mod`:

  ```
  retract v0.2.0 // Wrong output for ... ; use v0.2.1.
  ```

  Tag the commit that carries the `retract` directive as `v0.2.1`. The
  retraction takes effect once that version is fetched by the proxy. Mention
  it under the new version in `CHANGELOG.md`.
- **The docs job failed** but the release succeeded. Re-run the failed jobs
  from the Actions page; the release step recognises an existing release and
  leaves it alone.

## Starting a `/v2` line

A breaking change after `v1.0.0` needs a new module path.

1. Change `go.mod` to `module github.com/nayan9229/jsonflat/v2` and update the
   import paths in the repository (including `example/` and the tests).
2. Update the module path in `release.yml` (`MODULE`), the README badges and
   install lines, and `docs/`.
3. Tag `v2.0.0`. `scripts/validate-tag.sh` refuses `v2.x.y` until the suffix is
   there, and refuses `v1.x.y` once it is.
4. Bug fixes for v1 continue from a `v1` branch, tagged `v1.x.y`, with `go.mod`
   still on the unsuffixed path.

Before v1.0.0 this does not apply: `v0.x` minors may change the API or the
config language, as the README's versioning policy says.

## One-off repository settings

Set by hand; a workflow cannot do these with the default token.

- **Pages**: Settings → Pages → Build and deployment → Source: **GitHub
  Actions** (or `gh api -X POST repos/OWNER/REPO/pages -f build_type=workflow`).
  Enabling Pages creates a `github-pages` environment whose deployment policy
  allows only the default branch, and the docs job runs on a tag, so add a
  tag rule: Settings → Environments → github-pages → Deployment branches and
  tags → add tag `v*` (or
  `gh api -X POST repos/OWNER/REPO/environments/github-pages/deployment-branch-policies -f name='v*' -f type=tag`).
  Without it the job fails with "Tag … is not allowed to deploy to
  github-pages". Both were done for v0.1.0. Site URL:
  `https://nayan9229.github.io/jsonflat/`.
- **About**: the description and topics are at the top of `README.md` in an
  HTML comment.
- **Branch protection** on `main`: require a pull request, no force pushes,
  no deletion. CI does not run on pull requests, so it cannot be a required
  check; reviewers run `make all` or start the CI workflow on the branch
  (`gh workflow run CI --ref <branch>`). Optionally a **tag protection rule**
  for `v*` so only maintainers can push release tags.
- **Discussions**: enable them, or remove that link from
  `.github/ISSUE_TEMPLATE/config.yml`.

## Pinned tool versions

Update them together, then run `make lint`:

- `honnef.co/go/tools/cmd/staticcheck` in `Makefile`, `ci.yml`, `release.yml`
- `golang.org/x/vuln/cmd/govulncheck` in the same three files
- `github.com/rhysd/actionlint/cmd/actionlint` in the same three files
- GitHub Actions are pinned to commit SHAs with a version comment; Dependabot
  opens a weekly pull request for those.
- The Jekyll theme (`remote_theme` in `docs/_config.yml`) is pinned to a tag.
  If the theme ever fails to fetch in the docs job, switch `_config.yml` to
  `theme: minima` and re-run the job; the pages do not depend on the theme.
