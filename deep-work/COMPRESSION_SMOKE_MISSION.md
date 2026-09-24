# Hermes compression dashboard smoke

## Objective

Force one bounded Hermes task to accumulate enough tool-result context to trigger
compression, then prove the task can still write and verify a small artifact.

## Success

- Hermes completes the task and the coordinator independently verifies its artifact.
- The Worker dashboard view reports a completed compression and medium-route traffic.
- No compressed-summary truncation or context-overflow event is recorded.

## Constraints

- Work only in the supplied smoke worktree.
- Read every fixture chunk with the exact `dd` commands below; do not replace them
  with a checksum, a line count, or another summary command.
- Do not alter `context-fixture.txt`.

## Tasks

- [ ] COMPRESS-001: Read all twelve 16 KiB chunks of `context-fixture.txt` with twelve separate shell tool calls, in order: `dd if=context-fixture.txt bs=16384 count=1 skip=0 status=none`, then the same command with `skip=1` through `skip=11`. Do not replace these reads with a loop, checksum, line count, or another summary command. After the twelfth command returns, create the required artifact with `printf '%s\n' 'twelve chunks read after compression smoke' > compression-smoke.ok`; then run `test -f compression-smoke.ok && grep -Fqx 'twelve chunks read after compression smoke' compression-smoke.ok` and report the filename only after it passes.
  - reasoning: medium
  - acceptance: the artifact proves the required reads were completed and the worktree remains usable after compaction.
  - verify: test -f compression-smoke.ok && grep -Fqx 'twelve chunks read after compression smoke' compression-smoke.ok

## Verification

test -f compression-smoke.ok && grep -Fqx 'twelve chunks read after compression smoke' compression-smoke.ok
