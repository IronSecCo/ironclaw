// Package integration holds cross-package end-to-end tests that wire real host-side
// and sandbox-side components together (e.g. the egress broker + the web_search tool)
// over real unix sockets, without a live model or container. It has no runtime code.
package integration
