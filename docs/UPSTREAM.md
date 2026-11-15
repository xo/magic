# Following upstream

This document holds the rules for porting libmagic to magic, and for porting
each change that upstream makes after that. The rules were written on
2026-11-15, before any code of the port existed (D24). Each rule names the
decision that it comes from.

## The upstream

`docs/upstream/upstreams.tsv` lists the upstream repository (D26). It has
these columns, separated by a tab:

| Column | What it holds |
| --- | --- |
| `repository` | the short name, which is also the name of its ledger and of its checkout |
| `url` | the URL to clone |
| `checkout` | the folder of the checkout (D25) |
| `ports` | what magic ports from it |
| `magic` | the magic packages that hold the port |
| `policy` | `port` (D27) |
| `base` | the full hash of the base commit |
| `matched` | the full hash of the last commit that magic matches, or empty before the base port is done |

There is one upstream, file. The compression libraries are not upstreams of
the port, because magic replaces them (D27).

## The checkout

The upstream has a checkout in `upstream/file/`, in the root of this
repository, and git ignores the folder `upstream/` (D25). The checkout is a
reference. Do not edit a file in it, and do not commit it.

If the checkout is missing, make it in the root of the repository:

```bash
git clone https://github.com/file/file.git upstream/file
```

Before you port anything, make sure that the checkout is clean. This command
must print nothing:

```bash
git -C upstream/file status --short
```

Upstream holds no Go file on 2026-11-15. If it adds one, `go build ./...`
finds it. After each fetch, run this command. It must print nothing:

```bash
find upstream -name '*.go'
```

## The base commit

The base commit is the tag `FILE5_48`, commit
`eb754ace19fed5481d8142426543100a2d6bae4e` (D26). It is the release that the
cgo package wraps. The base port is made from this commit, and the column
`matched` stays empty until it is done.

## Where each upstream path goes

Each upstream path has one rule. A commit takes the strongest rule of the
paths that it changes: Port, then Review, then Not applicable.

| Upstream path | magic path | Rule |
| --- | --- | --- |
| `src/*.c`, `src/*.h`, except the ones below | the root package and `internal/`, as the plan maps them | Port |
| `magic/Magdir/*` | the rule tables, compiled when the package is built (D35) | Port |
| `tests/*.testfile`, `tests/*.result`, `tests/*.magic`, `tests/*.flags` | `testdata/upstream/` | Port |
| `src/file.c`, `src/file_opts.h` | none. `cmd/file` is an example, not a port (D27) | Review: a change to the meaning of a flag can change the library |
| `src/seccomp.c` | none. The port runs no system calls that it limits | Not applicable |
| `configure.ac`, `Makefile.am`, `src/Makefile.am` | none | Review: a new source file or a new feature test can change what the library does |
| `ChangeLog`, `README.md`, `doc/`, `*.readme`, `contrib/`, `.github/`, the Amiga scripts | none | Not applicable |
| any other path | none | Review |

A Review commit is read, and becomes `ported` or `not-applicable` in the
ledger with a note that says why.

## The ledger

`docs/upstream/file.tsv` has one line for each upstream commit after the base
commit, in the order of upstream, with these columns:

| Column | What it holds |
| --- | --- |
| `upstream` | the full hash of the upstream commit |
| `date` | the date of the commit, as `git log --format=%cs` prints it |
| `status` | `ported`, `not-applicable` or `pending` |
| `magic` | the short hash of the magic commit that ports it, or empty |
| `note` | the subject of the commit, and what it changes |

`TestTheLedgerIsComplete` fails on a line that is out of order, on a gap, and
on a status that is not one of the three. It reads the checkout when it exists,
and skips that part when it does not.

To add the commits that arrived since the last time:

1. Fetch the checkout: `git -C upstream/file fetch`.
2. Add a line for each commit after the last line of the ledger, with the rule
   of the table above.
3. Run `go test -run TestTheLedgerIsComplete .`.

## Porting a commit

After the base port, port the `pending` commits in the order of the ledger,
one magic commit for each upstream commit (D26):

1. Read the diff: `git -C upstream/file show <hash>`.
2. Find how the commit changes what libmagic does, and change the Go code to
   do the same. The Go code is not a copy of the C code (D34), so the change
   can look different.
3. If the commit changes `tests/`, port the test data, and run the tests.
4. If the commit changes the rules, rebuild the rule tables, and run the
   differential test (D30).
5. Set the status to `ported` and the column `magic` to the new commit.
6. Move `matched` in `upstreams.tsv` to this commit, if no earlier commit is
   still `pending`.

The commit message of magic names the upstream commit: its short hash and its
subject.
