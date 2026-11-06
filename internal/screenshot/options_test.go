package screenshot

import "testing"

func TestNormalizeSizeMode(t *testing.T) {
	for input, want := range map[string]string{
		"": SizeModeAuto, "auto": SizeModeAuto, "invalid": SizeModeAuto,
		"original": SizeModeOriginal, " ORIGINAL ": SizeModeOriginal, "display": SizeModeAuto, " DISPLAY ": SizeModeAuto,
	} {
		if got := NormalizeSizeMode(input); got != want {
			t.Errorf("NormalizeSizeMode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeHDRProcessor(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "default", input: "", want: HDRProcessorLibplacebo},
		{name: "libplacebo", input: "libplacebo", want: HDRProcessorLibplacebo},
		{name: "zscale", input: "zscale", want: HDRProcessorZscale},
		{name: "disabled", input: "off", want: HDRProcessorOff},
		{name: "normalized disabled", input: " OFF ", want: HDRProcessorOff},
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
