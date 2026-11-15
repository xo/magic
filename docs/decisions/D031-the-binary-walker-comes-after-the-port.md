# D31. The binary walker comes after the port

Status: Decided.

The maintainers want magic to do the work of binwalk as well: walk a binary
file, such as a firmware image, find the files that are embedded in it at any
offset, and extract them.

The maintainers decided on 2026-11-15 that this work waits until the port of
libmagic is finished. It is in [`../BACKLOG.md`](../BACKLOG.md), with what the
three models said about it. The walker will reuse the engine of the port, so
the design of the engine does not need to wait for it. A choice in the engine
that makes the walker harder is worth a note in the backlog.
