# D58. CI runs no oracle

Status: Decided. Amends D30. Amends D48. Amends D49.

The maintainers decided on 2026-11-15 that CI does not run the oracles.

CI builds and tests the package on each runner, builds it for the targets
that have no runner, and checks the format and the linters (D12, D47, D54,
D56). It no longer builds libmagic or binwalk, and does not run the test
module, the check that the table of the rules is current, or the test of the
ledger against upstream. A person who changes the port or the walker runs
them, as `CONTRIBUTING.md` says:

```bash
test/oracle/build.sh
test/oracle/build-binwalk.sh
cd test && go test ./...
```

The build of binwalk with cargo took more than 17 minutes on a runner, and
the oracle job was the slowest job of each run.
