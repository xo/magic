# Contributing

This document is for a person who changes `magic`. `AGENTS.md` holds the same
rules for a coding agent, in more detail. `docs/decisions/` holds every
decision and the reason for it, and `docs/PLAN.md` holds the plan of the
port.

## Build and test

The package is pure Go. Run these commands in the repository root:

```bash
go build ./...
go vet ./...
go test -race -count=1 ./...
gofmt -l .
```

`-race` needs cgo. Without a C compiler, run the tests with
`CGO_ENABLED=0 go test ./...`. `gofmt -l .` prints nothing when every file is
formatted. CI runs the build, `go vet` and the tests on Linux, macOS and
Windows, and builds the package for the targets that have no runner.

If `file` 5.48 is on your `PATH`, `TestOracle` compares the MIME types of the
package with it.
Otherwise the test skips.

## The harness

The test module in `test/` compares the package with libmagic, built from the
source of file (D30). It reads the checkout of file in `upstream/file/`,
which git ignores. Make it with:

```bash
git clone https://github.com/file/file.git upstream/file
```

The oracle needs the autotools, a C compiler, and the headers of zlib, bzip2,
xz and zstd. Build it, then run the test module:

```bash
test/oracle/build.sh
```

```bash
cd test && go test ./...
```

The test module holds the gates of the port: `TestPortMatchesTheOracle`,
`TestAPIMatchesTheOracle`, `TestPortPassesTheTestsOfFile` and `TestPortSize`
(D38). `docs/PLAN.md` says what each one proves.

## The rules

The rules of file are compiled into `internal/table/rules.flate` (D35). After
a change of the compiler, or of the commit of file that the port follows,
write the table again:

```bash
cd internal/table && go generate
```

`TestTableMatchesTheCompiler` fails until the table is current, and CI checks
that `go generate` changes nothing.

## Release

Tag only when a maintainer says so. The root module has a plain semver tag.
`compress/xz` and `compress/zstd` are modules of their own, tagged
`compress/xz/vX.Y.Z` and `compress/zstd/vX.Y.Z`.

1. Push the commit to `main`, and wait for CI.
2. Tag the root, such as `git tag v0.5.0`, and push the tag.
3. In `compress/xz/go.mod` and `compress/zstd/go.mod`, require the new
   version of the root. Push that commit.
4. Tag the two modules with the same version, and push the tags.

Never move a tag that is published, because the module proxy caches it. The
tags of the target modules of the cgo package, such as
`libmagic/linux_amd64/v5.48.2`, stay published (D47).

## Documents

The root holds four documents: `README.md`, `AGENTS.md`, `CLAUDE.md` and this
one. Every other document goes in `docs/`. A decision is a file of its own in
`docs/decisions/`, with a row in the index `docs/decisions/README.md` (D23).
The tests in `docs_test.go` fail if the index and the files disagree. The
documents name no maintainer (D32).

## The port

`internal/rules` compiles the rules, `internal/table` embeds them,
`internal/engine` runs them with the detectors of libmagic, and `compress/`
holds the decoders of compressed data. `docs/UPSTREAM.md` holds the rules for
following file. A Go file that translates a file of libmagic opens with the
notice of that file (D29).

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
`docs/decisions/`.
