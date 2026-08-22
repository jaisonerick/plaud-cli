package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestTheDeclarationIsFoundFromAnywhereBelowIt(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, FileName), `{"context": "docs/briefing.md"}`)
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}

	c, err := Find(deep)
	if err != nil {
		t.Fatal(err)
	}

	if c.Root != root {
		t.Errorf("root came back as %s, not %s", c.Root, root)
	}
	if c.Context != filepath.Join(root, "docs", "briefing.md") {
		t.Errorf("the context was resolved to %s", c.Context)
	}
}

func TestTheNearestDeclarationWins(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "client")
	write(t, filepath.Join(root, FileName), `{"filing": "outer.md"}`)
	write(t, filepath.Join(inner, FileName), `{"filing": "inner.md"}`)

	c, err := Find(inner)
	if err != nil {
		t.Fatal(err)
	}

	if c.Filing != "inner.md" {
		t.Errorf("a directory with its own declaration was governed by %q", c.Filing)
	}
}

func TestADirectoryDeclaringNothingIsNotAnError(t *testing.T) {
	c, err := Find(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.Declares() {
		t.Error("a directory with no file claimed to declare one")
	}
	if c.KeepsCatalog() {
		t.Error("a directory with no file claimed a catalog")
	}
}

func TestAKeyNobodyReadsIsReported(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, FileName), `{"filing": "f.md", "contexto": "x.md", "hubb": "y"}`)

	c, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(c.Unknown, ",") != "contexto,hubb" {
		t.Errorf("the misspelt keys came back as %v", c.Unknown)
	}
}

func TestBadJSONSaysWhichFile(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, FileName), `{"filing":`)

	_, err := Find(root)
	if err == nil || !strings.Contains(err.Error(), FileName) {
		t.Errorf("a broken file failed with %v", err)
	}
}
