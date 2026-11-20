# D66. The engine keeps the error text of libmagic

Status: Decided.

The `go-pedantry` skill asks that the context of an error starts with a verb
in the gerund form, such as "reading the rules". An error of the engine has
the text that libmagic prints, such as "cannot read `x' (No such file or
directory)", which is not in that form. The maintainers decided on
2026-11-20 that the text of libmagic wins.

## The decision

1. An error of the engine that libmagic also gives has the text of libmagic,
   word for word, after the prefix `magic: `. The engine does not change the
   text to the gerund form.
2. The same rule holds for the decoders of compressed data that register
   with `compress`, such as `compress/zlib` and `compress/bzip2`. The text of
   each error is the text that libmagic prints for its library (D45).
3. An error that magic makes itself, in the root package or in the engine,
   and that libmagic does not give, starts with `magic: ` and follows
   `go-pedantry`. An example is a pattern that Go cannot compile after the
   engine translates it (D44). When the error wraps another error, its
   context is in the gerund form, such as `magic: loading the rules: %w`.
   Otherwise it states the fault, such as
   `magic: the size -1 is negative`. A detection that stops for its context
   wraps `ErrStopped` and the error of the context.
4. The packages for ivy, such as `compress/lzx`, `cfb` and `container`, are
   not ports of libmagic. Their errors start with the name of the package
   (D61) and follow `go-pedantry`.

## Why

1. A person who gets an error searches for its text. The text of libmagic
   finds the code of file, its bug reports and the answers to them. A text
   that magic changed finds nothing.
2. The test module compares the error of the port with the error of the
   oracle, which is libmagic (D30, D48). A change of the text makes every
   such comparison fail, or makes the test translate each text back.

This closes the item of `docs/BACKLOG.md` about the gerund form.
