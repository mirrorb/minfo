package pixhost

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildUploadedImageIncludesThumbnailAndOriginalMetadata(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "shot.png")
	file, err := os.Create(imagePath)
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		_ = file.Close()
		t.Fatalf("encode image: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close image: %v", err)
	}

	item := buildUploadedImage(imagePath, "https://img1.pixhost.to/images/1/shot.png", "https://t1.pixhost.to/thumbs/1/shot.png")

	if item.URL != "https://img1.pixhost.to/images/1/shot.png" {
		t.Fatalf("URL = %q", item.URL)
	}
	if item.ThumbnailURL != "https://t1.pixhost.to/thumbs/1/shot.png" {
		t.Fatalf("ThumbnailURL = %q", item.ThumbnailURL)
	}
	if item.Filename != "shot.png" {
		t.Fatalf("Filename = %q", item.Filename)
	}
	if item.Size <= 0 {
		t.Fatalf("Size = %d", item.Size)
	}
	if item.Width != 3 || item.Height != 2 {
		t.Fatalf("dimensions = %dx%d", item.Width, item.Height)
	}
}
