package run

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSudoNoPasswdOKUsesFilteredDefaultEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sudo executable uses a POSIX shell script")
	}

	dir := t.TempDir()
	sudo := filepath.Join(dir, "sudo")
	script := "#!/bin/sh\n" +
		"test -z \"${DEPENGINE_TEST_OMITTED+x}\" && " +
		"test \"$DEPENGINE_TRACE_ID\" = \"session-trace-test\"\n"
	if err := os.WriteFile(sudo, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	const omittedName = "DEPENGINE_TEST_OMITTED"
	t.Setenv(omittedName, "must-remain-in-parent")
	t.Setenv("DEPENGINE_TRACE_ID", "session-trace-test")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx := WithOmittedEnv(context.Background(), omittedName)
	if !sudoNoPasswdOK(ctx) {
		t.Fatal("sudo probe did not receive the filtered environment and trace id")
	}
	if got := os.Getenv(omittedName); got != "must-remain-in-parent" {
		t.Fatalf("parent environment value = %q, want unchanged value", got)
	}
}
