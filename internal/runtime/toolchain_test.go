package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestToolchainVerifyEnforcesObservedVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "terraform.cmd"), []byte("@echo off\necho Terraform v9.9.9\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+old); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Setenv("PATH", old) }()
	toolchain := Toolchain{Lock: ToolchainLock{Terraform: ToolVersion{Version: "1.14.0"}}}
	if actual, err := toolchain.Verify("terraform", false); actual != "9.9.9" || err == nil {
		t.Fatalf("version mismatch was not rejected: %q %v", actual, err)
	}
	if actual, err := toolchain.Verify("terraform", true); actual != "9.9.9" || err != nil {
		t.Fatalf("override did not permit mismatch: %q %v", actual, err)
	}
	if _, err := toolchain.Verify("tflint", false); err == nil {
		t.Fatal("missing executable was not rejected")
	}
	if err := os.WriteFile(filepath.Join(dir, "terraform.cmd"), []byte("@echo off\necho not-a-version\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := toolchain.Verify("terraform", false); err == nil {
		t.Fatal("malformed version output was not rejected")
	}
}
