// Command clientgen writes the goserver clients from the //rr: directives of
// the api: one method per route, grouped by the api type that declares it.
// -go writes the routes of the Go client, whose runtime is hand-written in
// client/go. -ts writes the TypeScript client and the types it speaks, whose
// runtime is hand-written in client/ts.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/sirkostya009/rr/gen"
)

func main() {
	goOut := flag.String("go", "", "Go client file to write")
	tsOut := flag.String("ts", "", "directory to write the TypeScript client into")
	flag.Parse()
	set, err := gen.Load(flag.Args()...)
	if err != nil {
		log.Fatal(err)
	}
	root, err := findRoot(set)
	if err != nil {
		log.Fatal(err)
	}
	if *goOut != "" {
		src, err := renderGo(root)
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(*goOut, src, 0o644); err != nil { //nolint:gosec // a source file, world-readable like the rest
			log.Fatal(err)
		}
	}
	if *tsOut != "" {
		if err := writeTS(root, *tsOut); err != nil {
			log.Fatal(err)
		}
	}
}

// findRoot is the one api nothing mounts: the dispatcher the server serves.
func findRoot(set *gen.Set) (*gen.API, error) {
	var roots []*gen.API
	for _, p := range set.Packages {
		for _, a := range p.APIs {
			if !a.Mounted {
				roots = append(roots, a)
			}
		}
	}
	if len(roots) != 1 {
		return nil, fmt.Errorf("want one root api, found %d", len(roots))
	}
	if len(roots[0].XMounts) > 0 {
		return nil, fmt.Errorf("%s: cross-package mounts are not supported", roots[0].Name)
	}
	return roots[0], nil
}

// manyBody is a body sent as a JSON array: a slice, or a sequence the server
// reads an element at a time.
func manyBody(b *gen.Arg) bool { return b.Type.Slice || b.Type.Seq }

func groupName(a *gen.API) string {
	return strings.TrimSuffix(a.Name, "Api")
}

// check refuses a route the clients cannot call faithfully.
func check(rt *gen.Route) error {
	where := rt.API.Name + "." + rt.Handler
	if rt.Method == "" {
		return fmt.Errorf("%s: a route answering any method has no method to send", where)
	}
	if rt.Result != nil && rt.Result.Ptr {
		return fmt.Errorf("%s: pointer results are not supported", where)
	}
	for _, a := range rt.Args {
		if a.Kind == gen.ArgQuery || a.Kind == gen.ArgHeader {
			return fmt.Errorf("%s: query and header params are not supported", where)
		}
	}
	for _, s := range rt.Segments {
		if s.Kind == gen.SegRest && s.Param == nil {
			return fmt.Errorf("%s: a bare * cannot be addressed", where)
		}
	}
	if b := rt.Body(); b != nil && (b.Body != gen.BodyJSON || b.Type.Pkg == "") {
		return fmt.Errorf("%s: only a named json struct body, or an array of one, is supported", where)
	}
	return nil
}
