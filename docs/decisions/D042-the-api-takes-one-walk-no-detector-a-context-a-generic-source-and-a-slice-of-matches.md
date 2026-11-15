# D42. The API takes one walk, no Detector, a context, a generic Source and a slice of matches

Status: Decided. Amends D40. Amended by D46.

The maintainers decided on 2026-11-15 to take the recommendations of
[`../API.md`](../API.md) on its five open questions, after the reviews of
OpenAI Pro and Gemini Pro:

1. One walk. `Detect` walks the rules once and records, in the order of the
   walk, the text of each match and the first annotation of each kind: MIME
   type, extensions and Apple code. That is what each output flag of
   libmagic prints. The differential test (D30) proves it for each output.
   Where it fails, that output gets a walk of its own.
2. No `Detector` type. The options are given to each call (D40), and the
   tables do not change (D35). A `sync.Pool` inside the package reuses the
   buffers of `DetectReader`, `DetectReaderAt` and `DetectFile`.
3. A `context.Context` on every entry, `Detect` of bytes included, so that each
   entry has the same form, and a caller can stop a long detection of hostile
   bytes. The port checks it between the top-level rules and between reads.
4. The generic `Source`, with the type set `~[]byte | ~string`, so that a named
   type such as `json.RawMessage` works. Only the entry is generic, and the
   engine is one function. Its cost in bytes is measured against D38 when the
   first code exists. If it costs too much, `Detect` takes `[]byte`.
5. `Matches` is a slice, with one entry for each line that `WithAll` prints,
   which is a top-level rule and its continuations.

D40 said that the details of `docs/API.md` were a proposal. With this decision
the design is settled, until the first code shows a reason to change it.
