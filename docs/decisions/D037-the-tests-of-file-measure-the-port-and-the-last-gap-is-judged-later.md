# D37. The tests of file measure the port, and the last gap is judged later

Status: Decided.

The maintainers decided on 2026-11-15 to defer what "100% coverage" means.

The aim is to pass the great majority of the tests of file, the 93 expected
results of 5.48 in `tests/`, with the output of the reference (D33). When the
port is close, the maintainers judge the tests that still fail, one at a time,
and decide whether each is worth the cost. How close to file the port can get
is learned on the way, not set in advance.

So the gate of a phase is a count of the tests of file that pass, and a list of
the ones that fail, with the reason for each. The count goes up and never
down. The coverage of statements is reported, and it is not a gate.

The differential test against the oracle and the corpus of D30 still show the
gap on other files. They do not hold back a release until the maintainers set
a gate for them.
