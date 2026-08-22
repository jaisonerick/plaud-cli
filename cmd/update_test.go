package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewerThanReadsDottedReleases(t *testing.T) {
	for _, c := range []struct {
		release, than string
		want          bool
	}{
		{"0.16.0", "0.15.0", true},
		{"0.15.1", "0.15.0", true},
		{"1.0.0", "0.15.0", true},
		{"0.13.0", "0.15.0", false},
		{"0.15.0", "0.15.0", false},
		{"", "0.15.0", false},
		{"0.9.0", "0.10.0", false},
	} {
		if got := newerThan(c.release, c.than); got != c.want {
			t.Errorf("newerThan(%q, %q) = %v, want %v", c.release, c.than, got, c.want)
		}
	}
}

func TestReplaceBinaryTakesTheDirectRenameWhenItWorks(t *testing.T) {
	dir := t.TempDir()
	exec, tmp := filepath.Join(dir, "plaud"), filepath.Join(dir, "plaud-new")
	if err := os.WriteFile(exec, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := replaceBinary(exec, tmp, os.Rename); err != nil {
		t.Fatalf("replaceBinary errored: %v", err)
	}

	if got, _ := os.ReadFile(exec); string(got) != "new" {
		t.Errorf("the binary in place is %q", got)
	}
	if _, err := os.Stat(oldBinaryPath(exec)); err == nil {
		t.Error("nothing was running, so nothing should have been moved aside")
	}
}

// Windows will not write over a running binary, and this is what an update
// does there instead: the running one is renamed, which frees the name.
func TestTheRunningBinaryIsMovedAsideSoTheNewOneCanTakeItsName(t *testing.T) {
	dir := t.TempDir()
	exec, tmp := filepath.Join(dir, "plaud.exe"), filepath.Join(dir, "plaud-new.exe")
	os.WriteFile(exec, []byte("old"), 0755)
	os.WriteFile(tmp, []byte("new"), 0755)

	if err := replaceByMovingAside(exec, tmp, os.Rename); err != nil {
		t.Fatalf("replaceByMovingAside errored: %v", err)
	}

	if got, _ := os.ReadFile(exec); string(got) != "new" {
		t.Errorf("the binary that runs is %q", got)
	}
	if got, _ := os.ReadFile(oldBinaryPath(exec)); string(got) != "old" {
		t.Errorf("the one moved aside is %q", got)
	}
}

func TestAFailedUpdateLeavesTheOldBinaryRunnable(t *testing.T) {
	dir := t.TempDir()
	exec, tmp := filepath.Join(dir, "plaud.exe"), filepath.Join(dir, "plaud-new.exe")
	os.WriteFile(exec, []byte("old"), 0755)

	// The new binary never arrives: what must not happen is ending with no
	// binary at all where one was running.
	refuseTheNewOne := func(from, to string) error {
		if from == tmp {
			return os.ErrPermission
		}
		return os.Rename(from, to)
	}

	if err := replaceByMovingAside(exec, tmp, refuseTheNewOne); err == nil {
		t.Fatal("a failed replacement reported success")
	}
	if got, _ := os.ReadFile(exec); string(got) != "old" {
		t.Errorf("the binary that runs is %q, and should be the old one back", got)
	}
}

func TestSweepOldBinaryDropsWhatAnUpdateLeftBehind(t *testing.T) {
	dir := t.TempDir()
	exec := filepath.Join(dir, "plaud")
	aside := oldBinaryPath(exec)
	if err := os.WriteFile(aside, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	sweepOldBinaryAt(exec)

	if _, err := os.Stat(aside); err == nil {
		t.Error("the binary from the update before is still there")
	}
}
