package httpapi

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Which console queries have to key on the environment is decided by the server,
// not by how the console happens to build the URL.
//
// The console's own test asks whether a query's URL is built with rangeQuery,
// because that is what puts the environment into the query string. That caught
// the four screens that were answering from the previous environment's cache,
// and it is the wrong shape of question: api() appends the selected environment
// to every GET under /api/v1/sites/, so a screen can be environment-dependent
// without rangeQuery anywhere near it. The thing that actually decides is
// whether the handler reads requestEnvironment.
//
// So the rule is read from both sides at once, in the language that can see the
// handlers: every route whose handler reaches requestEnvironment, matched
// against the console query that calls it.
func TestTheConsoleKeysOnTheEnvironmentWhereverTheServerReadsIt(t *testing.T) {
	t.Parallel()
	// The package's own directory is where the test runs, so the repository is two
	// levels up.
	root := filepath.Join("..", "..")

	// The handlers, and what each one reaches.
	bodies := map[string]string{}
	definition := regexp.MustCompile(`func \(s \*Server\) (\w+)\(`)
	entries, err := os.ReadDir(filepath.Join(root, "internal", "httpapi"))
	if err != nil {
		t.Fatalf("read the package: %v", err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		source, err := os.ReadFile(filepath.Join(root, "internal", "httpapi", entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		lines := strings.Split(string(source), "\n")
		for index, line := range lines {
			match := definition.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			depth, end := 0, index
			for scan := index; scan < len(lines); scan++ {
				depth += strings.Count(lines[scan], "{") - strings.Count(lines[scan], "}")
				if depth <= 0 && scan > index {
					end = scan
					break
				}
			}
			bodies[match[1]] = strings.Join(lines[index:end+1], "\n")
		}
	}
	var readsEnvironment func(string, map[string]bool) bool
	readsEnvironment = func(name string, seen map[string]bool) bool {
		if seen[name] {
			return false
		}
		seen[name] = true
		body, ok := bodies[name]
		if !ok {
			return false
		}
		if strings.Contains(body, "requestEnvironment(") {
			return true
		}
		for called := range bodies {
			if called != name && strings.Contains(body, "s."+called+"(") && readsEnvironment(called, seen) {
				return true
			}
		}
		return false
	}

	server, err := os.ReadFile(filepath.Join(root, "internal", "httpapi", "server.go"))
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	route := regexp.MustCompile(`api\.Get\("/api/v1/sites/\{siteID\}(/[^"]*)",\s*(.+?)\)\n`)
	handlerName := regexp.MustCompile(`s\.(\w+)\)*\s*$`)
	varies := map[string]bool{}
	for _, match := range route.FindAllStringSubmatch(string(server), -1) {
		name := handlerName.FindStringSubmatch(strings.TrimSpace(match[2]))
		if name == nil {
			continue
		}
		if readsEnvironment(name[1], map[string]bool{}) {
			varies[match[1]] = true
		}
	}
	if len(varies) < 15 {
		t.Fatalf("only %d site routes were found to read the environment, so this proves nothing about the console", len(varies))
	}

	// The console's queries: the key, and the url its queryFn calls.
	pages, err := filepath.Glob(filepath.Join(root, "web", "src", "pages", "*.tsx"))
	if err != nil {
		t.Fatalf("list the console pages: %v", err)
	}
	more, _ := filepath.Glob(filepath.Join(root, "web", "src", "components", "*.tsx"))
	pages = append(pages, more...)
	block := regexp.MustCompile(`(?s)useQuery\(\{(.*?)\n  \}\)`)
	keyOf := regexp.MustCompile(`(?s)queryKey:\s*\[([^\]]*)\]`)
	urlOf := regexp.MustCompile("`/api/v1/sites/\\$\\{[^}]*\\}(/[a-z0-9-]+)")
	checked := 0
	for _, page := range pages {
		source, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("read %s: %v", page, err)
		}
		for _, found := range block.FindAllStringSubmatch(string(source), -1) {
			body := found[1]
			key := keyOf.FindStringSubmatch(body)
			if key == nil {
				continue
			}
			for _, call := range urlOf.FindAllStringSubmatch(body, -1) {
				if !varies[call[1]] {
					continue
				}
				checked++
				if testing.Verbose() {
					t.Logf("  %s %s", filepath.Base(page), call[1])
				}
				if !strings.Contains(key[1], "environment") {
					t.Errorf("%s reads %s, whose handler answers per environment, and its key is [%s]: React Query decides whether to fetch from the key, so switching environment leaves the previous one's answer on screen under the new one's name",
						filepath.Base(page), call[1], strings.Join(strings.Fields(key[1]), " "))
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no console query was matched to an environment-dependent route, so this proves nothing")
	}
	t.Logf("checked %d console queries against %d environment-dependent routes", checked, len(varies))
}
