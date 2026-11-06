package screenshot

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	screenshotruntime "minfo/internal/screenshot/runtime"
)

func TestPNGDownloadOnlyUsesLosslessOptimization(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test tools use shell scripts")
	}
	for _, tc := range []struct {
		name  string
		size  int64
		lossy bool
	}{
		{name: "small PNG download", size: 1024},
		{name: "large PNG download", size: oversizeBytes + 1},
		{name: "large PNG upload", size: oversizeBytes + 1, lossy: true},
		{name: "small PNG upload", size: 1024, lossy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "shot.png")
			original := bytes.Repeat([]byte{42}, int(tc.size))
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			calls := filepath.Join(dir, "calls")
			t.Setenv("MINFO_TEST_PNG_CALLS", calls)
			oxi := filepath.Join(dir, "oxipng")
			quant := filepath.Join(dir, "pngquant")
			if err := os.WriteFile(oxi, []byte("#!/bin/sh\nprintf 'oxipng\\n' >> \"$MINFO_TEST_PNG_CALLS\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			script := "#!/bin/sh\nprintf 'pngquant\\n' >> \"$MINFO_TEST_PNG_CALLS\"\nwhile [ $# -gt 0 ]; do\nif [ \"$1\" = --output ]; then printf quantized > \"$2\"; exit 0; fi\nshift\ndone\nexit 1\n"
			if err := os.WriteFile(quant, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			runner := &screenshotRunner{
				ctx: context.Background(), allowLossyPNG: tc.lossy,
				tools: screenshotruntime.Toolchain{OxiPNGBin: oxi, PNGQuantBin: quant},
			}
			runner.processPNG(path)
			gotCalls, _ := os.ReadFile(calls)
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.lossy && tc.size > oversizeBytes {
				if string(gotCalls) != "oxipng\npngquant\n" || string(got) != "quantized" || len(runner.lossyPNGFileList()) != 1 {
					t.Fatalf("large upload must retain quantization behavior: calls %q, lossy %v", gotCalls, runner.lossyPNGFileList())
				}
			} else {
				if !bytes.Equal(got, original) || strings.Contains(string(gotCalls), "pngquant") || len(runner.lossyPNGFileList()) != 0 {
					t.Fatalf("PNG was unexpectedly quantized: calls %q", gotCalls)
				}
				if !tc.lossy && string(gotCalls) != "oxipng\n" {
					t.Fatalf("download must run oxipng even below upload limit: %q", gotCalls)
				}
			}
		})
	}
}
