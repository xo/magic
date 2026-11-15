# D50. The walker is the package walk, and returns an iterator

Status: Decided.

The maintainers decided on 2026-11-15 that the binary walker is the package
`github.com/xo/magic/walk` of the root module, with the API of
`docs/WALKER.md`. This answers questions 18 and 20 of `docs/PLAN.md`.

1. `walk.Walk` and `walk.WalkFile` take a `context.Context` first and return
   an `iter.Seq2[*Entry, error]`. The entries come depth first, and each one
   points to its parent.
2. `Entry.SkipMembers` skips the members of one match, as `fs.SkipDir` does
   for `fs.WalkDir`. The API has no function that calls back.
3. `Entry.Members` gives the members of a match, and `walk.Extract` writes
   them only through an `os.Root`, inside the limits of `docs/WALKER.md`.
4. The formats are packages under `walk/format/` that register themselves,
   as the decoders of D45 do. A program imports the formats that it needs,
   and `walk/format/all` imports every one. A format that needs another
   module is a module of its own.
