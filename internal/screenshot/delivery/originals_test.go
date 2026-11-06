package delivery

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOriginalPNGSurvivesTaskCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "00h01m02s.png")
	original := []byte("unmodified lossless PNG data")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PreserveOriginalPNG(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("lossy PNG data"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := SaveOriginalPNG(OriginalPNGPath(path))
	if err != nil {
		t.Fatal(err)
	}
	cachedPath, filename, err := GetOriginalPNG(token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		originalPNGs.Lock()
		delete(originalPNGs.items, token)
		originalPNGs.Unlock()
		_ = os.Remove(cachedPath)
	})
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(cachedPath)
	if err != nil || !bytes.Equal(got, original) || filename != "00h01m02s.png" {
		t.Fatalf("cached original = %q, filename = %q, error = %v", got, filename, err)
	}
}

func TestOriginalPNGExpiresAndRemovesCachedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := SaveOriginalPNG(path)
	if err != nil {
		t.Fatal(err)
	}
	cachedPath, _, err := GetOriginalPNG(token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(cachedPath) })
	originalPNGs.Lock()
	item := originalPNGs.items[token]
	item.expiresAt = time.Now().Add(-time.Second)
	originalPNGs.items[token] = item
	originalPNGs.Unlock()
	if _, _, err := GetOriginalPNG(token); err == nil {
		t.Fatal("expired original must not be downloadable")
	}
	if _, err := os.Stat(cachedPath); !os.IsNotExist(err) {
		t.Fatalf("expired original was not removed: %v", err)
	}
}

func TestDeleteOriginalPNGOnlyRemovesRequestedCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := SaveOriginalPNG(path)
	if err != nil {
		t.Fatal(err)
	}
	other, err := SaveOriginalPNG(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = DeleteOriginalPNG(token); _ = DeleteOriginalPNG(other) })
	cachedPath, _, err := GetOriginalPNG(token)
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteOriginalPNG(token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := GetOriginalPNG(token); err == nil {
		t.Fatal("deleted original remains downloadable")
	}
	if _, err := os.Stat(cachedPath); !os.IsNotExist(err) {
		t.Fatalf("deleted original still exists on disk: %v", err)
	}
	if err := DeleteOriginalPNG(token); err != nil {
		t.Fatalf("repeat deletion must succeed: %v", err)
	}
	if _, _, err := GetOriginalPNG(other); err != nil {
		t.Fatalf("deleting one token removed another original: %v", err)
	}
}
