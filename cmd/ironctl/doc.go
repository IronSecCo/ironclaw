// Package main implements ironctl, IronClaw's administrative and local
// diagnostics command-line interface.
//
// The run function in main.go parses global options, handles version and help
// requests, and dispatches the first positional argument to the matching
// top-level command handler. The change command uses a second switch to route
// its submit, pending, history, approve, and reject verbs.
//
// To add a top-level subcommand, implement a focused cmdX handler in the
// appropriate file, connect it to run in main.go, and update the command
// reference and dispatch tests.
package main
