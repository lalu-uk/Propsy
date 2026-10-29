# Changelog

## [4.0.0](https://github.com/lalu-uk/Propsy/compare/v3.0.0...v4.0.0) (2026-10-29)


### ⚠ BREAKING CHANGES

* **store:** the default collection names change, so an existing deployment starts with empty data until its documents are copied from the old names (users, boards, and users/*/received and pairs) to the new ones. Set PROPSY_DB_PREFIX to keep using other names.

### Features

* **slack:** request im:write for DMs ([29117ec](https://github.com/lalu-uk/Propsy/commit/29117ec8ff526939d722004d446eca8c8b6a70a0))
* **store:** prefix Firestore collections with PROPSY_DB_PREFIX ([4f83abd](https://github.com/lalu-uk/Propsy/commit/4f83abdffdcf2d6d178f9022f5557f608cad749e))
* **webhook:** add /readyz for Cloud Run readiness probes ([dc356eb](https://github.com/lalu-uk/Propsy/commit/dc356eb659cd9b6e384213d193865e6aa30f81f4))

## [3.0.0](https://github.com/lalu-uk/Propsy/compare/v2.1.0...v3.0.0) (2026-10-29)


### ⚠ BREAKING CHANGES

* **release:** ship archives per platform, sign only checksums.txt

### Features

* add propsy and dropsy kudos bot with Slack and Firestore support ([016200a](https://github.com/lalu-uk/Propsy/commit/016200ab87da2261b3f8b25dd05447fdf821a70b))
* add propsy and dropsy kudos bot with Slack and Firestore support ([495eb0f](https://github.com/lalu-uk/Propsy/commit/495eb0fe44477e60b82c3e500b6a5e8501908973))
* add propsy entrypoint with socket and webhook modes ([17aba02](https://github.com/lalu-uk/Propsy/commit/17aba0207a490c7e6a7a58dbb59f75176c44f01f))
* **cli:** add --help and --version ([2dfc341](https://github.com/lalu-uk/Propsy/commit/2dfc3412ef9ea071a399505bee4678d26d07f7cf))
* **leaderboard:** list everyone on received and given boards ([b76493b](https://github.com/lalu-uk/Propsy/commit/b76493bf9fa8fef5cdb1a7af9f4010063f708ccf))
* leaderboards for everyone, immutable and signed releases ([f843f3b](https://github.com/lalu-uk/Propsy/commit/f843f3bd5889bae54da5f2d9626c5a689398d244))
* **readme:** add logo and credit Flaticon attribution ([9eb842c](https://github.com/lalu-uk/Propsy/commit/9eb842c7e4242f17da53f78e10aa68ab8020040d))
* **readme:** add logo and credit Flaticon attribution ([abf6a48](https://github.com/lalu-uk/Propsy/commit/abf6a489ef55ff180b8e0cb0c83fcf624a382387))
* **release:** ship archives per platform, sign only checksums.txt ([a7270ac](https://github.com/lalu-uk/Propsy/commit/a7270ac720456fd06ff79bc4871fc916e20cc5b3))
* **slack:** open a give form when /propsy or /dropsy has no recipients ([e198f2a](https://github.com/lalu-uk/Propsy/commit/e198f2a805b2ac6751580f5c6f5b43ac5c687c40))
* **slack:** open a give form when /propsy or /dropsy has no recipients ([f35e12c](https://github.com/lalu-uk/Propsy/commit/f35e12ced00691ef262c0e666da26ac37bd7ea99))
* **store:** keep local data in a JSON file instead of SQLite ([0580222](https://github.com/lalu-uk/Propsy/commit/05802222314ae5a20845c93fb4d4964ae0dadd28))
* **store:** keep local data in a JSON file instead of SQLite ([bdfea78](https://github.com/lalu-uk/Propsy/commit/bdfea780d80d50902413311b1fe1a326fd9ac914))


### Bug Fixes

* **config:** warn when a named Firestore database forfeits the free tier ([f01a67b](https://github.com/lalu-uk/Propsy/commit/f01a67b6b2c74300a6ad1499e9f00a22fa5d3319))
* **config:** warn when a named Firestore database forfeits the free tier ([602d600](https://github.com/lalu-uk/Propsy/commit/602d6006631333e484fc86099ba248a697b2d94d))
* **deps:** update module cloud.google.com/go/firestore to v1.29.0 ([b00eb1d](https://github.com/lalu-uk/Propsy/commit/b00eb1d1a2fd1b8fb33234bce433beab218b245d))
* **deps:** update module cloud.google.com/go/firestore to v1.29.0 ([2303190](https://github.com/lalu-uk/Propsy/commit/2303190ae7a57e92ba166906d0e1f18e94145d11))
* **deps:** update module cloud.google.com/go/firestore to v1.29.1 ([8cbf18a](https://github.com/lalu-uk/Propsy/commit/8cbf18a3f0d6b9f8b4a39a47da3d3fa01aec94b9))
* **deps:** update module github.com/slack-go/slack to v0.31.0 ([cabba61](https://github.com/lalu-uk/Propsy/commit/cabba610a0f8e27989711bfce2a3bcef17cadfef))
* **deps:** update module github.com/slack-go/slack to v0.31.0 ([9cdc4b6](https://github.com/lalu-uk/Propsy/commit/9cdc4b6d919f9ea2278968e411787a08dd09ced2))
* **deps:** update module github.com/slack-go/slack to v0.32.0 ([23196be](https://github.com/lalu-uk/Propsy/commit/23196be55117e3ca89ecaaac89ad1c24cdc2fa03))
* **deps:** update module google.golang.org/api to v0.308.0 ([530ccce](https://github.com/lalu-uk/Propsy/commit/530ccce7a46635424838166f674412bb90332a95))
* **deps:** update module google.golang.org/api to v0.308.0 ([9057138](https://github.com/lalu-uk/Propsy/commit/9057138de9d2d9c2d7b09e63179b55c3abc9a4ff))
* **deps:** update module google.golang.org/api to v0.309.0 ([6089104](https://github.com/lalu-uk/Propsy/commit/60891049aace7db0f017295664426eb1d1bec7eb))
* **deps:** update module google.golang.org/api to v0.309.0 ([806cd77](https://github.com/lalu-uk/Propsy/commit/806cd772472bd7b31614751039badbcb94820cc7))
* **deps:** update module google.golang.org/grpc to v1.85.0 ([a6631c6](https://github.com/lalu-uk/Propsy/commit/a6631c69a2f338a783ba71a9fb7bd7106517e394))
* **deps:** update module google.golang.org/grpc to v1.85.0 ([05ae0a8](https://github.com/lalu-uk/Propsy/commit/05ae0a83f87a058c35d417e9b06da4008dfa1134))
* **gitignore:** anchor the root binary rule ([f2f8838](https://github.com/lalu-uk/Propsy/commit/f2f88389ad383bdf9ee36f1713b5c6014cd39964))
* **release:** correct docker image repository path to lalu-uk/Propsy ([7cc66a7](https://github.com/lalu-uk/Propsy/commit/7cc66a739c878a8a9f9cb6de0e0ea7e1b9f514c3))
* **release:** correct docker image repository path to lalu-uk/Propsy ([9133e6f](https://github.com/lalu-uk/Propsy/commit/9133e6f568ca25a86494ffd010a734290690d873))
* **release:** generate SBOMs for the release binaries ([daeb719](https://github.com/lalu-uk/Propsy/commit/daeb719c3934e5f333282e3b559df4d36b957748))
* **release:** lowercase docker image repository path ([fc75b04](https://github.com/lalu-uk/Propsy/commit/fc75b047a411c4986d5263e185a698ef0089e504))
* **release:** tag images with the exact version only ([1093265](https://github.com/lalu-uk/Propsy/commit/10932655cad77914141a69f58589d289228bc721))
* **release:** verify CI for tagged commit ([657c6ad](https://github.com/lalu-uk/Propsy/commit/657c6ada34bd1ed2d51f6025b826478ebff4926c))

## [2.1.0](https://github.com/lalu-uk/Propsy/compare/v2.0.0...v2.1.0) (2026-10-28)


### Features

* **slack:** open a give form when /propsy or /dropsy has no recipients ([f5a0a2d](https://github.com/lalu-uk/Propsy/commit/f5a0a2d716d5c2f3536d132db7cdfeca87f37be2))
* **slack:** open a give form when /propsy or /dropsy has no recipients ([f1c3fe2](https://github.com/lalu-uk/Propsy/commit/f1c3fe2ac82711f44fe32448e9c72f645b6771ef))


### Bug Fixes

* **deps:** update module github.com/slack-go/slack to v0.31.0 ([b4a1ab1](https://github.com/lalu-uk/Propsy/commit/b4a1ab18136750579f203d2535a716466a225b0e))
* **deps:** update module github.com/slack-go/slack to v0.31.0 ([f03a77c](https://github.com/lalu-uk/Propsy/commit/f03a77cdfef8a82323a277e2efdd82e072b74ad6))
* **deps:** update module google.golang.org/api to v0.309.0 ([4983c79](https://github.com/lalu-uk/Propsy/commit/4983c79f257b4a7aca53834b84f90cdbcdab8631))
* **deps:** update module google.golang.org/api to v0.309.0 ([c45e38c](https://github.com/lalu-uk/Propsy/commit/c45e38c3353aaccaf11602bb67677bc3d96d101e))
* **deps:** update module google.golang.org/grpc to v1.85.0 ([873837f](https://github.com/lalu-uk/Propsy/commit/873837ffeff12b225989c5c27bd45239d9bf6b2c))
* **deps:** update module google.golang.org/grpc to v1.85.0 ([a3d879d](https://github.com/lalu-uk/Propsy/commit/a3d879dbd7f0bc56391d346077547d635e12ab0d))

## [2.0.0](https://github.com/lalu-uk/Propsy/compare/v1.3.2...v2.0.0) (2026-10-26)


### ⚠ BREAKING CHANGES

* **release:** ship archives per platform, sign only checksums.txt

### Features

* **readme:** add logo and credit Flaticon attribution ([ce4db3e](https://github.com/lalu-uk/Propsy/commit/ce4db3e592521f60c2eb1292418de8b7febfd3b9))
* **readme:** add logo and credit Flaticon attribution ([733680d](https://github.com/lalu-uk/Propsy/commit/733680dd8a6c8b2aee97dc8a289276cb8e3b0e86))
* **release:** ship archives per platform, sign only checksums.txt ([ebc90fc](https://github.com/lalu-uk/Propsy/commit/ebc90fc62a15addd904983c6a432d8f5684654df))

## [1.3.2](https://github.com/lalu-uk/Propsy/compare/v1.3.1...v1.3.2) (2026-10-26)


### Bug Fixes

* **deps:** update module google.golang.org/api to v0.308.0 ([148335b](https://github.com/lalu-uk/Propsy/commit/148335b122371c893aab6635ddd9852a167f8717))
* **deps:** update module google.golang.org/api to v0.308.0 ([500645d](https://github.com/lalu-uk/Propsy/commit/500645dcc844f979343a1dd93d4ff8c9c5b3e0d8))

## [1.3.1](https://github.com/lalu-uk/Propsy/compare/v1.3.0...v1.3.1) (2026-10-25)


### Bug Fixes

* **deps:** update module cloud.google.com/go/firestore to v1.29.0 ([8356114](https://github.com/lalu-uk/Propsy/commit/83561144e0a9db7d6e4d785dfdec9b14eb6bfcc3))
* **deps:** update module cloud.google.com/go/firestore to v1.29.0 ([4142a5f](https://github.com/lalu-uk/Propsy/commit/4142a5fa588af107bf072574c7fe6207a7306414))

## [1.3.0](https://github.com/lalu-uk/Propsy/compare/v1.2.2...v1.3.0) (2026-10-24)


### Features

* **store:** keep local data in a JSON file instead of SQLite ([b10cbe6](https://github.com/lalu-uk/Propsy/commit/b10cbe63df1d1837ab2505bbb528e8bf6a150793))
* **store:** keep local data in a JSON file instead of SQLite ([9518960](https://github.com/lalu-uk/Propsy/commit/951896077e9bf467373b5b82add248ba4936994a))

## [1.2.2](https://github.com/lalu-uk/Propsy/compare/v1.2.1...v1.2.2) (2026-10-23)


### Bug Fixes

* **config:** warn when a named Firestore database forfeits the free tier ([1dbef6b](https://github.com/lalu-uk/Propsy/commit/1dbef6b8ef95e615e6ae8aa189ad1802d9a19eb9))
* **config:** warn when a named Firestore database forfeits the free tier ([2bafa58](https://github.com/lalu-uk/Propsy/commit/2bafa58821e76ed5af3913c72215002dc04192d4))

## [1.2.1](https://github.com/lalu-uk/Propsy/compare/v1.2.0...v1.2.1) (2026-10-23)


### Bug Fixes

* **release:** generate SBOMs for the release binaries ([97b6b60](https://github.com/lalu-uk/Propsy/commit/97b6b60f3baaae93e2eca73383fb7c2588a067f5))

## [1.2.0](https://github.com/lalu-uk/Propsy/compare/v1.1.2...v1.2.0) (2026-10-23)


### Features

* **leaderboard:** list everyone on received and given boards ([b065d43](https://github.com/lalu-uk/Propsy/commit/b065d43cf07fe8cd120fbe4037517256fa102307))
* leaderboards for everyone, immutable and signed releases ([34f6a09](https://github.com/lalu-uk/Propsy/commit/34f6a090442d826fb39a6b201f2a660b7d860a20))


### Bug Fixes

* **release:** tag images with the exact version only ([1864912](https://github.com/lalu-uk/Propsy/commit/1864912e50d27fc1665aa8b6ee1aab2fbcf88065))

## [1.1.2](https://github.com/lalu-uk/Propsy/compare/v1.1.1...v1.1.2) (2026-10-23)


### Bug Fixes

* **release:** lowercase docker image repository path ([8f28d5c](https://github.com/lalu-uk/Propsy/commit/8f28d5ce558fdaad419a5ae82e11a21733e73ff1))

## [1.1.1](https://github.com/lalu-uk/Propsy/compare/v1.1.0...v1.1.1) (2026-10-23)


### Bug Fixes

* **release:** correct docker image repository path to lalu-uk/Propsy ([98ba880](https://github.com/lalu-uk/Propsy/commit/98ba88058aa02c472a9331105bc584d1f0f44cc1))
* **release:** correct docker image repository path to lalu-uk/Propsy ([a235746](https://github.com/lalu-uk/Propsy/commit/a2357461c0d47668ca4ce9a14c759f6d06b74b49))

## [1.1.0](https://github.com/lalu-uk/Propsy/compare/v1.0.0...v1.1.0) (2026-10-23)


### Features

* add propsy entrypoint with socket and webhook modes ([31acabb](https://github.com/lalu-uk/Propsy/commit/31acabb8a6cc6e521f7edc8379a5a3826f5fa9bc))

## 1.0.0 (2026-10-22)


### Features

* add propsy and dropsy kudos bot with Slack and Firestore support ([c1a8234](https://github.com/lalu-uk/Propsy/commit/c1a823489436fb74cd4943a0b3db2baf697b9065))
* add propsy and dropsy kudos bot with Slack and Firestore support ([99bc789](https://github.com/lalu-uk/Propsy/commit/99bc789ce68cb606a41e2d8fb33a064affc5f475))
