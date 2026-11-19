package bdinfo

import (
	"strings"
	"testing"
	"time"
)

func TestSelectLongestPlaylistBlock(t *testing.T) {
	tests := []struct {
		name   string
		first  string
		second string
		want   string
	}{
		{"duration before size", "Length: 0:30:00.000\nSize: 9,000 bytes", "Length: 1:30:00.000\nSize: 1,000 bytes", "00002.MPLS"},
		{"longest first", "Length: 1:30:00.000\nSize: 1,000 bytes", "Length: 0:30:00.000\nSize: 9,000 bytes", "00001.MPLS"},
		{"fractional duration", "Length: 1:30:00.001\nSize: 9,000 bytes", "Length: 1:30:00.002\nSize: 1,000 bytes", "00002.MPLS"},
		{"size breaks duration tie", "Length: 1:30:00.000\nSize: 1,000 bytes", "Length: 1:30:00.000\nSize: 9,000 bytes", "00002.MPLS"},
		{"stable exact tie", "Length: 1:30:00\nSize: 1,000 bytes", "Length: 1:30:00\nSize: 1,000 bytes", "00001.MPLS"},
		{"ignore angle totals", "Length: 0:30:00.000\nAll Angles Length: 3:00:00.000\nSize: 9,000 bytes", "Length: 1:30:00.000\nSize: 1,000 bytes", "00002.MPLS"},
		{"missing length falls back to size", "Size: 1,000 bytes", "Size: 9,000 bytes", "00002.MPLS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix := "DISC INFO:\nDisc Title: Test\n\n"
			report := prefix + "PLAYLIST: 00001.MPLS\n" + tt.first + "\nPLAYLIST: 00002.MPLS\n" + tt.second + "\n"
			got := SelectLongestPlaylistBlock(strings.ReplaceAll(report, "\n", "\r\n"))
			if !strings.HasPrefix(got, prefix) || !strings.Contains(got, "PLAYLIST: "+tt.want) || strings.Count(got, "PLAYLIST:") != 1 {
				t.Fatalf("unexpected selected report:\n%s", got)
			}
		})
	}
}

func TestSelectLongestPlaylistBlockWithoutMultiplePlaylists(t *testing.T) {
	for _, report := range []string{"", "DISC INFO:\nTest", "DISC INFO:\nPLAYLIST: 00000.MPLS\nLength: 1:16:53.025\nSize: 1,000 bytes\n"} {
		if got := SelectLongestPlaylistBlock(report); got != report {
			t.Fatalf("single or absent playlist changed: %q", got)
		}
	}
}

func TestParsePlaylistLength(t *testing.T) {
	tests := []struct {
		line string
		want time.Duration
	}{
		{"Length:                 1:16:53.025 (h:m:s.ms)", time.Hour + 16*time.Minute + 53*time.Second + 25*time.Millisecond},
		{" Length: 12:00:01 ", 12*time.Hour + time.Second},
		{"Length: 0:00:00.1", 100 * time.Millisecond},
		{"Length: 0:00:00.000000001", time.Nanosecond},
		{"All Angles Length: 3:00:00.000", 0},
		{"Length: 1:99:00", 0},
		{"Length: 1:00:60", 0},
		{"Length: 1:00:00.bad", 0},
		{"Length: invalid", 0},
		{"Length: 999999999999999999999:00:00", 0},
	}
	for _, tt := range tests {
		if got := parsePlaylistLength(tt.line); got != tt.want {
			t.Errorf("parsePlaylistLength(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}
