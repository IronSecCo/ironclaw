// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).

package contract

// Pinned cipher parameters. These are frozen here so the host and the sandbox
// compile byte-identical crypto; a mismatch is a silent decrypt failure rather
// than a build error, so the values must never drift.
const (
	CipherScheme   = "sqlcipher"
	CipherPageSize = 4096
	KDFRawKey      = true
)

// InboundSchema is the frozen DDL for the inbound queue database. The host is the
// sole writer of every table here; the sandbox reads only.
//
// Seq parity is frozen: the host uses EVEN seq numbers, the sandbox uses ODD.
// This lets either side write monotonically without coordinating on a counter.
const InboundSchema = `
CREATE TABLE IF NOT EXISTS messages_in (
    id                TEXT PRIMARY KEY,
    seq               INTEGER UNIQUE,
    kind              TEXT,
    timestamp         TEXT,
    status            TEXT,
    process_after     TEXT,
    recurrence        TEXT,
    series_id         TEXT,
    tries             INTEGER,
    trigger           INTEGER,
    platform_id       TEXT,
    channel_type      TEXT,
    thread_id         TEXT,
    content           TEXT,
    source_session_id TEXT,
    on_wake           INTEGER
);

CREATE TABLE IF NOT EXISTS destinations (
    name           TEXT PRIMARY KEY,
    display_name   TEXT,
    type           TEXT,
    channel_type   TEXT,
    platform_id    TEXT,
    agent_group_id TEXT
);

CREATE TABLE IF NOT EXISTS session_routing (
    id           INTEGER PRIMARY KEY CHECK(id = 1),
    channel_type TEXT,
    platform_id  TEXT,
    thread_id    TEXT
);

CREATE TABLE IF NOT EXISTS delivered (
    message_out_id     TEXT PRIMARY KEY,
    platform_message_id TEXT,
    status             TEXT,
    delivered_at       TEXT
);
`

// OutboundSchema is the frozen DDL for the outbound queue database. The sandbox is
// the sole writer of every table here; the host reads only.
//
// Seq parity is frozen: the host uses EVEN seq numbers, the sandbox uses ODD.
const OutboundSchema = `
CREATE TABLE IF NOT EXISTS messages_out (
    id            TEXT PRIMARY KEY,
    seq           INTEGER UNIQUE,
    in_reply_to   TEXT,
    timestamp     TEXT,
    deliver_after TEXT,
    recurrence    TEXT,
    kind          TEXT,
    platform_id   TEXT,
    channel_type  TEXT,
    thread_id     TEXT,
    content       TEXT
);

CREATE TABLE IF NOT EXISTS processing_ack (
    message_id     TEXT PRIMARY KEY,
    status         TEXT,
    status_changed TEXT
);

CREATE TABLE IF NOT EXISTS session_state (
    key        TEXT PRIMARY KEY,
    value      TEXT,
    updated_at TEXT
);

CREATE TABLE IF NOT EXISTS container_state (
    id                       INTEGER PRIMARY KEY CHECK(id = 1),
    current_tool             TEXT,
    tool_declared_timeout_ms INTEGER,
    tool_started_at          TEXT,
    updated_at               TEXT
);
`
