# Licensing — EX3-UI

EX3-UI is a fork of [3x-ui](https://github.com/MHSanaei/3x-ui). The project's own
code is under the **GNU General Public License v3.0**; third-party binaries and
data remain under their upstream terms. The full license texts are in
[`licenses/`](licenses/).

## 1. Project code — GPL-3.0

All EX3-UI code, including the sidecar manager and core-management integration in
`internal/sidecar/` and the panel layer that drives it, is under the **GNU
General Public License v3.0**. See [`LICENSE`](LICENSE) and
[`licenses/LICENSE-3x-ui-GPL-3.0.txt`](licenses/LICENSE-3x-ui-GPL-3.0.txt).

Releases up to and including 1.5.0 contained panel-side code adapted from
[LucX-UI](https://github.com/AlexeyLCP/lucx-ui) under the PolyForm
Noncommercial License 1.0.0. That code has since been replaced by an independent
implementation, so no part of the current tree carries those terms. The notices
for the earlier releases are retained in
[`licenses/THIRD-PARTY-NOTICES.md`](licenses/THIRD-PARTY-NOTICES.md).

## 2. Third-party binaries and data

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

CSQTT is a separate program by amurcanov. Redistribute CSQTT with its own
license and required notices.

## 3. What this means in practice

- **The panel and the GPL-3.0 sidecar binaries** (TUIC, qWDTT) keep their usual
  GPL-3.0 obligations.
- **Commercial use of CSQTT** requires a separate written permission from
  amurcanov.
- Do not remove license texts, copyright notices, or attribution.

See [`licenses/THIRD-PARTY-NOTICES.md`](licenses/THIRD-PARTY-NOTICES.md) for the
full third-party attribution list.

## 4. Contact

For CSQTT commercial licensing, contact amurcanov through
<https://github.com/amurcanov/csqtt>.
