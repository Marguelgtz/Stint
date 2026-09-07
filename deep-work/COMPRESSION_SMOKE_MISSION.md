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

- [ ] COMPRESS-001: Read all twelve 16 KiB chunks of `context-fixture.txt` using these exact commands, in order: `dd if=context-fixture.txt bs=16384 count=1 skip=0 status=none` through `dd if=context-fixture.txt bs=16384 count=1 skip=11 status=none`. After all reads finish, create `compression-smoke.ok` containing `twelve chunks read after compression smoke` and report the filename.
  - reasoning: medium
  - acceptance: the artifact proves the required reads were completed and the worktree remains usable after compaction.
  - verify: test -f compression-smoke.ok && grep -Fqx 'twelve chunks read after compression smoke' compression-smoke.ok

## Verification

test -f compression-smoke.ok && grep -Fqx 'twelve chunks read after compression smoke' compression-smoke.ok
