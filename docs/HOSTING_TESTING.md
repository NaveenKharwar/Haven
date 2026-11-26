# Hosting verification

Hosting is an experimental, administrator-only feature. This document records the supported evidence, remaining limitations, and checks required before the experimental flag can be removed.

## Current status

| Area | Status | Evidence and limitation |
| --- | --- | --- |
| Coolify installation | Passed | Haven installed the pinned Coolify 4.7.6 release on a clean Linux VM and reached a healthy dashboard, database, and Redis service. |
| Interrupted installation recovery | Passed | Haven detected a genuinely interrupted installation and resumed the same pinned installer without deleting existing data. |
| Static HTML | Passed | A Haven-guided site deployed privately, answered through its loopback-only port, and retained recoverable source. |
| WordPress | Passed for private use | Private deployment, retry, content restore, administrator sign-in, media, and reboot recovery were verified. |
| Ghost | Partial | Private deployment, owner setup, posts, and uploads were verified. Transactional email and a full data restore still require testing. |
| Modern frameworks | Passed as a Coolify handoff | A React/Vite repository built and served privately through Coolify. Git, build-pack, Dockerfile, image, and advanced Compose resources remain managed by Coolify rather than Haven’s guided workflow. |
| Cloudflare discovery | Passed | Haven discovered the exact active account and zone using a narrowly scoped token. |
| Cloudflare Tunnel | Passed with limitation | Exact-hostname publication, trusted HTTPS, reboot recovery, and unpublish were verified. The documented narrow token also passed the live API lifecycle and cleanup workflow. Public connector and HTTPS E2E automation remain outstanding. |
| Direct HTTPS safeguards | Passed | Review binding, confirmation, DNS ownership, public-address validation, proxy checks, and Coolify automatic-DNS checks are covered. |
| Direct public HTTPS | Unverified | The available test network is behind upstream NAT, so no end-to-end inbound HTTPS or direct certificate was claimed. No router or DNS change was made. |
| Certificate renewal and public IP change | Unverified | Initial trusted HTTPS was verified for Tunnel publishing. A natural renewal cycle and managed direct-IP change were not completed. |
| Responsive interface | Passed | The enabled Hosting page and Static HTML dialog rendered at desktop and 390 px mobile widths without horizontal overflow or console errors. |
| Coolify control-plane restore | Passed with limitation | Coolify configuration and service records were restored in an isolated clone. Application databases and files still require their own coordinated backups. |

## Automated checks

The hosting tests cover:

- guided Compose contracts and loopback-only website ports;
- database isolation and dedicated connector networks;
- administrator authorization and explicit publication review;
- secret-file permissions and file-explorer symlink protection;
- exact Cloudflare zone discovery;
- Tunnel route allowlisting and the final 404 fallback;
- DNS ownership, conflict detection, retry, and cleanup;
- hostname and public-address validation;
- Traefik HTTPS route verification; and
- rejection of conflicting Coolify automatic DNS.

Run the project checks before merging:

```sh
go vet ./...
go test ./...
cd web
npm ci --no-fund --no-audit
npx tsc --noEmit -p .
npm run build
```

Provider tests use controlled stubs unless a row above explicitly says the behavior was verified live.

### Automation coverage

| Capability | PR tests | Live API workflow | Public E2E workflow |
| --- | --- | --- | --- |
| Zone discovery | Covered with a provider stub | Passed | Not applicable |
| Tunnel creation and configuration | Route validation covered with a provider stub | Passed | Not applicable |
| DNS ownership and cleanup | Covered with a provider stub | Passed | Not applicable |
| Conflict safeguards | Covered with a provider stub | Passed | Not applicable |
| Connector online | Not covered | Not covered | Planned |
| Website reachable through Tunnel | Not covered | Not covered | Planned |
| Trusted public HTTPS | Not covered | Not covered | Planned |
| Unknown hostname returns 404 | Fallback configuration covered | Configuration check passed | Planned response check |

The live API workflow verified real Cloudflare permissions, request and response compatibility, resource ownership, and cleanup without claiming public connectivity. The public E2E workflow is required before removing the experimental flag.

### Configure the live API workflow

The manual **Cloudflare API lifecycle** workflow runs only from `main`. Create a protected GitHub environment named `cloudflare-integration`, require approval if available, and add:

- environment secret `CLOUDFLARE_API_TOKEN`; and
- environment variable `CLOUDFLARE_TEST_DOMAIN` containing the active root domain.

Scope the token to **Cloudflare Tunnel → Edit** for the selected account and **Zone → Read** plus **DNS → Edit** for the selected zone. Set an expiration and rotate it. Do not expose this environment to pull-request jobs. The workflow creates uniquely named `haven-ci-*` resources and runs ownership-checked cleanup both inside the test and in an unconditional final step.

## Preview release checklist

Use disposable Linux hosts and synthetic content. Keep credentials, provider identifiers, private addresses, and backups out of logs and screenshots.

### Private deployment

- [ ] Run preflight on a clean supported Linux host.
- [ ] Confirm setup cannot start without explicit approval.
- [ ] Install or recover the pinned Coolify release.
- [ ] Connect the local server, project, environment, and destination.
- [ ] Deploy each guided template privately.
- [ ] Confirm website ports bind only to loopback and databases publish no host ports.
- [ ] Cause and recover from a failed deployment without creating a duplicate service.
- [ ] Reboot and verify Haven, Coolify, websites, databases, and private content recover.

### Backup and recovery

- [ ] Back up application files and databases at a consistent point.
- [ ] Save Haven recovery metadata and Coolify configuration separately from credentials.
- [ ] Restore into an isolated host with public routes disconnected.
- [ ] Verify sign-in, content, uploads, image compatibility, and storage ownership.
- [ ] Confirm restored provider metadata cannot publish without a new explicit review.

### Cloudflare Tunnel

- [ ] Use the documented account- and zone-scoped token.
- [ ] Review and confirm the exact site, hostname, route, and DNS effect.
- [ ] Verify trusted HTTPS and intended content from outside the LAN and Tailscale.
- [ ] Confirm unknown hostnames, dashboards, databases, and unrelated services remain private.
- [ ] Reboot and verify the connector and route recover.
- [ ] Unpublish and confirm Haven’s exact route and DNS record are removed without changing neighboring records.
- [ ] Test token rotation and interrupted publish/unpublish retries.

### Direct HTTPS

- [ ] Use an isolated host with a real reachable public address and no unrelated proxy routes.
- [ ] Confirm the router WAN address matches the independently observed public address.
- [ ] Verify Coolify automatic DNS is disabled for the selected zone.
- [ ] Forward only TCP 80 and 443 when the approved test network requires it.
- [ ] Verify trusted HTTPS, intended content, dashboard isolation, and HTTP-to-HTTPS behavior externally.
- [ ] Test certificate renewal and a managed public-IP change.
- [ ] Unpublish, remove manual DNS and unused forwarding rules, and confirm private access remains available.

## Criteria for stable hosting

Keep `HAVEN_HOSTING_EXPERIMENTAL=1` required until:

1. WordPress and Ghost install, update, backup, and full restore pass on supported Linux hosts.
2. Tunnel publication, rotation, interruption recovery, and cleanup pass with the documented narrow token.
3. Direct HTTPS, renewal, public-IP change, and cleanup pass from an independent network.
4. Reboot and failure recovery do not expose dashboards, databases, unrelated applications, or unmanaged provider resources.
5. Issues found during real homelab use are fixed and covered by repeatable tests.

Hosting customer workloads would require a separate multi-tenant isolation, abuse-control, and support design.
