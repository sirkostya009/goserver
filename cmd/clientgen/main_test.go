package main

import (
	"bytes"
	"io/fs"
	"os"
	"testing"

	"github.com/sirkostya009/rr/gen"
)

const regenerate = "run go generate ./internal/httpapi"

func loadRoot(t *testing.T) *gen.API {
	t.Helper()
	set, err := gen.Load("../../internal/httpapi")
	if err != nil {
		t.Fatal(err)
	}
	root, err := findRoot(set)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRenderGo_MatchesCommitted(t *testing.T) {
	t.Parallel()
	got, err := renderGo(loadRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../client/go/client_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("client/go/client_gen.go is stale, " + regenerate)
	}
}

func TestWriteTS_MatchesCommitted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := writeTS(loadRoot(t), dir); err != nil {
		t.Fatal(err)
	}
	gotFS, wantFS := os.DirFS(dir), os.DirFS("../../client/ts")
	for _, name := range []string{"client.ts", "types.ts"} {
		got, err := fs.ReadFile(gotFS, name)
		if err != nil {
			t.Fatal(err)
		}
		want, err := fs.ReadFile(wantFS, name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("client/ts/%s is stale, %s", name, regenerate)
		}
	}
}
