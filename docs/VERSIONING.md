# Identifying a Stint build

`stint version`, `stint --version`, and `stint -v` print the build label,
full embedded commit, and `tree clean`, `tree dirty`, or `tree unknown`.
This identifies the local CLI, not a remote NInfer runtime or a previously
started watchdog. No Git commands, provider calls, or session reads are made.

Normal `make build` and `go build -o /tmp/stint ./cmd/stint` builds use Go's
automatic VCS stamping. The label defaults to `dev`; when Go embeds a module
version, that version is used (including development pseudo-versions).
Building with `-buildvcs=false`, outside Git, or from some source archives
can leave provenance unknown. Unknown never means clean.

For an intentionally designated release, the existing version variable can be
set using `go build -ldflags '-X main.version=<release>' -o /tmp/stint ./cmd/stint`.
Use an actual designated release identifier, not an invented version. The
override does not suppress dirty state or missing provenance. This change
creates no release or tag. Build timestamps are omitted: `vcs.time` in
`go version -m /path/to/stint` is the commit timestamp, not the build date.

Historical Go builds advanced a constant through 0.0.x to 0.1.0 in release
commit 71d5227 (2026-08-29), then continued reporting 0.1.0 as development
advanced. The command survived; provenance was never displayed. No Stint Git
tags or GitHub releases were present at the 2026-09-07 inspection.

To inspect an older binary, use `type -a stint`, `readlink -f` on the resolved
path, `sha256sum`, and `go version -m`. Embedded `vcs.modified=true` identifies
the base commit plus unspecified local changes; it cannot reconstruct those
changes. A hash identifies bytes, not their complete source history.

`make build` overwrites `bin/stint`. If your PATH symlink points there, that
also replaces the CLI you invoke. Build to a separate path when comparing
versions. This investigation deliberately did not replace installed binaries.
