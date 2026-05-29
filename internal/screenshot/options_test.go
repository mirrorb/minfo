package screenshot

import "testing"

func TestNormalizeHDRProcessor(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "default", input: "", want: HDRProcessorLibplacebo},
		{name: "libplacebo", input: "libplacebo", want: HDRProcessorLibplacebo},
		{name: "zscale", input: "zscale", want: HDRProcessorZscale},
		{name: "unknown", input: "foo", want: HDRProcessorLibplacebo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeHDRProcessor(tt.input); got != tt.want {
				t.Fatalf("NormalizeHDRProcessor(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
