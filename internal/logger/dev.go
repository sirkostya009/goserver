// Package logger picks the slog handler the process logs through.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Setup installs the default logger: "json" for production, anything else is
// the dev handler. Both write to stdout, so logs are one stream a pipe can read
// and diagnostics stay out of stderr.
func Setup(format string) {
	if format == "json" {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
		return
	}
	slog.SetDefault(slog.New(NewDev(os.Stdout, slog.LevelDebug)))
}

// DevHandler writes one short line per record, for a human or a model reading
// a terminal, where every character costs attention or tokens:
//
//	23:55:58.123 I GET /api/users [200] 1.86ms
//
// No date, milliseconds on the clock, one letter per level, quotes only where a
// value needs them, rounded durations, groups flattened to dotted keys, no
// colors and no source. The two keys the request line uses drop their names, so
// the highest-volume line in the log reads like a log line rather than a map:
// a top-level status prints as [200] and a top-level dur as the bare duration.
// It is not meant to be parsed back: production logs go through the json
// handler.
type DevHandler struct {
	w     io.Writer
	mu    *sync.Mutex
	level slog.Leveler

	prefix string // open groups, as "a.b."
	attrs  []byte // what WithAttrs collected, already formatted
}

func NewDev(w io.Writer, level slog.Leveler) *DevHandler {
	return &DevHandler{w: w, mu: &sync.Mutex{}, level: level}
}

func (h *DevHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *DevHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	next := *h
	next.attrs = append([]byte(nil), h.attrs...)
	for _, a := range attrs {
		next.attrs = appendAttr(next.attrs, h.prefix, a)
	}
	return &next
}

func (h *DevHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := *h
	next.prefix = h.prefix + name + "."
	return &next
}

var bufPool = sync.Pool{New: func() any { b := make([]byte, 0, 256); return &b }}

func (h *DevHandler) Handle(_ context.Context, r slog.Record) error {
	bp := bufPool.Get().(*[]byte) //nolint:forcetypeassert // New always returns *[]byte
	b := (*bp)[:0]

	if !r.Time.IsZero() {
		b = r.Time.AppendFormat(b, timeFormat)
		b = append(b, ' ')
	}
	b = append(b, levelLetter(r.Level), ' ')
	b = append(b, r.Message...)
	b = append(b, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		b = appendAttr(b, h.prefix, a)
		return true
	})
	b = append(b, '\n')

	h.mu.Lock()
	_, err := h.w.Write(b)
	h.mu.Unlock()

	*bp = b
	bufPool.Put(bp)
	return err
}

// The keys DevHandler prints without their names. They are what Request builds.
const (
	statusKey = "status"
	durKey    = "dur"
)

// Request logs one finished request, in the shape the dev handler renders
// shortest: "GET /api/users/{id} [200] 1.86ms".
func Request(r *http.Request, status int, dur time.Duration, extra ...slog.Attr) {
	ctx := r.Context()
	if !slog.Default().Enabled(ctx, slog.LevelInfo) {
		return
	}
	attrs := append([]slog.Attr{slog.Int(statusKey, status), slog.Duration(durKey, dur)}, extra...)
	slog.LogAttrs(ctx, slog.LevelInfo, requestLine(r), attrs...)
}

// requestLine names what the request hit: the route it matched, which already
// reads "METHOD /path/{param}" and puts every call of one route under one line,
// or the raw path when no route matched and there is none to name.
func requestLine(r *http.Request) string {
	switch {
	case r.Pattern == "":
		return r.Method + " " + r.URL.Path
	case !strings.Contains(r.Pattern, " "): // a route that takes any method
		return r.Method + " " + r.Pattern
	}
	return r.Pattern
}

// timeFormat is the wall clock with milliseconds: a date repeated on every
// line says nothing, a millisecond tells two requests apart.
const timeFormat = "15:04:05.000"

func levelLetter(l slog.Level) byte {
	switch {
	case l >= slog.LevelError:
		return 'E'
	case l >= slog.LevelWarn:
		return 'W'
	case l >= slog.LevelInfo:
		return 'I'
	}
	return 'D'
}

// appendAttr writes " key=value". Groups recurse with a longer prefix, so an
// empty group writes nothing, which is what slog asks of handlers.
func appendAttr(b []byte, prefix string, a slog.Attr) []byte {
	v := a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return b
	}
	if v.Kind() == slog.KindGroup {
		if a.Key != "" {
			prefix += a.Key + "."
		}
		for _, ga := range v.Group() {
			b = appendAttr(b, prefix, ga)
		}
		return b
	}

	// the request line's own keys, bare. Only at the top level: inside a group
	// they are somebody else's status and duration.
	if prefix == "" {
		switch {
		case a.Key == statusKey && v.Kind() == slog.KindInt64:
			b = strconv.AppendInt(append(b, ' ', '['), v.Int64(), 10)
			return append(b, ']')
		case a.Key == durKey && v.Kind() == slog.KindDuration:
			return append(append(b, ' '), roundDuration(v.Duration()).String()...)
		}
	}

	b = append(b, ' ')
	b = append(b, prefix...)
	b = append(b, a.Key...)
	b = append(b, '=')

	//nolint:exhaustive // KindAny, KindGroup and KindLogValuer fall through to the Any() switch below
	switch v.Kind() {
	case slog.KindString:
		return appendString(b, v.String())
	case slog.KindInt64:
		return strconv.AppendInt(b, v.Int64(), 10)
	case slog.KindUint64:
		return strconv.AppendUint(b, v.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.AppendFloat(b, v.Float64(), 'g', 6, 64)
	case slog.KindBool:
		return strconv.AppendBool(b, v.Bool())
	case slog.KindDuration:
		return append(b, roundDuration(v.Duration()).String()...)
	case slog.KindTime:
		return v.Time().AppendFormat(b, timeFormat)
	}
	switch x := v.Any().(type) {
	case error:
		return appendString(b, x.Error())
	case fmt.Stringer:
		return appendString(b, x.String())
	default:
		return appendString(b, fmt.Sprintf("%+v", x))
	}
}

// roundDuration keeps three or four significant digits: 1.863037ms says
// nothing that 1.86ms does not.
func roundDuration(d time.Duration) time.Duration {
	switch {
	case d >= time.Second:
		return d.Round(10 * time.Millisecond)
	case d >= time.Millisecond:
		return d.Round(10 * time.Microsecond)
	case d >= time.Microsecond:
		return d.Round(10 * time.Nanosecond)
	}
	return d
}

func appendString(b []byte, s string) []byte {
	if needsQuoting(s) {
		return strconv.AppendQuote(b, s)
	}
	return append(b, s...)
}

func needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c <= ' ' || c == '"' || c == '=' || c == 0x7f {
				return true
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError {
			return true
		}
		i += size
	}
	return false
}
