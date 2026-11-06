package screenshot

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	screenshotdelivery "minfo/internal/screenshot/delivery"
	screenshotruntime "minfo/internal/screenshot/runtime"
)

func TestQuantizedPNGKeepsDownloadableLosslessOriginal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test pngquant executable uses a shell script")
	}
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "shots")
	if err := os.Mkdir(outputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(outputDir, "00h01m02s.png")
	var original, quantized bytes.Buffer
	frame := image.NewRGBA(image.Rect(0, 0, 2, 2))
	frame.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&original, frame); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&quantized, image.NewPaletted(frame.Bounds(), color.Palette{color.Black})); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	quantizedPath := filepath.Join(dir, "quantized.png")
	if err := os.WriteFile(quantizedPath, quantized.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINFO_TEST_QUANTIZED_PNG", quantizedPath)
	tool := filepath.Join(dir, "pngquant")
	script := "#!/bin/sh\nwhile [ $# -gt 0 ]; do\n  if [ \"$1\" = --output ]; then cp \"$MINFO_TEST_QUANTIZED_PNG\" \"$2\"; exit $?; fi\n  shift\ndone\nexit 1\n"
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &screenshotRunner{ctx: context.Background(), tools: screenshotruntime.Toolchain{PNGQuantBin: tool}}
	if err := runner.compressPNGQuant(path); err != nil {
		t.Fatal(err)
	}
	compressed, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(compressed, quantized.Bytes()) {
		t.Fatalf("output PNG was not replaced by quantized PNG: %v", err)
	}
	files, err := screenshotdelivery.ListImageFiles(outputDir)
	if err != nil || len(files) != 1 || files[0] != path {
		t.Fatalf("original backup must not appear in uploads or ZIP: %v, %v", files, err)
	}
	urls, _ := prepareOriginalPNGDownloads(ScreenshotsResult{Files: files, LossyPNGFiles: runner.lossyPNGFileList()}, nil)
	_, token, ok := strings.Cut(urls[filepath.Base(path)], "?token=")
	if !ok {
		t.Fatalf("missing original download URL: %v", urls)
	}
	cachedPath, filename, err := screenshotdelivery.GetOriginalPNG(token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(cachedPath) })
	if err := os.RemoveAll(outputDir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(cachedPath)
	if err != nil || !bytes.Equal(got, original.Bytes()) || filename != filepath.Base(path) {
		t.Fatalf("download must contain pre-quantization PNG: filename %q, error %v", filename, err)
	}
	t.Run("backup failure keeps lossless PNG", func(t *testing.T) {
		blockedDir := t.TempDir()
		blockedPath := filepath.Join(blockedDir, "shot.png")
		if err := os.WriteFile(blockedPath, original.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(blockedDir, ".originals"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		blockedRunner := &screenshotRunner{ctx: context.Background(), tools: screenshotruntime.Toolchain{PNGQuantBin: tool}}
		if err := blockedRunner.compressPNGQuant(blockedPath); err == nil {
			t.Fatal("expected original backup failure")
		}
		got, err := os.ReadFile(blockedPath)
		if err != nil || !bytes.Equal(got, original.Bytes()) || len(blockedRunner.lossyPNGFileList()) != 0 {
			t.Fatalf("failed backup must keep the lossless PNG unchanged: %v", err)
		}
		if _, err := os.Stat(blockedPath + ".quant.png"); !os.IsNotExist(err) {
			t.Fatalf("failed quantization left a partial file: %v", err)
		}
	})
}
