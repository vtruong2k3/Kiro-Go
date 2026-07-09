// Example tests for add-account dialog composition in the modern panel
// (web/app.js).
//
// Kiro-Go is primarily a Go project with no JavaScript test harness (no
// package.json, node_modules, or jest/vitest config). web/app.js is a single
// IIFE that exposes none of its internals, so it cannot be required/executed
// from a unit test without introducing a heavy new toolchain. Consistent with
// how the rest of this repo validates behavior (Go's testing package), these
// example tests assert the dialog-composition contract at the source level:
//
//   - The add dialog offers one login method per supported provider. The
//     methods are declared in the METHOD_CARDS map and ordered by ALL_METHODS;
//     modalAdd renders cards from that map (optionally filtered to the current
//     provider), so the contract is asserted against METHOD_CARDS + ALL_METHODS
//     rather than inline methodCard(...) calls in modalAdd.
//   - showModal routes the 'enterprisesso' type to modalEnterpriseSso, and
//     modalEnterpriseSso replaces modalBody.innerHTML with its login view, so
//     selecting it after another method's view is shown swaps in the SSO view.
package web

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// addMethodCards is the set of login methods the add dialog must offer. It is
// the source of truth for the method-card contract test below; add a method
// here when a new methodCard(...) is wired into modalAdd.
var addMethodCards = []string{
	"builderid",
	"iam",
	"sso",
	"local",
	"credentials",
	"cookie",
	"enterprisesso",
	"antigravity",
	"grok",
}

// readAppJS returns the contents of the module that defines the add-account
// dialog composition. The UI was split into ES modules under web/js/; the
// dialog methods (METHOD_CARDS, ALL_METHODS, showModal, modalEnterpriseSso)
// now live in web/js/auth-modals.js.
func readAppJS(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("js/auth-modals.js")
	if err != nil {
		t.Fatalf("read js/auth-modals.js: %v", err)
	}
	return string(data)
}

// extractFunctionBody returns the body of the first `function <name>(` in src,
// using brace matching so nested braces are handled correctly.
func extractFunctionBody(t *testing.T, src, name string) string {
	t.Helper()
	sig := "function " + name + "("
	start := strings.Index(src, sig)
	if start < 0 {
		t.Fatalf("function %s not found in app.js", name)
	}
	// Find the opening brace of the body.
	open := strings.Index(src[start:], "{")
	if open < 0 {
		t.Fatalf("opening brace for %s not found", name)
	}
	open += start
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open : i+1]
			}
		}
	}
	t.Fatalf("unbalanced braces for %s", name)
	return ""
}

// TestModalAddRendersAllMethodCards verifies the add dialog offers exactly the
// expected set of selectable login methods, with no duplicates and nothing
// unexpected. The methods live in the METHOD_CARDS map (keyed by method id) and
// their default order in the ALL_METHODS array; modalAdd renders from these, so
// the contract is asserted against those two declarations.
func TestModalAddRendersAllMethodCards(t *testing.T) {
	src := readAppJS(t)

	// Keys of the METHOD_CARDS map: `<id>: () => methodCard('<id>', ...)`.
	cardRe := regexp.MustCompile(`(?m)^\s*([a-z]+):\s*\(\)\s*=>\s*methodCard\(`)
	matches := cardRe.FindAllStringSubmatch(src, -1)

	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		if seen[m[1]] {
			t.Fatalf("duplicate method card %q in METHOD_CARDS", m[1])
		}
		seen[m[1]] = true
	}

	if len(matches) != len(addMethodCards) {
		got := make([]string, 0, len(matches))
		for _, m := range matches {
			got = append(got, m[1])
		}
		t.Fatalf("expected %d entries in METHOD_CARDS, got %d: %v", len(addMethodCards), len(matches), got)
	}

	for _, want := range addMethodCards {
		if !seen[want] {
			t.Errorf("METHOD_CARDS is missing method %q", want)
		}
	}

	// ALL_METHODS must list every method id exactly once so the unfiltered
	// dialog renders all of them.
	allMethods := extractArrayLiteral(t, src, "ALL_METHODS")
	if len(allMethods) != len(addMethodCards) {
		t.Fatalf("expected ALL_METHODS to list %d methods, got %d: %v", len(addMethodCards), len(allMethods), allMethods)
	}
	for _, want := range addMethodCards {
		found := false
		for _, m := range allMethods {
			if m == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ALL_METHODS is missing method %q", want)
		}
	}
}

// extractArrayLiteral pulls the single-quoted string entries out of a
// `const <name> = [ '...', '...' ];` declaration in src.
func extractArrayLiteral(t *testing.T, src, name string) []string {
	t.Helper()
	decl := regexp.MustCompile(name + `\s*=\s*\[([^\]]*)\]`)
	m := decl.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("array literal %s not found in app.js", name)
	}
	itemRe := regexp.MustCompile(`'([^']+)'`)
	var out []string
	for _, im := range itemRe.FindAllStringSubmatch(m[1], -1) {
		out = append(out, im[1])
	}
	return out
}

// TestShowModalRoutesEnterpriseSsoToLoginView verifies that selecting the
// enterprise SSO method routes to modalEnterpriseSso, which replaces the shared
// modal body with its login view (so a previously shown method view is swapped
// out).
func TestShowModalRoutesEnterpriseSsoToLoginView(t *testing.T) {
	src := readAppJS(t)

	// showModal dispatches the 'enterprisesso' type to modalEnterpriseSso.
	showModal := extractFunctionBody(t, src, "showModal")
	routeRe := regexp.MustCompile(`type\s*===\s*'enterprisesso'\s*\)\s*modalEnterpriseSso\(`)
	if !routeRe.MatchString(showModal) {
		t.Fatalf("showModal does not route 'enterprisesso' to modalEnterpriseSso; body:\n%s", showModal)
	}

	// modalEnterpriseSso replaces the shared body content (rather than
	// appending), which is what makes the previously shown method view disappear.
	modalSso := extractFunctionBody(t, src, "modalEnterpriseSso")
	if !regexp.MustCompile(`body\.innerHTML\s*=`).MatchString(modalSso) {
		t.Fatalf("modalEnterpriseSso does not overwrite body.innerHTML; body:\n%s", modalSso)
	}

	// The rendered content is the Kiro-hosted SSO login view: it must present the
	// start-login control that is unique to this view.
	if !strings.Contains(modalSso, "startKiroSsoBtn") {
		t.Errorf("modalEnterpriseSso does not render the SSO start-login control (startKiroSsoBtn)")
	}

	// Sanity: the SSO view must not be an accidental copy of another method's
	// view — assert it does not render another method's unique start control.
	if strings.Contains(modalSso, "iamStartBtn") || strings.Contains(modalSso, "builderIdStartBtn") {
		t.Errorf("modalEnterpriseSso unexpectedly contains another method's login controls")
	}
}
