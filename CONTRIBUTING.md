# Contributing to Haven

Thank you for helping improve Haven. Keep changes focused, explain behavior in plain language, and preserve the ability to manage apps without Haven. Read the README and, for interface work, [design.md](design.md) before starting.

## Branches

Create a short-lived branch from the current target branch. Use lowercase words separated by hyphens after the slash:

- `feature/short-description` for features and enhancements.
- `bug/short-description` for fixes intended for the next release.
- `hotfix/short-description` for an urgent fix to a released version.

For example, `feature/backup-health-widget`. Include an issue number when useful. Open feature and bug pull requests against the development branch chosen by the maintainers (normally `main`); base urgent hotfixes on the affected release and coordinate bringing them back to development. The branch naming is adapted from the [Git branching guide](https://gist.github.com/digitaljhelms/4287848); Haven's actual release process is described in [docs/RELEASING.md](docs/RELEASING.md).

## Commits

Use concise Conventional Commits:

```text
type(scope): imperative summary
```

The scope is optional. Use lowercase types such as `feat`, `fix`, `refactor`, `perf`, `docs`, `test`, `build`, `ci`, `style`, `chore`, or `revert`. Write the summary as an imperative (for example, `add`, not `added`), without a trailing period. Aim for at most 50 characters for the whole subject; never exceed 72.

```text
feat(widgets): add backup health
fix(files): preserve selection
```

Omit the body when the subject tells the whole story. When context is needed, leave a blank line after the subject and explain why the change is needed rather than repeating the diff. Wrap body lines at 72 characters, use `-` for bullets, and put issue references at the end (for example, `Closes #42`). Include a body for breaking changes, security fixes, data migrations, and reverts; describe the impact and any required action. Mark breaking changes with `!` and a `BREAKING CHANGE:` footer where appropriate.

Keep related changes in reviewable commits. Do not include filler, emoji, or tool attribution in commit messages. Never commit secrets, local data, or generated build artifacts unless the project explicitly tracks them.

## Changes and checks

- Add user-facing changes to `CHANGELOG.md` under `Unreleased`; release maintainers move those notes into a version section before tagging. Describe the effect, not just implementation details.
- Update the README or other documentation when behavior, setup, security, or user-facing features change.
- For Go changes, run `go test ./...`. For web changes, run `cd web && npm run build`. Run `bash scripts/test-get.sh` when changing the installer. Add focused tests for behavior and failure cases where appropriate.
- Keep role checks on the server, handle failures clearly, and avoid exposing credentials or private server information.
- Follow existing design tokens, components, responsive layouts, and keyboard accessibility for UI changes.

## Review

Describe the problem, the change, and how you tested it in the pull request. Call out limitations and migration or compatibility concerns. Keep unrelated work separate. Resolve feedback before merging, and follow the maintainer's direction on release and merge timing.
