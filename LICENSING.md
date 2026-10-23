# Licensing — EX3-UI

EX3-UI is a fork of [3x-ui](https://github.com/MHSanaei/3x-ui). The project uses
two licenses for its own code; third-party binaries and data remain under their
upstream terms. The full license texts are in [`licenses/`](licenses/) and
[`internal/sidecar/LICENSE`](internal/sidecar/LICENSE).

## 1. Upstream 3x-ui code — GPL-3.0

All original 3x-ui code, and EX3-UI code that is not listed in section 2, remains
under the **GNU General Public License v3.0**. See [`LICENSE`](LICENSE) and
[`licenses/LICENSE-3x-ui-GPL-3.0.txt`](licenses/LICENSE-3x-ui-GPL-3.0.txt).

## 2. EX3-UI components derived from [LucX-UI](https://github.com/AlexeyLCP/lucx-ui) — PolyForm Noncommercial 1.0.0

The sidecar/core-management code adapted from
[LucX-UI](https://github.com/AlexeyLCP/lucx-ui) is licensed under the
**PolyForm Noncommercial License 1.0.0**. This currently covers:

- `internal/sidecar/` — the sidecar manager and protocol adapters. See
  [`internal/sidecar/NOTICE`](internal/sidecar/NOTICE) and
  [`internal/sidecar/LICENSE`](internal/sidecar/LICENSE).
- The related core-management integration:
  `internal/web/controller/cores.go`,
  `internal/web/job/sidecar_job.go`,
  `internal/web/service/sidecar_cores.go`,
  `internal/web/service/sidecar_migrate.go`,
  `frontend/src/api/queries/useCores.ts`,
  `frontend/src/pages/settings/CoresTab.tsx`,
  `frontend/src/pages/settings/CoresTab.css`,
  `frontend/src/pages/inbounds/form/protocols/sidecar.tsx`,
  `frontend/src/schemas/protocols/inbound/sidecar.ts`, and
  `docs/content/docs/en/reference/api/cores.mdx`.

These components are **not sublicensed under GPL-3.0**. They are free for
personal, educational, research, charity, and government use. Commercial use
(including paid VPN resale or embedding them in a commercial product) requires
explicit written permission from the LucX-UI author. See
[`licenses/LICENSE-lucx-PolyForm-Noncommercial-1.0.0.txt`](licenses/LICENSE-lucx-PolyForm-Noncommercial-1.0.0.txt).

## 3. Third-party binaries and data

The panel supervises external processes. These binaries are not statically
linked into the EX3-UI Go binary, and their licenses remain with their upstream
authors.

| Component | License | License file |
|---|---|---|
| 3x-ui | GPL-3.0 | `licenses/LICENSE-3x-ui-GPL-3.0.txt` |
| Xray-core | MPL-2.0 | `licenses/LICENSE-xray-core-MPL-2.0.txt` |
| qWDTT (`bin/extra-qwdtt`) | GPL-3.0 | `licenses/LICENSE-qwdtt-GPL-3.0.txt` |
| olcRTC (`bin/extra-olcrtc`) | WTFPL-2.0 | `licenses/LICENSE-olcrtc-WTFPL-2.0.txt` |
| TUIC (`bin/tuic-server*`) | GPL-3.0 | `licenses/LICENSE-tuic-GPL-3.0.txt` |
| mtg (`bin/mtg-linux-*`) | MIT | `licenses/LICENSE-mtg-multi-MIT.txt` |
| zapret | MIT | `licenses/LICENSE-zapret-linux-easy-MIT.txt` |
| [CSQTT](https://github.com/amurcanov/csqtt) (`bin/csqtt-linux-*`) | PolyForm Noncommercial 1.0.0 (amurcanov) | `licenses/LICENSE-csqtt-PolyForm-Noncommercial-1.0.0.txt` |
| Geo `.dat` files | Upstream dataset terms | See the dataset sources in `licenses/THIRD-PARTY-NOTICES.md` |

CSQTT is a separate program by amurcanov. LucX-UI's commercial permission does
not cover CSQTT. Redistribute CSQTT with its own license and required notices.

## 4. What this means in practice

- **Personal, educational, research, charity, and government use** is free.
- **Commercial use of the PolyForm components** requires written permission from
  the respective author (LucX-UI for the adapted sidecar code, amurcanov for
  CSQTT).
- **GPL-3.0 obligations** for upstream 3x-ui code and for GPL-3.0 sidecar
  binaries (TUIC, qWDTT) still apply.
- Do not remove license texts, copyright notices, or attribution.
- The GPL-3.0 and PolyForm components are distributed together but are not
  relicensed into one another. If you need a single-license GPL-3.0 build, build
  without the PolyForm components.

See [`licenses/THIRD-PARTY-NOTICES.md`](licenses/THIRD-PARTY-NOTICES.md) for the
full third-party attribution list.

## 5. Contact

For commercial licensing of the LucX-UI-derived components, open an issue at
<https://github.com/AlexeyLCP/lucx-ui/issues> or contact the LucX-UI author.
For CSQTT commercial licensing, contact amurcanov through
<https://github.com/amurcanov/csqtt>.
