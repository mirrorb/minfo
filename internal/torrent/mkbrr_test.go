package torrent

import (
	"reflect"
	"testing"
)

func TestPieceLengthExponent(t *testing.T) {
	exp, err := PieceLengthExponent(4 << 20)
	if err != nil {
		t.Fatal(err)
	}
	if exp != 22 {
		t.Fatalf("exp = %d, want 22", exp)
	}
}

func TestBuildMkbrrArgs(t *testing.T) {
	args, err := BuildMkbrrArgs("/media/Movie", "/tmp/output.torrent", Options{
		Format:      "v1",
		PieceLength: 4 << 20,
		Private:     true,
		Trackers:    []string{"https://tracker.example/announce"},
		WebSeeds:    []string{"https://seed.example/file"},
		Comment:     "hello",
		Source:      "PT",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"create", "/media/Movie",
		"--output", "/tmp/output.torrent",
		"--skip-prefix",
		"--piece-length", "22",
		"--private=true",
		"--tracker", "https://tracker.example/announce",
		"--web-seed", "https://seed.example/file",
		"--comment", "hello",
		"--source", "PT",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestParseProgressLine(t *testing.T) {
	progress, ok := ParseProgressLine("\x1b[36mHashing pieces...\x1b[0m [3.6 GiB/s]  92% [====>]")
	if !ok {
		t.Fatal("expected progress")
	}
	if progress.Percent != 92 {
		t.Fatalf("percent = %v, want 92", progress.Percent)
	}
	if progress.Stage == "" || progress.Detail == "" {
		t.Fatalf("expected stage and detail: %#v", progress)
	}
}
