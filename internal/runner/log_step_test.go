package runner

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/priyanshujain/sanderling/internal/verifier"
)

func logStepLine(t *testing.T, action verifier.Action, treeJSON string) string {
	t.Helper()
	var buffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buffer, nil))
	logStep(logger, 7, "LoginScreen", 42, action, nil, "", mustParseTree(t, treeJSON))
	return buffer.String()
}

func TestLogStepNamesTheActionItsTargetAndTheTypedValue(t *testing.T) {
	line := logStepLine(t, typeInto("id:LoginEmail"), iosLoginTreeJSON)

	for _, want := range []string{
		"index=7", "screen=LoginScreen", "nodes=42",
		"action=InputText", "target=id:LoginEmail", typedCredential,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("step log = %q, want it to carry %q", line, want)
		}
	}
}

func TestLogStepRedactsTheTypedValueOfASecureField(t *testing.T) {
	line := logStepLine(t, typeInto("id:LoginPassword"), iosLoginTreeJSON)

	if strings.Contains(line, typedCredential) {
		t.Errorf("step log = %q, want the typed credential withheld", line)
	}
	if !strings.Contains(line, verifier.RedactedInputText) {
		t.Errorf("step log = %q, want %q", line, verifier.RedactedInputText)
	}
	if !strings.Contains(line, "target=id:LoginPassword") {
		t.Errorf("step log = %q, want it to still name the field typed into", line)
	}
}

func TestLogStepReportsAStepThatActedOnNothing(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buffer, nil))
	logStep(logger, 3, "", 0, verifier.Action{}, verifier.ErrNoAction, actionSkippedNoActionProduced, nil)

	line := buffer.String()
	if !strings.Contains(line, "action=none") {
		t.Errorf("step log = %q, want action=none", line)
	}
	if !strings.Contains(line, "skipped="+string(actionSkippedNoActionProduced)) {
		t.Errorf("step log = %q, want the skip reason", line)
	}
}
