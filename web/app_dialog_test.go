// Feature: microsoft-365-sso
//
// Example tests for MS365 dialog composition in the modern panel (web/app.js).
//
// Kiro-Go is primarily a Go project with no JavaScript test harness (no
// package.json, node_modules, or jest/vitest config). web/app.js is a single
// IIFE that exposes none of its internals, so it cannot be required/executed
// from a unit test without introducing a heavy new toolchain. Consistent with
// how the rest of this repo validates behavior (Go's testing package), these
// example tests assert the dialog-composition contract at the source level:
//
//   - modalAdd renders exactly seven methodCard(...) entries, one of which is
//     the MS365 method (Requirement 1.1).
//   - showModal routes the 'ms365' type to modalMs365, and modalMs365 replaces
//     modalBody.innerHTML with the MS365 login view, so selecting MS365 after
//     another method's view is shown swaps in the MS365 view (Requirement 1.4).
package web

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

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

// TestModalAddRendersSevenMethodCardsIncludingMs365 verifies Requirement 1.1:
// the add dialog offers seven selectable login methods and MS365 is one of them.
func TestModalAddRendersSevenMethodCardsIncludingMs365(t *testing.T) {
	body := extractFunctionBody(t, readAppJS(t), "modalAdd")

	methodCall := regexp.MustCompile(`methodCard\(\s*'([^']+)'`)
	matches := methodCall.FindAllStringSubmatch(body, -1)

	if len(matches) != 7 {
		got := make([]string, 0, len(matches))
		for _, m := range matches {
			got = append(got, m[1])
		}
		t.Fatalf("expected 7 methodCard entries in modalAdd, got %d: %v", len(matches), got)
	}

	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		if seen[m[1]] {
			t.Fatalf("duplicate method card %q in modalAdd", m[1])
		}
		seen[m[1]] = true
	}

	// The six pre-existing methods must remain (Requirement 8.1) alongside MS365.
	for _, want := range []string{"builderid", "iam", "sso", "local", "credentials", "cookie", "ms365"} {
		if !seen[want] {
			t.Errorf("modalAdd is missing method card %q", want)
		}
	}
}

// TestShowModalRoutesMs365ToLoginView verifies Requirement 1.4: selecting the
// MS365 method routes to modalMs365, which replaces the shared modal body with
// the MS365 login view (so a previously shown method view is swapped out).
func TestShowModalRoutesMs365ToLoginView(t *testing.T) {
	src := readAppJS(t)

	// showModal dispatches the 'ms365' type to modalMs365.
	showModal := extractFunctionBody(t, src, "showModal")
	routeRe := regexp.MustCompile(`type\s*===\s*'ms365'\s*\)\s*modalMs365\(`)
	if !routeRe.MatchString(showModal) {
		t.Fatalf("showModal does not route 'ms365' to modalMs365; body:\n%s", showModal)
	}

	// modalMs365 replaces the shared body content (rather than appending),
	// which is what makes the previously shown method view disappear.
	modalMs365 := extractFunctionBody(t, src, "modalMs365")
	if !regexp.MustCompile(`body\.innerHTML\s*=`).MatchString(modalMs365) {
		t.Fatalf("modalMs365 does not overwrite body.innerHTML; body:\n%s", modalMs365)
	}

	// The rendered content is the MS365 login view: it must present the MS365
	// start-login control that is unique to this view.
	if !strings.Contains(modalMs365, "ms365StartBtn") {
		t.Errorf("modalMs365 does not render the MS365 start-login control (ms365StartBtn)")
	}

	// Sanity: the MS365 view must not be an accidental copy of another method's
	// view — assert it does not render another method's unique start control.
	if strings.Contains(modalMs365, "iamStartBtn") || strings.Contains(modalMs365, "builderIdStartBtn") {
		t.Errorf("modalMs365 unexpectedly contains another method's login controls")
	}
}
