# Releasing Haven

This page is for the people who publish Haven releases. If you only want to run Haven, you don't need any of it: see the [README](../README.md).

## How releases work

Pushing a tag such as `v0.2.0` starts the **Release** workflow (`.github/workflows/release.yml`). It tests the code, builds the Linux binaries for Intel/AMD and ARM, signs the checksum list, and publishes a GitHub Release with:

| File | What it is |
|---|---|
| `haven-linux-amd64`, `haven-linux-arm64` | The program |
| `SHA256SUMS`, `SHA256SUMS.sig` | Checksums of the binaries and install scripts, and their signature |
| `get.sh` | The one-line installer. The workflow fills in the repository name, so the same file works under any repository name |
| `install.sh`, `uninstall.sh` | The scripts `get.sh` runs |
| `haven-<version>.tar.gz` | Everything in one archive, for installing by hand |

The release notes come from the `## <version>` section of [CHANGELOG.md](../CHANGELOG.md). If there is none, they list the commit titles since the last release.

Installed copies of Haven check the repository they were built from once a day. When a newer release exists, the admin sees it on the Updates page and installs it with one click. Haven verifies the signature and checksum first, keeps the old version, and goes back to it if the new one keeps failing.

## One-time setup for a repository

Releases are signed so that no one who gets into a GitHub account can push code to people's servers. The signing key is only needed by whoever publishes releases, never by people who install Haven.

1. Make a signing key: `go run ./tools/sign keygen ~/.haven/release-signing-key`. Back it up somewhere safe.
2. The command prints the **public key**. Put it in `updatePublicKey` in `selfupdate.go`. Servers accept only releases signed by that key.
3. Add the private key to the repository as a secret: `gh secret set HAVEN_SIGNING_KEY < ~/.haven/release-signing-key`

If the key is lost, installed servers can't accept updates until they are reinstalled with a build that carries a new public key.

## Keeping the key safe

The public key in `selfupdate.go` is meant to be public. It only lets Haven check signatures; it can't make one. The private key is the one secret that matters: whoever holds it can ship code to every running Haven.

- Keep it in two places only: the repository secret `HAVEN_SIGNING_KEY`, and a backup in a password manager. Never paste it into an issue, a pull request, a chat or a log.
- Turn on two-factor sign-in for the GitHub account that owns the repository. The release workflow can read the secret.
- Protect the repository: require pull requests for `main`, and limit who can create tags that start with `v`. Secrets are not shared with pull requests from forks, so leave that default alone.
- A fork can't publish releases that your servers accept, because it doesn't have your key. A fork that wants its own updates puts its own public key in `selfupdate.go`.

## If the key leaks

Swap the key in two releases, so servers that are already installed keep updating:

1. Make a new key: `go run ./tools/sign keygen ~/.haven/release-signing-key-new`.
2. Put the **new** public key in `updatePublicKey`. Leave `HAVEN_SIGNING_KEY` as the **old** key for now.
3. Tag a release. It is signed with the old key, so installed servers accept it, and the program inside now trusts the new key.
4. Replace the `HAVEN_SIGNING_KEY` secret with the new private key and delete the old one everywhere.
5. Tag one more release. It is signed with the new key. After servers install it, the old key is useless.

Servers that skip the first release can't verify the second one. Tell people to update promptly, or reinstall with the one-line installer, which always fetches the newest release.

If you only suspect a leak and the key was never published, do the same swap anyway. If the key is lost rather than leaked, there is no old key to sign step 3 with, so servers have to be reinstalled with a build that carries the new public key.

## Making a release

1. Add a `## <version>` section to `CHANGELOG.md` describing what changed.
2. Commit and push, then tag and push the tag:

```bash
git tag v0.2.0 && git push origin v0.2.0
```

3. Watch the Release workflow on the Actions tab. When it is green, the release is published and `releases/latest/download/get.sh` points at it.
