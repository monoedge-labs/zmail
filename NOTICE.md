# Third-party software

Zmail includes the following software, each under its own license.

| Module | License |
| --- | --- |
| github.com/egoist/mygo | MIT |
| github.com/lobehub/lobe-mygo | MIT |
| github.com/emersion/go-imap/v2 | MIT |
| github.com/emersion/go-message | MIT |
| github.com/zalando/go-keyring | MIT |
| github.com/clipperhouse/uax29 | MIT |
| golang.org/x/net | BSD-3-Clause |
| golang.org/x/oauth2 | BSD-3-Clause |
| modernc.org/sqlite | BSD-3-Clause |

Their transitive dependencies are listed in `go.sum`; each keeps its license.
The Lucide icons come through Lobe MyGo under the ISC license. The marks of the
mail services in `assets/brands` are from [Simple Icons](https://simpleicons.org)
(CC0-1.0); the marks belong to their owners.

The agent's face on the statistics page is the BotAvatar component of
[Libraries.dev](https://github.com/Jakubantalik/Libraries.dev) (MIT, Copyright
(c) 2026 Jakub Antalik), bundled with React (MIT) in `assets/botavatars.js`;
`scripts/botavatars.sh` rebuilds it from a pinned commit. The orbs that show
syncing and connecting are a Go port of its Thinking orbs geometry
(`internal/orb`), checked against the library's golden vectors.

The local masking model reader in `internal/guard` is a copy of
[magpie](https://github.com/yetone/magpie)'s `internal/pii/guard` (MIT,
Copyright (c) 2026 yetone), without its CoreML path. The model it reads is
[AstrLink Guard](https://huggingface.co/QuantumNous/astrlink-guard)
(Apache-2.0), which Zmail does not bundle: it is downloaded on request, at the
revision and sha256 pinned in `internal/guard/install.go`.
