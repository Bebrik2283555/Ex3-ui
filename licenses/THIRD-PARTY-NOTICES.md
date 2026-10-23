# Third-party notices

EX3-UI is a fork of [3x-ui](https://github.com/MHSanaei/3x-ui). The
3x-ui-derived portion is distributed under GPL-3.0. Some components have
separate licenses listed below. The full license texts are in this directory.
See [`../LICENSING.md`](../LICENSING.md) for the project-level license split.

| Component | License | License file |
|---|---|---|
| 3x-ui | GPL-3.0 | `LICENSE-3x-ui-GPL-3.0.txt` |
| Xray-core | MPL-2.0 | `LICENSE-xray-core-MPL-2.0.txt` |
| qWDTT | GPL-3.0 | `LICENSE-qwdtt-GPL-3.0.txt` |
| olcRTC | WTFPL-2.0 | `LICENSE-olcrtc-WTFPL-2.0.txt` |
| TUIC | GPL-3.0 | `LICENSE-tuic-GPL-3.0.txt` |
| mtg | MIT | `LICENSE-mtg-multi-MIT.txt` |
| zapret | MIT | `LICENSE-zapret-linux-easy-MIT.txt` |
| CSQTT | PolyForm Noncommercial 1.0.0 | `LICENSE-csqtt-PolyForm-Noncommercial-1.0.0.txt` |
| LucX-UI components | PolyForm Noncommercial 1.0.0; upstream 3x-ui code remains GPL-3.0 | `LICENSE-lucx-PolyForm-Noncommercial-1.0.0.txt`, `LICENSE-lucx-GPL-3.0.txt` |

## CSQTT

- Source: <https://github.com/amurcanov/csqtt>
- Copyright 2026 amurcanov.
- License: PolyForm Noncommercial License 1.0.0.
- Binary shipped as `bin/csqtt-linux-amd64` and `bin/csqtt-linux-arm64`.

Required notices from the CSQTT license:

> Copyright 2026 amurcanov.
>
> Commercial use of CSQTT requires a separate written license from the licensor.
>
> Redistributions and derivative works must retain attribution to the original
> CSQTT project and these notices.

EX3-UI's own distribution terms do not grant any commercial rights to CSQTT.
Commercial use of CSQTT requires a separate written license from amurcanov.

## LucX-UI components

- Source: <https://github.com/AlexeyLCP/lucx-ui>
- Copyright (c) 2025-2026 LucX-UI Project.
- LucX-specific components are licensed under the PolyForm Noncommercial
  License 1.0.0. Upstream 3x-ui code remains under GPL-3.0.
- EX3-UI's core/sidecar management code in `internal/sidecar/` is adapted from
  LucX-UI's `internal/lucx/tunnel/` package and related core-management code.
  The original LucX-UI copyright and license notices are retained here.

The PolyForm Noncommercial components are not sublicensed under GPL-3.0.
Commercial use (including paid VPN resale or embedding in a commercial product)
requires explicit written permission from the LucX-UI author.

## TUIC

- Source: <https://github.com/tuic-protocol/tuic>
- License: GPL-3.0.
- Binary shipped as `bin/tuic-server` and `bin/tuic-server-linux-*`.

## qWDTT and olcRTC

- qWDTT: <https://github.com/SpaceNeuroX/proxy-turn-vk-android> (GPL-3.0).
- olcRTC: <https://github.com/openlibrecommunity/olcrtc> (WTFPL-2.0).

These are external processes. Their binaries remain under their own licenses;
shipping them with EX3-UI does not relicense the panel source.
