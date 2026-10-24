# Security

Haven runs as root on your server and can control Docker, the network and files, so please treat it carefully.

## Reporting a problem

Please report security problems privately to the repository owner rather than opening a public issue. Include what you found and how to reproduce it.

## What to know when running it

- Don't expose Haven's port (8484) to the internet. Use Tailscale for remote access.
- There is exactly one admin. Managers, members and guests are limited by the server, not only by the interface.
- The file explorer opens only safe folders unless the admin turns on Advanced access.
- The first-run setup code is printed once in the service log and saved in `/opt/haven/setup-token` until the admin account exists.
