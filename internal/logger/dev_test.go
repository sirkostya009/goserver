package logger

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/slogtest"
	"time"
)

var at = time.Date(2026, 9, 20, 23, 55, 58, 123456789, time.UTC)

func line(t *testing.T, h slog.Handler, level slog.Level, msg string, attrs ...slog.Attr) {
	t.Helper()
	r := slog.NewRecord(at, level, msg, 0)
	r.AddAttrs(attrs...)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
}

func TestDevHandler_Format(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	h := NewDev(&buf, slog.LevelDebug)

	for _, tc := range []struct {
		level slog.Level
		msg   string
		attrs []slog.Attr
		want  string
	}{
		// the request line: status and dur lose their names
		{slog.LevelInfo, "GET /api/users", []slog.Attr{
			slog.Int("status", 200), slog.Duration("dur", 1863037*time.Nanosecond),
		}, "23:55:58.123 I GET /api/users [200] 1.86ms"},

		// only at the top level, and only with the kind they are meant to hold
		{slog.LevelInfo, "grouped", []slog.Attr{
			slog.Group("job", slog.Int("status", 2), slog.Duration("dur", time.Second)),
			slog.String("status", "draining"),
		}, "23:55:58.123 I grouped job.status=2 job.dur=1s status=draining"},

		{slog.LevelDebug, "d", nil, "23:55:58.123 D d"},
		{slog.LevelWarn, "w", nil, "23:55:58.123 W w"},
		{slog.LevelError, "e", nil, "23:55:58.123 E e"},

		{slog.LevelInfo, "quoting", []slog.Attr{
			slog.String("plain", "guy@acme.io"), slog.String("spaced", "two words"),
			slog.String("empty", ""), slog.String("eq", "a=b"), slog.String("unicode", "яблоко"),
		}, `23:55:58.123 I quoting plain=guy@acme.io spaced="two words" empty="" eq="a=b" unicode=яблоко`},

		{slog.LevelError, "handler", []slog.Attr{slog.Any("err", errors.New("relation does not exist"))},
			`23:55:58.123 E handler err="relation does not exist"`},

		{slog.LevelInfo, "kinds", []slog.Attr{
			slog.Bool("ok", true), slog.Float64("ratio", 0.123456789), slog.Uint64("n", 7),
			slog.Duration("slow", 2345678*time.Microsecond), slog.Duration("tiny", 581*time.Nanosecond),
		}, "23:55:58.123 I kinds ok=true ratio=0.123457 n=7 slow=2.35s tiny=581ns"},

		{slog.LevelInfo, "groups", []slog.Attr{
			slog.Group("req", slog.String("id", "x"), slog.Group("user", slog.Int("n", 1))),
			slog.Group("nothing"),
		}, "23:55:58.123 I groups req.id=x req.user.n=1"},
	} {
		buf.Reset()
		line(t, h, tc.level, tc.msg, tc.attrs...)
		if got := strings.TrimSuffix(buf.String(), "\n"); got != tc.want {
			t.Errorf("\n got %s\nwant %s", got, tc.want)
		}
	}
}

func TestDevHandler_WithAttrsAndGroups(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	base := NewDev(&buf, slog.LevelInfo)
	scoped := base.WithAttrs([]slog.Attr{slog.String("svc", "api")}).WithGroup("db").WithAttrs([]slog.Attr{slog.Int("pool", 4)})

	line(t, scoped, slog.LevelInfo, "query", slog.Int("rows", 3))
	line(t, base, slog.LevelInfo, "untouched")
	want := "23:55:58.123 I query svc=api db.pool=4 db.rows=3\n23:55:58.123 I untouched\n"
	if buf.String() != want {
		t.Errorf("\n got %q\nwant %q", buf.String(), want)
	}
}

func TestDevHandler_Level(t *testing.T) {
	t.Parallel()
	h := NewDev(&bytes.Buffer{}, slog.LevelWarn)
	if h.Enabled(context.Background(), slog.LevelInfo) || !h.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("level threshold ignored")
	}
}

func TestDevHandler_Concurrent_LinesStayWhole(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := slog.New(NewDev(&buf, slog.LevelInfo))
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { log.Info("GET /api/users", "status", 200, "dur", time.Millisecond) })
	}
	wg.Wait()
	for l := range strings.Lines(buf.String()) {
		if !strings.HasSuffix(l, " I GET /api/users [200] 1ms\n") {
			t.Fatalf("torn line %q", l)
		}
	}
}

