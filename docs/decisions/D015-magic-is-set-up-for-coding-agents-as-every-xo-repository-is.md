# D15. magic is set up for coding agents as every xo repository is

Status: Decided. Amended by D23.

The maintainers decided on 2026-09-27 that every repository in the `xo`
namespace is set up for coding agents the same way. dbmeta records it as dbmeta
D110. This decision adopts it for `magic`.

The repository now has these files:

- `AGENTS.md` holds the rules for a coding agent, and opens with the three
  standing rules of dbmeta D110.
- `CLAUDE.md` holds one line, `@AGENTS.md`, so that Claude Code reads the
  same rules. It is a file and not a symbolic link.
- `README.md` and `CONTRIBUTING.md` are new. `CONTRIBUTING.md` has the
  commands that install the agent skills.
- `skills-lock.json` names the two skills, `simple-english` and
  `go-pedantry`. Each is an ordinary folder in `.agents/skills/` and in
  `.claude/skills/`, installed with `--copy`.
- `.gitignore` ignores `.claude/settings.local.json`, the Claude Code
  permissions of one person.
- `.gitattributes` holds `* text=auto eol=lf`.
- `docs/PLAN.md` is this document, and `docs/BACKLOG.md` holds the known
  work.

`magic` is a small library, so its decisions stay in this document and are
not split into one file each (dbmeta D111).

The tests in `skills_test.go` and `docs_test.go` hold the setup:
`TestSkillsAreCopies`, `TestClaudeImportsAgents`,
`TestTheRootHoldsFourDocuments`, `TestTheDecisionIndexIsComplete`,
`TestAnAmendmentPointsBothWays` and `TestEveryDecisionReferenceExists`. They
use only the standard library.
