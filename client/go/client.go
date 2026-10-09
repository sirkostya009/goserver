// Package client calls the goserver api. Its routes are generated from the
// server's //rr: directives into client_gen.go, and its types are domain's.
package client

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/sirkostya009/ggen"

	"goserver/domain"
)

// Options configures a Client. The zero value calls the api anonymously
// through http.DefaultClient.
type Options struct {
	// HTTPClient sends every request. A session needs one with a cookie jar:
	// Login answers with the session cookie, and the jar sends it from then on.
	HTTPClient *http.Client
	// Token is an api key, or the admin token, sent as a bearer token.
	Token string
	// Tenant is the tenant the admin acts in. Nobody else sends one.
	Tenant domain.TenantID
}

func New(baseURL string, o Options) *Client {
	c := &conn{http: o.HTTPClient, base: strings.TrimRight(baseURL, "/")}
	if c.http == nil {
		c.http = http.DefaultClient
	}
	if o.Token != "" {
		c.auth = []string{"Bearer " + o.Token}
	}
	if o.Tenant != (domain.TenantID{}) {
		c.tenant = []string{o.Tenant.String()}
	}
	return wrap(c)
}

// WithTenant is a copy of the client acting in another tenant, for the admin.
func (cl *Client) WithTenant(id domain.TenantID) *Client {
	c := *cl.c
	c.tenant = []string{id.String()}
	return wrap(&c)
}

// Error is a response outside 2xx: its status and the server's plain text.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return strconv.Itoa(e.Status) + " " + e.Message
}

// Is matches the domain sentinel the server mapped to the status, so
// errors.Is(err, domain.ErrNotFound) reads the same on both ends of the wire.
func (e *Error) Is(target error) bool {
	switch e.Status {
	case http.StatusBadRequest:
		return target == domain.ErrInvalid || target == domain.ErrMalformed
	case http.StatusUnprocessableEntity:
		return target == domain.ErrInvalid
	case http.StatusUnauthorized:
		return target == domain.ErrUnauthenticated
	case http.StatusForbidden:
		return target == domain.ErrForbidden
	case http.StatusNotFound:
		if e.Message == domain.ErrUnknownTenant.Error() {
			return target == domain.ErrUnknownTenant
		}
		return target == domain.ErrNotFound
	case http.StatusConflict:
		return target == domain.ErrConflict
	}
	return false
}

// Header values are built once and shared by every request, which only reads
// them.
type conn struct {
	http   *http.Client
	base   string
	auth   []string
	tenant []string
}

var jsonCT = []string{"application/json"}

const (
	maxErrorBody = 4 << 10
	maxPrealloc  = 4 << 20
)

func (c *conn) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header["Content-Type"] = jsonCT
	}
	if c.auth != nil {
		req.Header["Authorization"] = c.auth
	}
	if c.tenant != nil {
		req.Header["X-Tenant-Id"] = c.tenant
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, readError(res)
	}
	return res, nil
}

// send encodes into a buffer of its own rather than a pooled one: the
// transport may still be reading the body after Do returns.
func send[B ggen.Marshaler](ctx context.Context, c *conn, method, path string, body B) (*http.Response, error) {
	b, err := body.AppendJSON(make([]byte, 0, body.JSONSize()))
	if err != nil {
		return nil, err
	}
	return c.do(ctx, method, path, b)
}

func sendMany[B ggen.Marshaler](ctx context.Context, c *conn, method, path string, body []B) (*http.Response, error) {
	if body == nil {
		body = []B{} // null is no array
	}
	b, err := ggen.MarshalSlice(body)
	if err != nil {
		return nil, err
	}
	return c.do(ctx, method, path, b)
}

func none(res *http.Response, err error) error {
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, err = io.Copy(io.Discard, res.Body)
	return err
}

// one and many decode straight from the bytes read. Each response gets fresh
// ones, so the strings decoded may alias them.
func one[T ggen.Decoder[T]](res *http.Response, err error) (T, error) {
	var v T
	if err != nil {
		return v, err
	}
	data, err := readAll(res)
	if err != nil {
		return v, err
	}
	v, n, err := v.DecodeFrom(data)
	if err != nil {
		return v, err
	}
	if i := ggen.SkipSpace(data, n); i < len(data) {
		return v, ggen.NewParseErr("", i, ggen.ErrTrailingData)
	}
	return v, nil
}

func many[T ggen.Decoder[T]](res *http.Response, err error) ([]T, error) {
	if err != nil {
		return nil, err
	}
	data, err := readAll(res)
	if err != nil {
		return nil, err
	}
	return ggen.UnmarshalSlice[T](data)
}

func readAll(res *http.Response) ([]byte, error) {
	defer res.Body.Close()
	if n := res.ContentLength; n >= 0 && n <= maxPrealloc {
		b := make([]byte, n)
		_, err := io.ReadFull(res.Body, b)
		return b, err
	}
	return io.ReadAll(res.Body)
}

func readError(res *http.Response) error {
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
	return &Error{Status: res.StatusCode, Message: strings.TrimSpace(string(b))}
}
