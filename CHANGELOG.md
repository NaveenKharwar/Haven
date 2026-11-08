# Changelog

Each release's notes come from its section here. Add a `## <version>` section before you tag. If there is none, the release notes list the commit titles since the last release.

## 0.1.29

- **SnapOtter in catalog**:
  - Added SnapOtter (`catalog/snapotter.yaml`), an open-source, self-hosted file-processing platform with over 200 utilities across images, videos, audio, PDFs, and documents.
  - Includes local AI capabilities (OCR, transcription, photo restoration, background removal) and a layer-based web editor.
  - Configured embedded PostgreSQL 17 and Redis 8 single-container runtime, web port 1349, automated user/permission remapping (`PUID`/`PGID`), volume mapping, and extended startup timeout (`start_timeout: 300`) to guarantee reliable first-boot initialization and health checks.

## 0.1.28

- **Full server access for file managers**:
  - Configured FileBrowser Quantum, BoxBox, Copyparty, and Filestash to default their storage paths to `/` with root permissions (`user: "0:0"`), allowing users to browse, organize, edit, upload, and manage files anywhere on their Ubuntu server out of the box.
  - **FileBrowser Quantum**: Fixed initial admin password configuration (`admin` / `admin123`) via `FILEBROWSER_ADMIN_PASSWORD` and updated login instructions.
  - **BoxBox**: Configured container with `allow_root_mount: true` and mapped the host filesystem as the primary `server` drive, removing empty desktop/media folders from the interface.
- **Karakeep boot and health check fix**:
  - Fixed Karakeep installation failure caused by Zod URL validation errors on bare hostnames during database migration.
  - Enhanced frontend URL prefill in `Store.tsx` and `TV.tsx` to automatically generate full, valid URLs (`http://<host>:<port>`) for URL settings.
  - Added backend URL normalization (`normalizeSettingValue`) in `apps.go` and safe compose defaults (`${PUBLIC_URL:-http://localhost:3000}`).

## 0.1.27

- **Fix file manager catalog templates**:
  - **BoxBox**: Added `user: "${PUID}:${PGID}"`, mapped `${APP_DATA}/tmp:/tmp/boxbox`, and mapped storage to `/home/user`, resolving the container startup crash (`chmod /data: operation not permitted`).
  - **FileBrowser Quantum**: Fixed config path to use internal default `/home/filebrowser/config.yaml` and set initial admin password, preventing fatal error on missing config.
  - **Copyparty**: Configured volume command `-v /w/files:/:rwmda -e2dsa` to override Docker zero-access failsafe and enable read/write/delete and media search out of the box.

## 0.1.26

- **New file managers in catalog**: Added four popular, actively maintained web file managers to the app store catalog:
  - **FileBrowser Quantum**: Modern fork of FileBrowser with on-the-fly media transcoding and document editing.
  - **BoxBox**: Homelab file manager built with Go and SvelteKit featuring desktop keyboard shortcuts and Monaco code editor.
  - **Filestash**: Web client supporting local files, SFTP, S3 buckets, FTP, and WebDAV.
  - **Copyparty**: High-speed, lightweight file server with resumable uploads and WebDAV support.
- **Catalog screenshot galleries**: Added verified UI screenshots to catalog entries for visual previews in the Haven app store.

## 0.1.25

- **Fix update notice on `latest` and custom versions**: Apps configured to track `latest` or newer custom semver versions no longer display a false update notice pointing back to the catalog's default version.
- **Preserve app version during updates**: Running an update on an app now preserves its configured version (such as `latest`) instead of resetting it back to the catalog default.

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
