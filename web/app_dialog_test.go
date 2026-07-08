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
//   - modalAdd renders one methodCard(...) entry per supported login method,
//     including the Kiro-hosted enterprise SSO method that replaced the older
//     MS365-specific flow.
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

// readAppJS returns the contents of web/app.js relative to this test's package
// directory.
func readAppJS(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
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
// unexpected.
func TestModalAddRendersAllMethodCards(t *testing.T) {
	body := extractFunctionBody(t, readAppJS(t), "modalAdd")

	methodCall := regexp.MustCompile(`methodCard\(\s*'([^']+)'`)
	matches := methodCall.FindAllStringSubmatch(body, -1)

	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		if seen[m[1]] {
			t.Fatalf("duplicate method card %q in modalAdd", m[1])
		}
		seen[m[1]] = true
	}

	if len(matches) != len(addMethodCards) {
		got := make([]string, 0, len(matches))
		for _, m := range matches {
			got = append(got, m[1])
		}
		t.Fatalf("expected %d methodCard entries in modalAdd, got %d: %v", len(addMethodCards), len(matches), got)
	}

	for _, want := range addMethodCards {
		if !seen[want] {
			t.Errorf("modalAdd is missing method card %q", want)
		}
	}
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
