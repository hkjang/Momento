package httpapi

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// resolveSiteByID exists because six administrative handlers each decided for
// themselves whether to check which workspace a site belongs to, and four of them
// decided wrong. Its own comment records what that cost: a workspace_admin whose
// only membership was one workspace could rename, rotate the tracking key of,
// rotate the server key of, and delete a site in another. Rotating a key stops
// that site collecting anything until somebody redeploys its tracker, and the
// delete cannot be undone.
//
// The fix was one function to call. What makes that hold is nobody adding a
// seventh handler that parses the uuid itself — and the middleware will not
// notice, because it checks that the caller is at least a workspace_admin and
// says nothing about which workspace.
//
// So the routes are read, not the handlers: a route under /api/v1/sites/{id} is
// addressed by a uuid a caller supplies, and its handler has to resolve that uuid
// through the one place that applies the membership rule.
func TestEverySiteRouteAddressedByIDChecksTheWorkspace(t *testing.T) {
	t.Parallel()
	server, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	route := regexp.MustCompile(`api\.\w+\("(/api/v1/sites/\{(?:id|siteID)\}[^"]*)",\s*(.+?)\)\n`)
	handlerName := regexp.MustCompile(`s\.(\w+)\)*\s*$`)
	matches := route.FindAllStringSubmatch(string(server), -1)
	// Six administrative routes and everything analytical under {siteID}. The
	// six were the ones that went wrong, and they were guarded alone while the
	// other seventy-odd rested on convention — which is how the six came to be
	// wrong in the first place.
	if len(matches) < 50 {
		t.Fatalf("found only %d routes addressed by a site, so this proves nothing about the rest", len(matches))
	}

	// One index of the package, so a handler defined anywhere is found.
	bodies := map[string]string{}
	files, _ := os.ReadDir(".")
	// Every method on the server, not only the handlers: the membership rule is
	// often reached through a helper.
	definition := regexp.MustCompile(`func \(s \*Server\) (\w+)\(`)
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		source, err := os.ReadFile(file.Name())
		if err != nil {
			t.Fatalf("read %s: %v", file.Name(), err)
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

	checked := 0
	for _, match := range matches {
		path, wiring := match[1], strings.TrimSpace(match[2])
		name := handlerName.FindStringSubmatch(wiring)
		if name == nil {
			t.Errorf("%s is wired to %q and this test cannot tell which handler answers it", path, wiring)
			continue
		}
		if _, ok := bodies[name[1]]; !ok {
			t.Errorf("%s is wired to %s, which is not a handler in this package", path, name[1])
			continue
		}
		checked++
		if !resolves(name[1], bodies, map[string]bool{}) {
			t.Errorf("%s is answered by %s, which never reaches resolveSite or resolveSiteByID: the middleware checks that the caller is a workspace_admin somewhere, not that this site is in a workspace they belong to",
				path, name[1])
		}
	}
	if checked == 0 {
		t.Fatal("no route was checked")
	}
	t.Logf("checked %d routes addressed by a site uuid", checked)
}

// resolves says whether a handler reaches the membership rule, directly or
// through something it calls.
//
// The rule is not always in the handler: workspaceForSite calls resolveSite and
// four routes reach it that way, which is right — a helper that resolves the
// site is the same guarantee, and requiring the call to be inline would push
// somebody to inline it rather than to keep the check. Following the calls one
// package deep is what the property actually is.
func resolves(name string, bodies map[string]string, seen map[string]bool) bool {
	if seen[name] {
		return false
	}
	seen[name] = true
	body, ok := bodies[name]
	if !ok {
		return false
	}
	if strings.Contains(body, "resolveSite(") || strings.Contains(body, "resolveSiteByID(") {
		return true
	}
	for called := range bodies {
		if called == name {
			continue
		}
		if strings.Contains(body, "s."+called+"(") && resolves(called, bodies, seen) {
			return true
		}
	}
	return false
}
