# D44. The port translates each regular expression for Go

Status: Decided.

The maintainers decided on 2026-11-15 that the port translates the regular
expression of each rule into the syntax of Go's `regexp`, and does not take a
POSIX engine from another module.

libmagic runs each regular expression with `regcomp` and `regexec` of the C
library, in the C locale (D33). There one byte is one character. Go's
`regexp` reads UTF-8. The translator in `internal/engine/regex.go` closes the
gap in four ways:

1. It reads each byte of the pattern and of the text as the character with
   the same number, 0 to 255.
2. It writes each bracket expression as a set of bytes. A backslash in it is
   a literal, as POSIX says.
3. A negated bracket expression and `.` do not match a newline, as
   `REG_NEWLINE` says.
4. With `REG_ICASE`, only the ASCII letters fold, as in the C locale.

It also takes the longest match that starts first, as POSIX does. A
detection converts the bytes at an offset of a buffer to Latin-1 once, and
keeps them for each rule that reads there, in `internal/engine/latin1.go`.

The translator was compared with glibc and with
`github.com/oneregex/revera/go`, a POSIX engine in Go, on the 368 regular
expressions of the rules:

| Measure | Translator | revera |
| --- | --- | --- |
| Patterns that compile as glibc compiles them | 368 of 368 | 367 of 368 on bytes, 368 after the byte mapping |
| Matches equal to glibc, on 568,192 corpus cases | all | 13,118 differ on bytes, all after the byte mapping |
| Matches equal to glibc, on 8,693,632 random cases | all | 361,715 differ on bytes, all after the byte mapping |
| Bytes added to a stripped program | 299,008, the standard `regexp` | 139,264 |

The reasons:

1. revera decodes UTF-8, and a byte that is not UTF-8 matches nothing. So it
   needs the same byte mapping as the translator.
2. revera allows a bound of 255 at most. The largest bound in the rules today
   is 255, and libmagic allows 1,000.
3. revera has no tagged release.
4. The translator adds no dependency, and a program that already links
   `regexp` pays nothing more for it.

revera is about 160 KB smaller when nothing else links `regexp`. If D38 needs
the bytes, the maintainers can look at it again. The engine calls a regular
expression through one small type, so the change is small.
