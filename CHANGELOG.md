# Changelog

## [1.0.3](https://github.com/mgoodness/eve-trader/compare/v1.0.2...v1.0.3) (2026-08-29)


### Bug Fixes

* treat a 404 page as end of pagination for ESI market orders ([#31](https://github.com/mgoodness/eve-trader/issues/31)) ([808f28a](https://github.com/mgoodness/eve-trader/commit/808f28a4644ae313f8e3b7ef93254d32b7b9023c))

## [1.0.2](https://github.com/mgoodness/eve-trader/compare/v1.0.1...v1.0.2) (2026-08-29)


### Bug Fixes

* redirect to the dashboard after completing ESI login ([#30](https://github.com/mgoodness/eve-trader/issues/30)) ([7af4ff6](https://github.com/mgoodness/eve-trader/commit/7af4ff653d4a8c0d4cfb82665c738249473c90be))
* skip CI on release-please's own PR branch ([02d5d49](https://github.com/mgoodness/eve-trader/commit/02d5d4912e2b864c1c4626ebda173fa3869ec086))

## [1.0.1](https://github.com/mgoodness/eve-trader/compare/v1.0.0...v1.0.1) (2026-08-29)


### Bug Fixes

* use TARGETPLATFORM-qualified COPY path for goreleaser dockers_v2 ([e4cb5aa](https://github.com/mgoodness/eve-trader/commit/e4cb5aa3c66519f18f9947a0c270e81be06fc955)), closes [#20](https://github.com/mgoodness/eve-trader/issues/20)

## 1.0.0 (2026-08-29)


### Features

* /fix: commit. ([c5129b1](https://github.com/mgoodness/eve-trader/commit/c5129b1588456480dc304c5f2edfce0f1a6a354c))
* add alert detection and in-app surfacing ([84cbcc9](https://github.com/mgoodness/eve-trader/commit/84cbcc93af39bc4bdad694988cd5b9e6fea28b9a)), closes [#17](https://github.com/mgoodness/eve-trader/issues/17)
* add Discord alert delivery ([0e75a3b](https://github.com/mgoodness/eve-trader/commit/0e75a3b1927381089f9edb15b5dbb8914e26d415)), closes [#18](https://github.com/mgoodness/eve-trader/issues/18)
* add EVE SSO login and refresh-token persistence ([7df6c38](https://github.com/mgoodness/eve-trader/commit/7df6c38c75d8f35d0ff70bb733bc5c6094bef398)), closes [#15](https://github.com/mgoodness/eve-trader/issues/15)
* add read-only order-tracking dashboard ([6e0afe4](https://github.com/mgoodness/eve-trader/commit/6e0afe4bab08ac951886c9cce410f989a34db120)), closes [#16](https://github.com/mgoodness/eve-trader/issues/16)
* add release automation and Docker image build (CI/CD, part 1 of 2) ([19f9c9b](https://github.com/mgoodness/eve-trader/commit/19f9c9b71f8858ed9d94c02fa0f714668f364038)), closes [#20](https://github.com/mgoodness/eve-trader/issues/20)
* add the Opportunity Scanner (Jita + Rens) ([7151774](https://github.com/mgoodness/eve-trader/commit/7151774aeb1d285aa6a2eb983ab6a5bf9d3c4ac9)), closes [#19](https://github.com/mgoodness/eve-trader/issues/19)
* add walking skeleton (HTTP server, SQLite storage, ESIClient seam, CI) ([83c4898](https://github.com/mgoodness/eve-trader/commit/83c4898acede25b7f0f819129894d6b2754e80d7)), closes [#14](https://github.com/mgoodness/eve-trader/issues/14)


### Bug Fixes

* allow manually re-triggering release-please without an empty commit ([c5129b1](https://github.com/mgoodness/eve-trader/commit/c5129b1588456480dc304c5f2edfce0f1a6a354c)), closes [#20](https://github.com/mgoodness/eve-trader/issues/20)
* configure SQLite for safe concurrent access ([ff88c58](https://github.com/mgoodness/eve-trader/commit/ff88c58203f6f2638ae365339a26e3aead289d66)), closes [#23](https://github.com/mgoodness/eve-trader/issues/23)
* wizard's first-deploy stage ran before any image was ever published ([9412139](https://github.com/mgoodness/eve-trader/commit/9412139a4da5adff2f2368fc2beebf14deeef35a)), closes [#20](https://github.com/mgoodness/eve-trader/issues/20)
