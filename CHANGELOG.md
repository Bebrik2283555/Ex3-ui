# Changelog

## 1.4.2 — 2026-10-23

Based on 3x-ui 3.10.2.

### Added

- CSQTT inbound protocol: TURN/RTP sidecar, per-client credentials, share links,
  subscription output, traffic accounting, and core API endpoints.
- Unified core management adapted from LucX-UI: qWDTT, olcRTC, and CSQTT now use
  a single sidecar manager with per-inbound lifecycle, status, traffic, and
  migration from the previous extra-core layout.
- Settings → Cores page for starting, stopping, and monitoring external cores.
- TUIC sidecar is included in release archives and installed with both
  `bin/tuic-server` and `bin/tuic-server-linux-<arch>` names.
- `install.sh --local <archive>` for offline installation from a release archive.
- `install-extra.sh` installs qWDTT, olcRTC, CSQTT, and TUIC from the release
  archive, with a local-archive mode for offline use.

### Changed

- `install.sh`, `install-extra.sh`, and `update.sh` verify the release
  `.sha256` when it is published.
- Release sidecar binaries are made executable on install and update. This fixes
  qWDTT, olcRTC, CSQTT, and TUIC failing after an update with `permission denied`.
- `uninstall-extra.sh` now also removes TUIC and the legacy `bin/tuic` directory.
- `x-ui uninstall` keeps `/root/cert`, so certificates issued by the panel's
  ACME flow survive an uninstall.
- CSQTT clients use only their own `csqttPassword`; the inbound main password
  and the generic client password are no longer fallbacks.
- Third-party notices updated for CSQTT (PolyForm Noncommercial 1.0.0),
  LucX-UI-derived components (PolyForm Noncommercial 1.0.0), and TUIC (GPL-3.0).

### Fixed

- Sidecar inbounds routed through Xray now restart the core when the egress
  bridge is added, changed, or removed, so new and edited sidecars work
  immediately instead of waiting for a manual restart.
- CSQTT client synchronization is retried by the reconcile tick after a failed
  startup push, instead of succeeding only once.
- Sidecar traffic is attributed with the per-protocol client password, so
  qWDTT and CSQTT quota accounting works for newly created clients.
- Editing a CSQTT client password no longer drops the sidecar SQLite store, so
  traffic counters and clients created in csqtt's own panel survive.
- Clearing a qWDTT client's expiry or VK hash now removes the old
  `expires_at`/`vk_hash` values from `passwords.json`.
- Saving an inbound now rejects sidecar `wgPort`, `rawPort`, `listenDirect`,
  `webPort`, and loopback `routeXrayPort` collisions instead of starting a core
  that cannot bind its socket or an Xray bridge that is silently skipped.
- Full backup now fails with the offending path instead of returning a ZIP with
  unreadable or oversized certificates silently omitted. Restore rejects
  manifest-listed certificates that are missing or too large.
- A data race in core status reads (`Manager.StatusOf`) is fixed.

### Licensing

CSQTT and the LucX-UI-derived core-management code are distributed under the
PolyForm Noncommercial License 1.0.0. Commercial use requires separate written
permission from the respective authors. See `licenses/THIRD-PARTY-NOTICES.md`.
