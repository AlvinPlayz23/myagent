package tools

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestShellArgsFor(t *testing.T) {
	cases := map[string][]string{
		`C:\Windows\System32\cmd.exe`:  {"/C"},
		"cmd":                            {"/C"},
		"powershell.exe":                 {"-Command"},
		"pwsh":                           {"-Command"},
		`C:\Program Files\Git\bin\bash.exe`: {"-c"},
		"/bin/sh":                        {"-c"},
	}
	for shell, want := range cases {
		if got := shellArgsFor(shell); !reflect.DeepEqual(got, want) {
			t.Errorf("shellArgsFor(%q) = %v, want %v", shell, got, want)
		}
	}
}

func TestIsWSLStub(t *testing.T) {
	stubs := []string{
		`C:\Windows\System32\bash.exe`,
		`c:\windows\system32\bash.exe`,
		`C:\Users\me\AppData\Local\Microsoft\WindowsApps\bash.exe`,
	}
	for _, p := range stubs {
		if !isWSLStub(p) {
			t.Errorf("isWSLStub(%q) = false, want true", p)
		}
	}
	real := []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
		"/usr/bin/bash",
	}
	for _, p := range real {
		if isWSLStub(p) {
			t.Errorf("isWSLStub(%q) = true, want false", p)
		}
	}
}

func TestShellConfigMyagentShellPrecedence(t *testing.T) {
	t.Setenv("MYAGENT_SHELL", `C:\tools\pwsh.exe`)
	shell, args := shellConfig()
	if shell != `C:\tools\pwsh.exe` {
		t.Errorf("shell = %q, want the MYAGENT_SHELL value", shell)
	}
	if !reflect.DeepEqual(args, []string{"-Command"}) {
		t.Errorf("args = %v, want [-Command] for a pwsh override", args)
	}
}

// clearShellEnv zeroes the env vars findWindowsBash consults so the test only
// sees what it explicitly sets.
func clearShellEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"MYAGENT_SHELL", "PATH", "ProgramFiles", "ProgramFiles(x86)",
		"ProgramW6432", "LOCALAPPDATA",
	} {
		t.Setenv(k, "")
	}
}

func TestFindWindowsBashSkipsSystem32Stub(t *testing.T) {
	clearShellEnv(t)
	// A fake System32 bash.exe on PATH must be ignored (it's the WSL stub).
	sys32 := filepath.Join(t.TempDir(), "System32")
	if err := os.MkdirAll(sys32, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sys32, "bash.exe"), []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", sys32)
	if got := findWindowsBash(); got != "" {
		t.Errorf("findWindowsBash() = %q, want \"\" (System32 stub must be skipped)", got)
	}
}

func TestFindWindowsBashFindsRealBashOnPath(t *testing.T) {
	clearShellEnv(t)
	gitBin := filepath.Join(t.TempDir(), "Git", "bin")
	if err := os.MkdirAll(gitBin, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(gitBin, "bash.exe")
	if err := os.WriteFile(real, []byte("real"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", gitBin)
	if got := findWindowsBash(); got != real {
		t.Errorf("findWindowsBash() = %q, want %q", got, real)
	}
}
