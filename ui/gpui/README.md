# Stint Deep Work GPUI dashboard

An experimental, read-only desktop view of Stint Deep Work session state. It
shows the latest mission, phase, task statuses, and selected task details. The
view reloads the state file every two seconds and does not control a session.

## Run

```sh
cargo run --release
```

This crate was built against `gpui-kit 0.7.3`; `Cargo.lock` pins the GPUI
snapshot that release uses. Linux builds need the native packages listed in
[Zed's Linux development guide](https://github.com/zed-industries/zed/blob/main/docs/src/development/linux.md).

## State source

The dashboard reads the latest session ID from
`${XDG_STATE_HOME:-$HOME/.local/state}/stint/deep/latest`, then loads
`stint/deep/<session-id>/deep.json`. It follows the same latest-session pointer
and JSON field names as Stint's Go coordinator.
