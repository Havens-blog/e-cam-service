// Command depcheck enforces cross-domain dependency rules inside internal/.
//
// Rules (see docs/architecture: domain boundaries):
//
//	R1 reach-in:  a file under internal/<A>/... must not import
//	              internal/<B>/repository (or .../repository/dao) when A != B.
//	              Cross-domain access must go through a domain interface, not
//	              the other domain's persistence layer.
//	R2 shared-leaf: internal/shared/... must not import any internal/<domain>.
//	              shared is a leaf and may only be imported, never import back.
//	R3 severed-edge: domain pairs that have been deliberately decoupled (see
//	              severedEdges) must not import each other in EITHER direction
//	              at ANY layer — not just repository/dao. Locks in completed
//	              decoupling (e.g. the cert extraction severed alert<->cert) so
//	              a service-layer back-edge cannot silently regrow under R1's
//	              radar. Route cross-domain needs via a consumer interface
//	              injected at the composition root.
//	R4 extraction-ready: a domain in extractionReadyDomains must not import ANY
//	              other internal/<domain> at ANY layer. These domains have been
//	              driven to zero cross-domain outbound coupling and are ready to
//	              be lifted into a standalone service; every outside need is met
//	              through a consumer interface injected at the composition root.
//	              Locks that invariant so a new feature cannot re-tangle the
//	              domain back into the monolith (e.g. cert).
//
// The checker is a ratchet: known pre-existing violations are frozen in a
// baseline file and tolerated; only NEW violations fail the build. Fix a
// historical violation and delete its baseline line to prevent regressions.
//
// Usage:
//
//	go run ./scripts/depcheck            # check, exit 1 on new violations
//	go run ./scripts/depcheck -update    # rewrite baseline from current tree
//	go run ./scripts/depcheck -root .    # override module root (default: cwd)
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const modulePrefix = "github.com/Havens-blog/e-cam-service/internal/"

// severedEdges lists domain pairs that have been deliberately, fully decoupled.
// Membership is unordered — BOTH directions are forbidden, at ANY layer (not
// just repository/dao). Add a pair here the moment its cross-import count hits
// zero in both directions, to ratchet the decoupling shut.
//
//	alert|cert — cert service extraction (strangler step 1): cert's alert
//	             publisher moved into internal/cert/alertpub (zero alert import);
//	             alert's generic infra is adapted to cert ports at the
//	             composition root. Neither domain may import the other again.
var severedEdges = map[string]bool{
	"alert|cert": true,
}

// extractionReadyDomains lists domains driven to ZERO cross-domain outbound
// coupling, ready to be lifted into a standalone service. A domain here must
// not import ANY other internal/<domain> at any layer; every outside need is
// met through a consumer interface injected at the composition root. Add a
// domain here only once `go list -deps` shows it imports no sibling domain, to
// ratchet that invariant shut.
//
//	cert — cert service extraction (strangler step 2): all outbound edges
//	        (alert, asset, cam/dns, audit, shared/middleware) replaced by cert
//	        consumer ports + composition-root adapters. cert internal depends
//	        on no other e-cam domain.
var extractionReadyDomains = map[string]bool{
	"cert": true,
}

// edgeKey normalizes an unordered domain pair to a lookup key.
func edgeKey(a, b string) string {
	if a < b {
		return a + "|" + b
	}
	return b + "|" + a
}

type violation struct {
	file   string // path relative to internal/, forward slashes
	imp    string // offending import path (module-relative, after internal/)
	rule   string // "R1" or "R2"
	detail string
}

func (v violation) key() string { return v.file + "|" + v.imp }

