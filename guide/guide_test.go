// Package guide checks the snippets of the guide, README.md, against the
// code they quote, so that the guide can't show code that no longer is:
// the service tasks, in Go, and its twin on NestJS and Knex, in nestjs.
// A snippet follows a marker that names where it comes from:
//
//	<!-- guide:go:create-task -->   a region of the Go code of ../tasks
//	<!-- guide:nest:create-task --> a region of the TypeScript of nestjs
//	<!-- guide:sql:ListTasks -->    a query of sqlc, by its name
//	<!-- guide:file:tasks/sqlc.yaml --> a whole file of the repository
//
// A region is the lines between //guide:<name> and //guide:end, without
// their common indentation. Every region must be in the guide. After a
// change to the code, go test -update rewrites the snippets.
package guide

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the snippets of README.md from the code")

func TestSnippets(t *testing.T) {
	sources := map[string]string{}
	regions(t, "../tasks", ".go", "go", sources)
	regions(t, "nestjs", ".ts", "nest", sources)
	var quoted []string // the regions the guide quotes

	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		out = append(out, lines[i])
		m := markerRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		key := m[1]
		if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "```") {
			t.Errorf("README.md:%d: no code block after the marker %s", i+1, key)
			continue
		}
		end := slices.IndexFunc(lines[i+2:], func(l string) bool { return l == "```" })
		if end < 0 {
			t.Fatalf("README.md:%d: the code block of %s doesn't end", i+2, key)
		}
		end += i + 2
		want, err := source(key, sources)
		if err != nil {
			t.Errorf("README.md:%d: %v", i+1, err)
			continue
		}
		quoted = append(quoted, key)
		if got := strings.Join(lines[i+2:end], "\n"); got != want && !*update {
			t.Errorf("README.md:%d: the snippet %s isn't the code it quotes; rewrite it with go test -update", i+3, key)
		}
		out = append(out, lines[i+1])
		out = append(out, strings.Split(want, "\n")...)
		out = append(out, "```")
		i = end
	}
	for key := range sources {
		if !slices.Contains(quoted, key) {
			t.Errorf("the region %s isn't in the guide: quote it or drop its markers", key)
		}
	}
	if *update {
		if err := os.WriteFile("README.md", []byte(strings.Join(out, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// markerRe matches the marker of a snippet and captures what it names.
var markerRe = regexp.MustCompile(`^<!-- guide:((?:go|nest|sql|file):\S+) -->$`)

// source returns the code that key names.
func source(key string, regions map[string]string) (string, error) {
	kind, name, _ := strings.Cut(key, ":")
	switch kind {
	case "sql":
		return query(name)
	case "file":
		data, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(name)))
		return strings.TrimRight(string(data), "\n"), err
	}
	code, ok := regions[key]
	if !ok {
		return "", fmt.Errorf("no region %s in the code", key)
	}
	return code, nil
}

// regions adds to sources the regions of the files of dir with the
// extension ext, as lang:<name>, skipping node_modules and the output of
// builds.
func regions(t *testing.T, dir, ext, lang string, sources map[string]string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "dist") {
			return filepath.SkipDir
		}
		if d.IsDir() || filepath.Ext(path) != ext {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var name string // of the region open, if any
		var start int
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			marker, ok := strings.CutPrefix(strings.TrimSpace(line), "//guide:")
			switch {
			case !ok:
			case marker == "end" && name != "":
				key := lang + ":" + name
				if _, dup := sources[key]; dup {
					t.Errorf("%s:%d: a second region %s", path, start, key)
				}
				sources[key] = dedent(lines[start:i])
				name = ""
			case marker == "end" || name != "":
				t.Errorf("%s:%d: //guide:%s, but the region %q is open", path, i+1, marker, name)
			default:
				name, start = marker, i+1
			}
		}
		if name != "" {
			t.Errorf("%s: the region %q doesn't end", path, name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// dedent returns lines without their leading and trailing blank lines and
// without the indentation they share.
func dedent(lines []string) string {
	blank := func(l string) bool { return strings.TrimSpace(l) == "" }
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	indent := ""
	for i, l := range lines {
		if blank(l) {
			continue
		}
		prefix := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		if i == 0 || len(prefix) < len(indent) {
			indent = prefix
		}
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimPrefix(l, indent)
	}
	return strings.Join(out, "\n")
}

// query returns the query of sqlc named name, from its -- name: line to
// the next query, in ../tasks/store/queries.
func query(name string) (string, error) {
	files, err := filepath.Glob("../tasks/store/queries/*.sql")
	if err != nil {
		return "", err
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		var q []string
		for line := range strings.SplitSeq(string(data), "\n") {
			if strings.HasPrefix(line, "-- name: ") {
				if q != nil {
					break
				}
				if strings.HasPrefix(line, "-- name: "+name+" ") {
					q = []string{}
				}
			}
			if q != nil {
				q = append(q, line)
			}
		}
		if q != nil {
			return dedent(q), nil
		}
	}
	return "", fmt.Errorf("no query %s of sqlc", name)
}
