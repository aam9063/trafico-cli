// Package service schedules `trafico watch run` with the platform
// scheduler: systemd user units on Linux and a launchd LaunchAgent on macOS
// (WATCH-02, spec S9, docs/ARRANQUE.md §5.1 and §5.5).
//
// The package is deliberately split in two halves so tests never touch the
// real scheduler:
//
//   - Pure generation: Unit returns the exact file path and content of every
//     unit file for the platform. The content carries no dates, no hostnames
//     and no path derived from the running machine: everything comes from
//     the injected Env.
//   - Thin runner: Install, Uninstall and Status take an Env with an
//     injected CommandRunner, so the enable/disable/load/unload commands are
//     asserted against a fake and a real systemctl or launchctl is only ever
//     executed by SystemRunner, which the CLI wires in.
//
// Uninstall removes exactly the files Install created (spec S9: no
// leftovers), is idempotent, and cleans up the directories it created when
// they end up empty. Windows has no integration: every entry point returns
// a typed UnsupportedError instead of pretending.
package service
