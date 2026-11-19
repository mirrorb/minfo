package media

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSuggestedPlaylistBDInfoDefaults(t *testing.T) {
	tests := []struct {
		name       string
		items      [][2]uint32
		repeatClip bool
		visible    bool
	}{
		{"below threshold", [][2]uint32{{0, 20*mplsClockRate - 1}}, false, false},
		{"at threshold", [][2]uint32{{0, 20 * mplsClockRate}}, false, true},
		{"sum of items", [][2]uint32{{0, 10 * mplsClockRate}, {0, 10 * mplsClockRate}}, false, true},
		{"loop same clip and entry", [][2]uint32{{0, 30 * mplsClockRate}, {0, 40 * mplsClockRate}}, true, false},
		{"same clip different entry", [][2]uint32{{0, 30 * mplsClockRate}, {30 * mplsClockRate, 60 * mplsClockRate}}, true, true},
		{"different clips same entry", [][2]uint32{{0, 30 * mplsClockRate}, {0, 30 * mplsClockRate}}, false, true},
		{"empty", nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildTestMPLSFile(tt.items)
			if tt.repeatClip {
				copy(data[54:59], data[32:37])
			}
			path := filepath.Join(t.TempDir(), "00001.MpLs")
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, got := suggestedPlaylist(path); got != tt.visible {
				t.Fatalf("visible = %v, want %v", got, tt.visible)
			}
		})
	}
}

func TestPlaylistFilteringBeforeLimitAndSearch(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "BDMV", "PLAYLIST")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, seconds := range map[string]uint32{"00000.mpls": 10, "00001.MPLS": 20, "00002.mpls": 30} {
		if err := os.WriteFile(filepath.Join(dir, name), buildTestMPLSFile([][2]uint32{{0, seconds * mplsClockRate}}), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	items, _, err := SuggestPaths([]string{root}, withDirSuffix(dir), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || filepath.Base(items[0].Path) != "00001.MPLS" || items[0].Duration != "0:00:20" {
		t.Fatalf("unexpected limited result: %+v", items)
	}
	items, _, err = SuggestPaths([]string{root}, filepath.Join(dir, "00000"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("short playlist exposed by search: %+v", items)
	}
	names, err := ListBDInfoPlaylists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"00001.MPLS", "00002.mpls"}) {
		t.Fatalf("scan selection = %v", names)
	}
}

func TestSuggestedPlaylistKeepsUnreadableMetadataAndOtherFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"broken.mpls", "video.mkv"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("invalid"), 0o644); err != nil {
			t.Fatal(err)
		}
		if duration, visible := suggestedPlaylist(path); !visible || duration != "" {
			t.Fatalf("%s: %q %v", name, duration, visible)
		}
	}
	// Truncated item lengths must produce errors, never slice beyond the item.
	data := buildTestMPLSFile([][2]uint32{{0, 30 * mplsClockRate}})
	binary.BigEndian.PutUint16(data[30:32], 1)
	path := filepath.Join(dir, "truncated.mpls")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readMPLSMetadata(path); err == nil {
		t.Fatal("expected truncated-item error")
	}
}

// Set MINFO_TEST_DISC to validate a local disc without storing media in the repo.
func TestBuccaneersGirlPlaylistFiltering(t *testing.T) {
	root := os.Getenv("MINFO_TEST_DISC")
	if root == "" {
		t.Skip("MINFO_TEST_DISC is not set")
	}
	dir := filepath.Join(root, "BDMV", "PLAYLIST")
	want := []string{"00000.mpls", "00004.mpls", "00005.mpls", "00006.mpls", "00007.mpls", "00009.mpls", "00011.mpls"}
	names, err := ListBDInfoPlaylists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("scan playlists = %v, want %v", names, want)
	}
	items, _, err := SuggestPaths([]string{root}, withDirSuffix(dir), 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range items {
		got = append(got, filepath.Base(item.Path))
		t.Logf("%s %s", filepath.Base(item.Path), item.Duration)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("browser playlists = %v, want %v", got, want)
	}
}
