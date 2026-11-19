package bdinfo

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildCommandArgsFiltersDiscPlaylists(t *testing.T) {
	t.Setenv("BDINFO_ARGS", "")
	root := t.TempDir()
	dir := filepath.Join(root, "BDMV", "PLAYLIST")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, seconds := range map[string]uint32{"00001.mpls": 10, "00002.mpls": 20} {
		data := make([]byte, 52)
		copy(data, "MPLS0100")
		binary.BigEndian.PutUint32(data[8:12], 20)
		binary.BigEndian.PutUint16(data[26:28], 1)
		binary.BigEndian.PutUint16(data[30:32], 20)
		copy(data[32:41], "00001M2TS")
		binary.BigEndian.PutUint32(data[48:52], seconds*45000)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []string{root, filepath.Dir(dir)} {
		args, err := buildCommandArgs(input, "report", "")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"-m", "00002.mpls", input, "report"}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("args = %v, want %v", args, want)
		}
	}
	args, err := buildCommandArgs(root, "report", "00001.mpls")
	if err != nil {
		t.Fatal(err)
	}
	if args[1] != "00001.mpls" {
		t.Fatalf("explicit selection lost: %v", args)
	}
	t.Setenv("BDINFO_ARGS", "-w")
	args, err = buildCommandArgs(root, "report", "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, []string{"-w", root, "report"}) {
		t.Fatalf("explicit whole-disc lost: %v", args)
	}
	t.Setenv("BDINFO_ARGS", "")
	if err := os.Remove(filepath.Join(dir, "00002.mpls")); err != nil {
		t.Fatal(err)
	}
	if _, err := buildCommandArgs(root, "report", ""); err == nil {
		t.Fatal("empty filtered selection must not fall back to scanning all playlists")
	}
}
