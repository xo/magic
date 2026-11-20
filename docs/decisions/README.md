# Decisions

Every decision of this project is a file in this folder, named by its number
and its title (D23). This table is the index. Find the number here, then open
the file.

Each file opens with its status. `Decided` means that the maintainers chose
it. `Proposed` means that it waits for the maintainers. A decision that
changes an earlier one says so in its status, as `Amends D3`, and the earlier
one says it back, as `Amended by D20`. Read the status before the decision.

A bare number, such as D3, names a decision in this folder. A decision of
another repository names that repository, such as dbmeta D110 or okage D12.

A new decision gets the next number and a file of its own. Add its row here.
An open question is not a decision. It goes at the end of
[`../PLAN.md`](../PLAN.md) until the maintainers answer it.

D1 to D21 were written in `docs/PLAN.md` and moved here on 2026-11-15. D22 to
D32 set the project up as a port of libmagic.

| # | Decision | Status |
| --- | --- | --- |
| D1 | [The package links static libmagic archives that the repository carries](D001-the-package-links-static-libmagic-archives-that-the-repository-carries.md) | Decided. Amended by D16. Amended by D17. Amended by D19. Amended by D22. Amended by D47 |
| D2 | [build-libmagic.sh cross-compiles the archives with zig cc](D002-build-libmagic-sh-cross-compiles-the-archives-with-zig-cc.md) | Decided. Amended by D16. Amended by D17. Amended by D22. Amended by D47 |
| D3 | [The magic database is embedded, and one copy serves every target](D003-the-magic-database-is-embedded-and-one-copy-serves-every-target.md) | Decided. Amended by D20. Amended by D21. Amended by D47 |
| D4 | [libmagic reads compressed files through zlib, bzip2, xz and zstd](D004-libmagic-reads-compressed-files-through-zlib-bzip2-xz-and-zstd.md) | Decided. Amended by D43 |
| D5 | [The API follows the libmagic C API](D005-the-api-follows-the-libmagic-c-api.md) | Decided. Amended by D39. Amended by D47 |
| D6 | [A handle is not safe for concurrent use](D006-a-handle-is-not-safe-for-concurrent-use.md) | Decided. Amended by D9 |
| D7 | [A closed handle reports an error and never reaches C](D007-a-closed-handle-reports-an-error-and-never-reaches-c.md) | Decided. Amended by D47 |
| D8 | [The code is under the MIT license](D008-the-code-is-under-the-mit-license.md) | Decided |
| D9 | [A handle is safe to share across goroutines](D009-a-handle-is-safe-to-share-across-goroutines.md) | Decided. Amends D6. Amended by D47 |
| D10 | [A lookup can use other flags and leave the flags of the handle alone](D010-a-lookup-can-use-other-flags-and-leave-the-flags-of-the-handle-alone.md) | Decided. Amended by D47 |
| D11 | [The tests expect what libmagic 5.48 reports, and file(1) is the reference](D011-the-tests-expect-what-libmagic-5-48-reports-and-file-1-is-the-reference.md) | Decided. Amended by D48 |
| D12 | [CI builds and tests only on hosted runners](D012-ci-builds-and-tests-only-on-hosted-runners.md) | Decided. Amended by D16. Amended by D47. Amended by D53. Amended by D54. Amended by D56 |
| D13 | [The script builds against the newest stable release of each dependency](D013-the-script-builds-against-the-newest-stable-release-of-each-dependency.md) | Decided. Amended by D47 |
| D14 | [The module has no Go dependencies](D014-the-module-has-no-go-dependencies.md) | Decided. Amended by D28 |
| D15 | [magic is set up for coding agents as every xo repository is](D015-magic-is-set-up-for-coding-agents-as-every-xo-repository-is.md) | Decided. Amended by D23 |
| D16 | [windows/arm64 is a target](D016-windows-arm64-is-a-target.md) | Decided. Amends D1. Amends D2. Amends D12. Amended by D17. Amended by D47 |
| D17 | [Each target is a Go module of its own, tagged as resvg tags its modules](D017-each-target-is-a-go-module-of-its-own-tagged-as-resvg-tags-its-modules.md) | Decided. Amends D1. Amends D2. Amends D16. Amended by D47 |
| D18 | [A target version has a patch number, and a target module declares go 1.21](D018-a-target-version-has-a-patch-number-and-a-target-module-declares-go-1-21.md) | Decided. Amended by D41. Amended by D47 |
| D19 | [magic builds the targets that resvg builds](D019-magic-builds-the-targets-that-resvg-builds.md) | Decided. Amends D1. Amended by D47 |
| D20 | [The embedded database is compressed with zstd, and expands into C memory](D020-the-embedded-database-is-compressed-with-zstd-and-expands-into-c-memory.md) | Decided. Amends D3. Amended by D47 |
| D21 | [MagicDB is a function that expands the database](D021-magicdb-is-a-function-that-expands-the-database.md) | Decided. Amends D3. Amended by D38. Amended by D47 |
| D22 | [magic becomes a pure Go port of libmagic](D022-magic-becomes-a-pure-go-port-of-libmagic.md) | Decided. Amends D1. Amends D2. Amended by D34 |
| D23 | [Each decision is a file of its own](D023-each-decision-is-a-file-of-its-own.md) | Decided. Amends D15 |
| D24 | [The plan and the rules come before any code](D024-the-plan-and-the-rules-come-before-any-code.md) | Decided |
| D25 | [The upstream checkout is in `upstream/`, and git ignores it](D025-the-upstream-checkout-is-in-upstream-and-git-ignores-it.md) | Decided |
| D26 | [The upstream has a base commit and a ledger](D026-the-upstream-has-a-base-commit-and-a-ledger.md) | Decided. Amended by D34 |
| D27 | [Each part of file has a policy: port, replace or none](D027-each-part-of-file-has-a-policy-port-replace-or-none.md) | Decided. Amended by D36. Amended by D35 |
| D28 | [xz and zstd come from other repositories when they are needed](D028-xz-and-zstd-come-from-other-repositories-when-they-are-needed.md) | Decided. Amends D14. Amended by D45 |
| D29 | [The port keeps the license of file](D029-the-port-keeps-the-license-of-file.md) | Decided |
| D30 | [The test module holds the oracle](D030-the-test-module-holds-the-oracle.md) | Decided. Amended by D33. Amended by D58 |
| D31 | [The binary walker comes after the port](D031-the-binary-walker-comes-after-the-port.md) | Decided |
| D32 | [The documents name no maintainer](D032-the-documents-name-no-maintainer.md) | Decided |
| D33 | [The reference is libmagic 5.48 built against glibc](D033-the-reference-is-libmagic-5-48-built-against-glibc.md) | Decided. Amends D30. Amended by D48 |
| D34 | [The port copies the behavior of libmagic, not its code](D034-the-port-copies-the-behavior-of-libmagic-not-its-code.md) | Decided. Amends D22. Amends D26 |
| D35 | [The rules are compiled into tables when the package is built](D035-the-rules-are-compiled-into-tables-when-the-package-is-built.md) | Decided. Amends D27 |
| D36 | [magic runs no outside program](D036-magic-runs-no-outside-program.md) | Decided. Amends D27 |
| D37 | [The tests of file measure the port, and the last gap is judged later](D037-the-tests-of-file-measure-the-port-and-the-last-gap-is-judged-later.md) | Decided |
| D38 | [The port adds no more to a program than the cgo package](D038-the-port-adds-no-more-to-a-program-than-the-cgo-package.md) | Decided. Amends D21. Amended by D45 |
| D39 | [The switch to the port is a minor version, and breaks what it needs to](D039-the-switch-to-the-port-is-a-minor-version-and-breaks-what-it-needs-to.md) | Decided. Amends D5 |
| D40 | [The API is new, and `magic.Detect` is its entry](D040-the-api-is-new-and-magic-detect-is-its-entry.md) | Decided. Amended by D42 |
| D41 | [The module requires Go 1.27](D041-the-module-requires-go-1-27.md) | Decided. Amends D18 |
| D42 | [The API takes one walk, no Detector, a context, a generic Source and a slice of matches](D042-the-api-takes-one-walk-no-detector-a-context-a-generic-source-and-a-slice-of-matches.md) | Decided. Amends D40. Amended by D46 |
| D43 | [The Linux archives carry their own regex engine](D043-the-linux-archives-carry-their-own-regex-engine.md) | Decided. Amends D4. Amended by D47 |
| D44 | [The port translates each regular expression for Go](D044-the-port-translates-each-regular-expression-for-go.md) | Decided |
| D45 | [A program chooses its decoders with a blank import](D045-a-program-chooses-its-decoders-with-a-blank-import.md) | Decided. Amends D28. Amends D38 |
| D46 | [The port runs one walk for each form of a result](D046-the-port-runs-one-walk-for-each-form-of-a-result.md) | Decided. Amends D42 |
| D47 | [The switch removes the cgo package and its archives](D047-the-switch-removes-the-cgo-package-and-its-archives.md) | Decided. Amends D1. Amends D2. Amends D3. Amends D5. Amends D7. Amends D9. Amends D10. Amends D12. Amends D13. Amends D16. Amends D17. Amends D18. Amends D19. Amends D20. Amends D21. Amends D43 |
| D48 | [The port follows the commit of the ledger, and its bugs](D048-the-port-follows-the-commit-of-the-ledger-and-its-bugs.md) | Decided. Amends D11. Amends D33. Amended by D58 |
| D49 | [The walker follows binwalk 3, and its names](D049-the-walker-follows-binwalk-3-and-its-names.md) | Decided. Amended by D58. Amended by D59. Amended by D60 |
| D50 | [The walker is the package walk, and returns an iterator](D050-the-walker-is-the-package-walk-and-returns-an-iterator.md) | Decided |
| D51 | [The walker carries the keys of binwalk](D051-the-walker-carries-the-keys-of-binwalk.md) | Decided |
| D52 | [The walker is built in five phases](D052-the-walker-is-built-in-five-phases.md) | Decided |
| D53 | [CI runs on the current macOS runners](D053-ci-runs-on-the-current-macos-runners.md) | Decided. Amends D12 |
| D54 | [CI checks the format with gofmt](D054-ci-checks-the-format-with-gofmt.md) | Decided. Amends D12 |
| D55 | [The test data of mimetype stays as it is](D055-the-test-data-of-mimetype-stays-as-it-is.md) | Decided |
| D56 | [CI runs golangci-lint](D056-ci-runs-golangci-lint.md) | Decided. Amends D12 |
| D57 | [CI passes before a tag](D057-ci-passes-before-a-tag.md) | Decided |
| D58 | [CI runs no oracle](D058-ci-runs-no-oracle.md) | Decided. Amends D30. Amends D48. Amends D49 |
| D59 | [The walker fixes a bug of binwalk with a decision, and so does the oracle](D059-the-walker-fixes-a-bug-of-binwalk-with-a-decision-and-so-does-the-oracle.md) | Decided. Amends D49. Amended by D60 |
| D60 | [The walker follows binwalk-ng](D060-the-walker-follows-binwalk-ng.md) | Decided. Amends D49. Amends D59. Amended by D68 |
| D61 | [The decoders for ivy are packages of compress that do not register](D061-the-decoders-for-ivy-are-packages-of-compress-that-do-not-register.md) | Decided |
| D62 | [The reader of Compound Files for ivy is the package cfb](D062-the-reader-of-compound-files-for-ivy-is-the-package-cfb.md) | Decided |
| D63 | [The decoders of LZFSE and ADC for ivy follow D61](D063-the-decoders-of-lzfse-and-adc-for-ivy-follow-d61.md) | Decided |
| D64 | [A package for ivy names the formats that libmagic names as their container](D064-a-package-for-ivy-names-the-formats-that-libmagic-names-as-their-container.md) | Decided |
| D65 | [The package container names the format inside a container](D065-the-package-container-names-the-format-inside-a-container.md) | Decided |
| D66 | [The engine keeps the error text of libmagic](D066-the-engine-keeps-the-error-text-of-libmagic.md) | Decided |
| D67 | [Rules beyond libmagic are the option WithExtraRules](D067-rules-beyond-libmagic-are-the-option-withextrarules.md) | Proposed |
| D68 | [The walker uses the decoders of compress](D068-the-walker-uses-the-decoders-of-compress.md) | Decided. Amends D60 |
