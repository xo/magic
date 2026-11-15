# D52. The walker is built in five phases

Status: Decided.

The maintainers decided on 2026-11-15 that the work on the walker follows
the order of `docs/WALKER.md`. This answers question 22 of `docs/PLAN.md`.

1. W1: the scanner, the iterator, the registry, `Source`, the limits, the
   oracle of binwalk (D49), and the formats that need only a parser.
2. W2: members, `Extract`, the walk of members, and the compressed formats
   and simple archives. The decoders of `compress/` get a kind that counts
   the bytes of input that it reads.
3. W3: SquashFS, with the variants of vendors that `sasquatch` reads.
4. W4: the other file systems of firmware.
5. W5: the formats that binwalk decrypts (D51), and the rest.

The scanner, the oracle and the limits come before SquashFS, because every
format needs them.
