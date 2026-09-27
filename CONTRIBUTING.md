# Contributing

This document is for a person who changes `magic`. `AGENTS.md` holds the same
rules for a coding agent, in more detail. `docs/PLAN.md` holds every decision
and the reason for it.

## Build and test

The package uses cgo, so you need a C compiler. Run these commands in the
repository root:

```bash
go build ./...
go vet ./...
go test -race -count=1 ./...
gofmt -l .
```

`gofmt -l .` prints nothing when every file is formatted. CI runs the build,
`go vet` and the tests on Linux, macOS and Windows.

If `file` 5.48 is on your `PATH`, `TestOracle` compares the package with it.
Otherwise the test skips.

## Rebuild the archives

`build-libmagic.sh` builds libmagic and its compression libraries for all six
targets with `zig cc`. It needs zig 0.13 or later, curl, tar, make, git and a
native C compiler. The header of the script gives every option.

```bash
./build-libmagic.sh
```

By default the script builds against the newest stable release of each
dependency. `PINNED=1` builds against the fallback versions in the script.

A new release of file changes `libmagic/magic.mgc` and the text that libmagic
reports. After you rebuild, run the tests and update the expected values that
changed. D11 in `docs/PLAN.md` says why the tests expect exact values.

The script writes its downloads and build folders to `src/`, `.build/`,
`.native/`, `.toolchains/` and `.zig-cache/`. Git ignores all five.

## Documents

The root holds four documents: `README.md`, `AGENTS.md`, `CLAUDE.md` and this
one. Every other document goes in `docs/`. A decision goes at the end of the
decisions in `docs/PLAN.md`, with a row in the index at the top of them. The
tests in `docs_test.go` fail if the index and the headings disagree.

## Agent skills

The repository carries two agent skills. A skill is a set of instructions
that a coding agent loads for a task. `simple-english` sets how text is
written, and `go-pedantry` sets how Go is written.

`skills-lock.json` names the source of each skill. The `skills` command from
npm writes that file, and version 1.7.0 is the one used here. It writes each
skill into two folders. Codex and the other agents read
`.agents/skills/<name>`, and Claude Code reads `.claude/skills/<name>`.

To install the skills again or to update them, run these commands in the
repository root:

```bash
npx skills@1.7.0 add AminBlg/SimpleEnglish --skill simple-english --agent codex claude-code --copy -y
```

```bash
npx skills@1.7.0 add oborchers/fractional-cto --skill go-pedantry --agent codex claude-code --copy -y
```

Keep `--copy`. Without it, the command writes `.claude/skills/<name>` as a
symbolic link. A Windows checkout writes a symbolic link as a small text
file, and Claude Code then loads no skill and reports nothing.
`TestSkillsAreCopies` fails on a link, on a missing copy, on two copies that
differ and on a skill that `skills-lock.json` does not name. See D15 in
`docs/PLAN.md`.