func main() {
	var (
		root       string
		baseline   string
		update     bool
		includeTst bool
	)
	flag.StringVar(&root, "root", ".", "module root containing internal/")
	flag.StringVar(&baseline, "baseline", "", "baseline file (default: <scriptdir>/baseline.txt)")
	flag.BoolVar(&update, "update", false, "rewrite baseline from current violations")
	flag.BoolVar(&includeTst, "tests", false, "also scan _test.go files")
	flag.Parse()

	internalDir := filepath.Join(root, "internal")
	if fi, err := os.Stat(internalDir); err != nil || !fi.IsDir() {
		fatalf("internal/ not found under %q (use -root); %v", root, err)
	}
	if baseline == "" {
		baseline = filepath.Join("scripts", "depcheck", "baseline.txt")
		if root != "." {
			baseline = filepath.Join(root, "scripts", "depcheck", "baseline.txt")
		}
	}

	vios, err := scan(internalDir, includeTst)
	if err != nil {
		fatalf("scan failed: %v", err)
	}
	sort.Slice(vios, func(i, j int) bool { return vios[i].key() < vios[j].key() })

	if update {
		if err := writeBaseline(baseline, vios); err != nil {
			fatalf("write baseline: %v", err)
		}
		fmt.Printf("depcheck: baseline updated with %d entries -> %s\n", len(vios), baseline)
		return
	}

	allowed, err := readBaseline(baseline)
	if err != nil {
		fatalf("read baseline (%s): %v\nrun `go run ./scripts/depcheck -update` to create it", baseline, err)
	}

	var isNew []violation
	seen := map[string]bool{}
	for _, v := range vios {
		seen[v.key()] = true
		if !allowed[v.key()] {
			isNew = append(isNew, v)
		}
	}
	// Baseline entries that no longer occur: nudge to tighten.
	var stale []string
	for k := range allowed {
		if !seen[k] {
			stale = append(stale, k)
		}
	}
	sort.Strings(stale)

	if len(isNew) == 0 {
		fmt.Printf("depcheck: OK (%d known boundary exceptions tolerated)\n", len(vios))
		if len(stale) > 0 {
			fmt.Printf("depcheck: %d baseline line(s) no longer needed — delete to tighten:\n", len(stale))
			for _, s := range stale {
				fmt.Printf("  - %s\n", s)
			}
		}
		return
	}

	fmt.Fprintf(os.Stderr, "depcheck: %d NEW cross-domain boundary violation(s):\n\n", len(isNew))
	for _, v := range isNew {
		fmt.Fprintf(os.Stderr, "  [%s] internal/%s\n        imports %s\n        %s\n\n", v.rule, v.file, modulePrefix+v.imp, v.detail)
	}
	fmt.Fprintf(os.Stderr, "Cross-domain access must go through a domain interface, not another\n")
	fmt.Fprintf(os.Stderr, "domain's repository/dao, and internal/shared must stay a leaf.\n")
	fmt.Fprintf(os.Stderr, "If this is a deliberate, reviewed exception, add its line to %s.\n", baseline)
	os.Exit(1)
}

func scan(internalDir string, includeTst bool) ([]violation, error) {
	var out []violation
	err := filepath.Walk(internalDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if !includeTst && strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(internalDir, path)
		rel = filepath.ToSlash(rel)
		fromDomain := firstSeg(rel)
		if fromDomain == "" {
			return nil
		}

		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", rel, perr)
		}
		for _, spec := range f.Imports {
			imp := strings.Trim(spec.Path.Value, `"`)
			if !strings.HasPrefix(imp, modulePrefix) {
				continue
			}
			sub := strings.TrimPrefix(imp, modulePrefix) // e.g. "account/repository/dao"
			toDomain := firstSeg(sub)
			if toDomain == "" || toDomain == fromDomain {
				continue
			}
			// R3: deliberately severed domain pairs — forbid either direction
			// at any layer (checked before R1/R2 so a severed edge reports as
			// R3 rather than slipping to repository-only R1).
			if severedEdges[edgeKey(fromDomain, toDomain)] {
				out = append(out, violation{rel, sub, "R3",
					fmt.Sprintf("internal/%s and internal/%s are a deliberately severed pair; route the need through a consumer interface injected at the composition root, never import across this edge.", fromDomain, toDomain)})
				continue
			}
			// R4: an extraction-ready domain must not import ANY other domain
			// (checked before R2/R1 so a stray import reports as R4 rather than
			// only tripping the repository-layer R1).
			if extractionReadyDomains[fromDomain] {
				out = append(out, violation{rel, sub, "R4",
					fmt.Sprintf("internal/%s is extraction-ready (zero cross-domain outbound); it must not import internal/%s — add a consumer interface in internal/%s and inject the implementation at the composition root.", fromDomain, toDomain, fromDomain)})
				continue
			}
			// R2: shared must not depend on any domain.
			if fromDomain == "shared" {
				out = append(out, violation{rel, sub, "R2",
					"internal/shared is a leaf; move the domain-specific logic out or inject it via an interface."})
				continue
			}
			// R1: reach-in to another domain's persistence layer.
			if hasSeg(sub, "repository") || hasSeg(sub, "dao") {
				out = append(out, violation{rel, sub, "R1",
					fmt.Sprintf("define the needed interface in internal/%s and depend on that, not %s's repository.", fromDomain, toDomain)})
			}
		}
		return nil
	})
	return out, err
}

func firstSeg(p string) string {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
}

func hasSeg(p, seg string) bool {
	for _, s := range strings.Split(p, "/") {
		if s == seg {
			return true
		}
	}
	return false
}

func readBaseline(path string) (map[string]bool, error) {
	m := map[string]bool{}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m[line] = true
	}
	return m, sc.Err()
}

func writeBaseline(path string, vios []violation) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# depcheck baseline — frozen cross-domain boundary exceptions.\n")
	b.WriteString("# Format: <file-relative-to-internal>|<import-relative-to-internal>\n")
	b.WriteString("# Only shrink this file. Fix a violation, delete its line; never add without review.\n")
	b.WriteString("# Regenerate (adds nothing new on a clean tree): go run ./scripts/depcheck -update\n\n")
	for _, v := range vios {
		b.WriteString(v.key())
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "depcheck: "+format+"\n", args...)
	os.Exit(2)
}
