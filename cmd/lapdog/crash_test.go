package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCrashOutputCapturesUnhandledPanic(t *testing.T) {
	const childPathEnv = "LAPDOG_TEST_CRASH_PATH"
	if path := os.Getenv(childPathEnv); path != "" {
		if _, err := installCrashOutput(path); err != nil {
			panic(err)
		}
		panic("lapdog crash output test")
	}

	path := t.TempDir() + "/lapdog-crash.log"
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashOutputCapturesUnhandledPanic$")
	cmd.Env = append(os.Environ(), childPathEnv+"="+path)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("panic subprocess succeeded; output: %s", output)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"panic: lapdog crash output test", "TestCrashOutputCapturesUnhandledPanic"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("crash report missing %q: %s", want, body)
		}
	}
}
