# Changelog

Each release's notes come from its section here. Add a `## <version>` section before you tag. If there is none, the release notes list the commit titles since the last release.

## 0.1.24

- **App version selection**: Choose an app's version when installing or change it later from its settings tab. Select from the recommended default, `latest`, recent upstream release versions, or enter a custom tag with real-time container registry validation and inline error reporting.
- **Dynamic catalog image tags**: Updated all 54 catalog app templates so image tags use `${APP_VERSION}` / `${VERSION}` instead of hardcoded versions.
- **Password reset**: Added password reset support via the sign-in screen using a secure 15-minute token generated on the server (`/opt/haven/reset-token`), along with CLI subcommands and flags (`haven reset-password [user]`, `haven reset-token`).

## 0.1.23

- Removed the unused option to read a GitHub token from the data folder. It was only needed while the repository was private, and Haven now asks GitHub for public releases only.

## 0.1.22

- Fixed the one-line installer from 0.1.21, which refused to run with "this copy of get.sh was not published by a release". The release step filled in the repository name in the check as well as in the setting. The installer's tests now run that same step.
- Updating Haven no longer depends on how the repository name is capitalised.

## 0.1.21

Haven is now public.

- Install apps from a catalog of 54, with checks against your server first.
- Plain-language explanations when an install or update fails.
- One-click updates for apps and for Haven itself, signed and with automatic rollback.
- An Overview with rearrangeable widgets, a file explorer, disk cleanup, roles for other people and a TV mode.
- Server → Insights: the devices on your network and the details of the server.
