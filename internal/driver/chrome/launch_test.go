package chrome

import (
	"testing"

	"github.com/chromedp/cdproto/runtime"
)

func TestSecurityOrigin(t *testing.T) {
	cases := map[string]string{
		"http://localhost:8088":           "http://localhost:8088",
		"http://localhost:5173/":          "http://localhost:5173",
		"https://app.example.com/a/b?c=d": "https://app.example.com",
		"data:text/html,<body>hi</body>":  "",
		"file:///tmp/index.html":          "",
		"about:blank":                     "",
		"":                                "",
		"://not a url":                    "",
	}
	for target, want := range cases {
		if got := securityOrigin(target); got != want {
			t.Errorf("securityOrigin(%q) = %q, want %q", target, got, want)
		}
	}
}

// TestExceptionMessage_PrefersDescription pins the useful half of a page
// exception. ExceptionDetails.Text is "Uncaught" for every throw; the
// description is where the error a caller can act on lives.
func TestExceptionMessage_PrefersDescription(t *testing.T) {
	withDescription := &runtime.ExceptionDetails{
		Text: "Uncaught",
		Exception: &runtime.RemoteObject{
			Description: "SecurityError: Access is denied for this document.",
		},
	}
	if got := exceptionMessage(withDescription); got != withDescription.Exception.Description {
		t.Errorf("exceptionMessage = %q, want the description", got)
	}
	textOnly := &runtime.ExceptionDetails{Text: "Uncaught SyntaxError"}
	if got := exceptionMessage(textOnly); got != "Uncaught SyntaxError" {
		t.Errorf("exceptionMessage = %q, want the text fallback", got)
	}
	if got := exceptionMessage(nil); got != "" {
		t.Errorf("exceptionMessage(nil) = %q, want empty", got)
	}
}
