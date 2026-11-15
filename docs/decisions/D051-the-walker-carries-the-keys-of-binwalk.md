# D51. The walker carries the keys of binwalk

Status: Decided.

The maintainers decided on 2026-11-15 that the walker decrypts the firmware
formats that binwalk decrypts, with the same keys. This answers question 21
of `docs/PLAN.md`.

binwalk decrypts `encfw`, `encrpted_img`, `dlink_tlv`, `dlke`, `shrs`,
`dkbs` and `openssl` with keys that are known for each vendor. The keys are
in the source of binwalk, under the MIT license. The walker translates them
with that notice (D49).

The formats and their keys are in a package of their own under
`walk/format/`, so that a program carries the keys only when it imports that
package (D50).
