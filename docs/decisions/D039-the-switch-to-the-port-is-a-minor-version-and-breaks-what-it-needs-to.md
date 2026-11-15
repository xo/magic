# D39. The switch to the port is a minor version, and breaks what it needs to

Status: Decided. Amends D5.

The maintainers decided on 2026-11-15 that the release that replaces the cgo
package with the port is the next minor version of `v0`, not `v1.0.0`, and that
magic does not need to stay compatible with the releases before it.

The API of the port is designed for the port. D5 shaped the API after the C
API of libmagic, with flags as numbers that mirror `MAGIC_*`. The port can
keep that, or change it, as the design needs. A name that the port does not
need, such as `MagicDB` (D38), goes.

Each break is listed in the release notes, with what a program changes to
keep working.
