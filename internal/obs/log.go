// Package obs provides IronClaw's structured logging abstraction. It wraps the
// standard library log/slog so the host control-plane and the in-sandbox agent
// emit consistent, machine-parseable records (text for humans, JSON for log
// shippers) through one constructor.
//
// Because IronClaw is a sealed, key-custody system, the logger is secret-aware:
// every handler installed by this package runs a redaction pass that masks
// attribute values whose key names look sensitive (keys, tokens, passwords,
// credentials, ...). Callers should additionally wrap known secrets with
// Secret() so a value is masked regardless of its attribute name. Redaction is
// best-effort defence-in-depth, not a licence to log secrets.
package obs

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// Format selects the wire encoding of log records.
type Format string

const (
	// FormatText is slog's human-readable key=value encoding (the default).
	FormatText Format = "text"
	// FormatJSON is line-delimited JSON, suitable for log shippers.
	FormatJSON Format = "json"
)

// Options configures a Logger. The zero value is usable: it yields a text
// logger at Info level writing to stderr with redaction enabled.
type Options struct {
	// Format is "text" (default) or "json". Unknown values fall back to text.
	Format Format
	// Level is the minimum level emitted. The zero value is slog.LevelInfo.
	Level slog.Level
	// Output is the sink. Nil defaults to os.Stderr.
	Output io.Writer
	// AddSource includes the source file:line of the call site.
	AddSource bool
	// DisableRedaction turns off the secret-masking pass. It defaults to off
	// (i.e. redaction on) so the safe behaviour requires no configuration.
	DisableRedaction bool
}

// Logger is the IronClaw structured logger. It embeds *slog.Logger, so all the
// standard slog methods (Info, Error, With, ...) are available directly, while
// New and Component centralise construction and scoping.
type Logger struct {
	*slog.Logger
}

// New builds a Logger from opts.
func New(opts Options) *Logger {
	out := opts.Output
	if out == nil {
		out = os.Stderr
	}

	handlerOpts := &slog.HandlerOptions{
		Level:     opts.Level,
		AddSource: opts.AddSource,
	}
	if !opts.DisableRedaction {
		handlerOpts.ReplaceAttr = redactingReplaceAttr
	}

	var h slog.Handler
	switch opts.Format {
	case FormatJSON:
		h = slog.NewJSONHandler(out, handlerOpts)
	default:
		h = slog.NewTextHandler(out, handlerOpts)
	}
	return &Logger{Logger: slog.New(h)}
}

// Default returns a text logger at Info level on stderr with redaction enabled.
func Default() *Logger { return New(Options{}) }

// Discard returns a Logger that drops every record. Useful in tests and as a
// safe nil-object so callers never have to guard against a missing logger.
func Discard() *Logger {
	return &Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// Component returns a child logger tagged with a "component" attribute (e.g.
// "host/gateway"), mirroring IronClaw's existing log-prefix convention. The
// receiver is left unchanged.
func (l *Logger) Component(name string) *Logger {
	return &Logger{Logger: l.Logger.With(slog.String("component", name))}
}

// With returns a child logger with the given attributes attached to every
// record. It overrides the embedded slog method so the result keeps the obs
// wrapper type (and thus Component) rather than decaying to *slog.Logger.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{Logger: l.Logger.With(args...)}
}

// WithContext is a convenience for callers that carry a context; it currently
// just exposes the embedded logger so context-aware handlers can be slotted in
// later without changing call sites.
func (l *Logger) WithContext(_ context.Context) *Logger { return l }