// slogtest checks the handler contract: resolved values, empty attrs and
// groups dropped, WithGroup and WithAttrs nesting, zero time omitted
func TestDevHandler_Conformance(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := slogtest.TestHandler(NewDev(&buf, slog.LevelDebug), func() []map[string]any {
		var out []map[string]any
		for l := range strings.Lines(buf.String()) {
			out = append(out, parse(strings.TrimSuffix(l, "\n")))
		}
		return out
	})
	if err != nil {
		t.Error(err)
	}
}

// parse reads a line back well enough for slogtest, whose messages are one
// word and whose values hold no spaces.
func parse(l string) map[string]any {
	m := map[string]any{}
	fields := strings.Fields(l)
	if len(fields[0]) == len(timeFormat) && fields[0][2] == ':' {
		m[slog.TimeKey] = fields[0]
		fields = fields[1:]
	}
	m[slog.LevelKey], m[slog.MessageKey] = fields[0], fields[1]
	for _, f := range fields[2:] {
		key, value, _ := strings.Cut(f, "=")
		at := m
		path := strings.Split(key, ".")
		for _, group := range path[:len(path)-1] {
			next, ok := at[group].(map[string]any)
			if !ok {
				next = map[string]any{}
				at[group] = next
			}
			at = next
		}
		at[path[len(path)-1]] = value
	}
	return m
}

const id = "AaDAkZAkfJqWQZ2kfnUM5w"

func req(method, path, pattern string) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	r.Pattern = pattern
	return r
}

// the route stands in for the path, so every call of one route reads the same
//
//nolint:paralleltest // redirects the shared slog default, which every other test's logging would race
func TestRequest_NamesTheRoute(t *testing.T) {
	for _, tc := range []struct{ method, path, pattern, want string }{
		{"GET", "/api/users/" + id, "GET /api/users/{id}", "GET /api/users/{id}"},
		{"GET", "/nope", "", "GET /nope"},                         // nothing matched
		{"PATCH", "/api/admin", "/api/admin", "PATCH /api/admin"}, // route takes any method
	} {
		var buf bytes.Buffer
		slog.SetDefault(slog.New(NewDev(&buf, slog.LevelInfo)))
		Request(req(tc.method, tc.path, tc.pattern), 200, time.Millisecond)
		if got := buf.String(); !strings.Contains(got, " I "+tc.want+" [200] 1ms\n") {
			t.Errorf("pattern %q gave %q, want line %q", tc.pattern, got, tc.want)
		}
	}
	slog.SetDefault(slog.New(slog.DiscardHandler))
}

//nolint:paralleltest // redirects the shared slog default, which every other test's logging would race
func TestRequest_Line(t *testing.T) {
	var buf bytes.Buffer
	slog.SetDefault(slog.New(NewDev(&buf, slog.LevelInfo)))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.DiscardHandler)) })

	Request(req("GET", "/api/users/"+id, "GET /api/users/{id}"), 200, 175330*time.Nanosecond)
	Request(req("POST", "/api/users", "POST /api/users"), 400, time.Millisecond, slog.String("tenant", "acme"))

	got := buf.String()
	want := " I GET /api/users/{id} [200] 175.33µs\n"
	if !strings.HasSuffix(strings.SplitAfter(got, "\n")[0], want) {
		t.Errorf("\n got %q\nwant a line ending %q", got, want)
	}
	if !strings.Contains(got, " I POST /api/users [400] 1ms tenant=acme\n") {
		t.Errorf("extra attrs dropped: %q", got)
	}
}

// json keeps the fields a machine needs, whatever the dev handler does with them
//
//nolint:paralleltest // redirects the shared slog default, which every other test's logging would race
func TestRequest_JSON_StaysStructured(t *testing.T) {
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.DiscardHandler)) })

	Request(req("GET", "/api/users", "GET /api/users"), 200, time.Millisecond)
	for _, want := range []string{`"msg":"GET /api/users"`, `"status":200`, `"dur":1000000`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("%s missing from %s", want, buf.String())
		}
	}
}
