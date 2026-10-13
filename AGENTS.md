# magic

`magic` is a Go package that wraps libmagic, the library of the `file`
command. It identifies the type of a file or a buffer. The repository carries
libmagic as static archives for eight targets, and it embeds the magic database
in the package. A program that imports `magic` needs Go and a C compiler, and
nothing else.

## Standing rules

These rules hold in every `xo` repository, for every coding agent (dbmeta
D110, which D15 adopts).

1. Stage changes for review. Commit and push only when Ken says so.
2. Load the `simple-english` skill before you write any text that a person
   reads: project documentation, a code comment, an error message or a commit
   message.
3. In a Go project, load the `go-pedantry` skill before you write or review Go
   code. A rule in this file wins where the two conflict.

`CLAUDE.md` holds one line that imports this file, so that Claude Code and
every other agent read the same rules. Edit this file, not `CLAUDE.md`.

## Which document to read

`docs/PLAN.md` holds the plan, every decision and the open questions. Read
the status of a decision, because D6 is amended by D9 and D14 is only
proposed. Do not decide an open question on your own. The open questions are
at the end of `docs/PLAN.md`. Ask Ken.

| If you are | Read |
| --- | --- |
| asking why something is the way it is | the index of decisions in `docs/PLAN.md` |
| changing `magic.go` | D3, D7, D9 and D10 in `docs/PLAN.md`, then Go conventions below |
| changing an expected value in a test | D11 in `docs/PLAN.md` |
| rebuilding the archives or changing a dependency version | D1, D2 and D13 in `docs/PLAN.md`, the header of `build-libmagic.sh`, then `CONTRIBUTING.md` |
| adding a platform | D17 and D19 in `docs/PLAN.md`. Ken names the platforms, and resvg is the reference. A platform needs an entry in `TARGETS` in `build-libmagic.sh`, then `./build-libmagic.sh <target>`, which writes its module files and its `link_<target>.go`. Add its `require` and `replace` lines to `go.mod`. `TestEveryTargetIsWired` fails until you do. Add a runner to `.github/workflows/test.yml` if GitHub has one |
| tagging a release | D17 and D18 in `docs/PLAN.md`, then `CONTRIBUTING.md`, under Release. Tag only when Ken says so |
| changing CI | D12 in `docs/PLAN.md`, then `.github/workflows/test.yml` |
| looking for work that is known and not done | `docs/BACKLOG.md` |
| writing a document, a code comment, an error message or a commit message | the `simple-english` skill. Load it first |
| writing or reviewing Go code | the `go-pedantry` skill, then Go conventions below |
| adding or updating an agent skill | `CONTRIBUTING.md`, under Agent skills, and D15 |

`README.md` is for a person who uses the package. `CONTRIBUTING.md` is for a
person who changes it.

## How to name a decision

A bare number, such as D3, names a decision of this repository, in
`docs/PLAN.md`. A decision of another repository is named with that
repository, such as dbmeta D110. Write the name of the repository before each
number: "dbmeta D110 and dbmeta D111". `TestEveryDecisionReferenceExists`
fails on a bare number that `docs/PLAN.md` does not hold.

A new decision goes at the end of the decisions in `docs/PLAN.md`, with a row
in the index. Its heading is `### D<n>. <title>. <status>.`, and the status is
`Decided` or `Proposed`. If it changes an earlier decision, its heading also
says `Amends Dn`, and the heading of the earlier one says `Amended by D<n>`.
`TestTheDecisionIndexIsComplete` and `TestAnAmendmentPointsBothWays` hold this.

## Layout

| Path | What it holds |
| --- | --- |
| `magic.go` | the whole package: the cgo preamble, the `Magic` type, the flags and the embedded database |
| `magic_test.go` | the tests of the package, in package `magic` |
| `skills_test.go`, `docs_test.go`, `targets_test.go` | the tests of the agent setup, the documents and the target layout, in package `magic_test` |
| `go.mod` | the root module. It requires each target module and has a `replace` line for each (D17) |
| `link_<goos>_<goarch>.go` | one generated file for each target. It imports the module of its platform. Do not edit it |
| `cmd/file/` | an example program that prints the type of each file that it gets |
| `libmagic/magic.h`, `libmagic/magic.mgc` | the header and the compiled database, shared by all targets |
| `libmagic/<goos>_<goarch>/` | one target. It is a Go module that holds the static archives, a `go.mod` and a generated `link.go`. `linux_arm` is armv7 |
| `build-libmagic.sh` | the script that builds everything in `libmagic/` |
| `testdata/` | the sample files that the tests read |
| `.github/workflows/test.yml` | CI |
| `docs/` | the plan, the decisions and the backlog |

`build-libmagic.sh` writes its downloads and build folders to `src/`,
`.build/`, `.native/`, `.toolchains/` and `.zig-cache/`. `.gitignore` ignores
all five. They can hold hundreds of megabytes.

## Commands

The package uses cgo, so every command needs a C compiler.

```bash
go build ./...
go vet ./...
go test -race -count=1 ./...
gofmt -l .
go run ./cmd/file testdata/gif.gif testdata/utf8.txt testdata/xlsx.xlsx
./build-libmagic.sh modules
```

`./build-libmagic.sh modules` rewrites the generated module files and builds
nothing.

`gofmt -l .` prints nothing when every file is formatted. CI runs the other
four commands on every platform (D12). The repository has no `.golangci.yml`,
and CI runs no linter.

`TestOracle` runs only when `file` 5.48 is on `PATH`. Otherwise it skips.

`build-libmagic.sh` rebuilds the archives. It needs zig, curl, git, make and
the network. It changes the committed archives, and a new release of file
changes the output that the tests expect (D11, D13).

## Go conventions

The code follows these conventions today:

- Each exported method of `Magic` holds `m.mu` for the whole call, then calls
  an unexported method that does the work. The comment of that method says
  "Caller must hold m.mu" (D9).
- Each method that uses the cookie first returns `errClosed` when
  `m.cookie` is nil (D7).
- C keeps only C memory. Go memory goes to C only for the length of one call,
  and `runtime.KeepAlive` follows the call (D3).
- A string from libmagic is copied with `C.GoString` at once, because the
  cookie owns it only until the next call.
- A C helper in the cgo preamble takes the place of pointer arrays on the Go
  side, as `magic_load_one` does.
- An error that the package makes starts with `magic: `. An error from
  libmagic is the name of the C function, a colon and the text of
  `magic_error`. `New` is the one exception: it returns `magic_open failed`.
- The flags are untyped constants that take their values from the `MAGIC_*`
  constants of `magic.h` (D5).
- A file that starts with "Code generated by build-libmagic.sh" is output of
  the script. Change the script, and do not edit the file.
- The receiver of `Magic` is `m`.
- A comment gives the reason for the code, often the behavior of libmagic or
  of cgo that the code works around.
- Each test calls `t.Parallel`, except `TestVersion`. A failure message has
  the form `expected no error, got: %v` or `expected %q, got: %q`.
- An expected value in a test is what libmagic 5.48 reports (D11).
- `go.mod` has no `require` line (D14, proposed).

The code differs from `go-pedantry` in two places. It makes its errors with
`fmt.Errorf` and exports no sentinel error, and its error text is not in the
gerund form. Ken did not decide yet which one wins. `docs/BACKLOG.md` holds
the item.
