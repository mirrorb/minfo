package handlers

import (
	"bytes"
	"image"
	"image/png"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"minfo/internal/httpapi/transport"
	"minfo/internal/screenshot"
	screenshotdelivery "minfo/internal/screenshot/delivery"
)

func TestScreenshotOriginalDownload(t *testing.T) {
	var original bytes.Buffer
	if err := png.Encode(&original, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "00h01m02s.png")
	if err := os.WriteFile(path, original.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := screenshotdelivery.SaveOriginalPNG(path)
	if err != nil {
		t.Fatal(err)
	}
	cachedPath, _, err := screenshotdelivery.GetOriginalPNG(token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(cachedPath) })
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := httptest.NewRecorder()
		ScreenshotOriginalHandler(response, httptest.NewRequest(method, "/api/screenshot-original?token="+token, nil))
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("download returned %d, %v", response.Code, response.Header())
		}
		kind, params, err := mime.ParseMediaType(response.Header().Get("Content-Disposition"))
		if err != nil || kind != "attachment" || params["filename"] != filepath.Base(path) {
			t.Fatalf("incorrect download filename: %v, %v", params, err)
		}
		if method == http.MethodGet && !bytes.Equal(response.Body.Bytes(), original.Bytes()) {
			t.Fatal("downloaded bytes differ from original PNG")
		}
	}
	for method, want := range map[string]int{http.MethodGet: http.StatusNotFound, http.MethodPost: http.StatusMethodNotAllowed} {
		response := httptest.NewRecorder()
		ScreenshotOriginalHandler(response, httptest.NewRequest(method, "/api/screenshot-original?token=unknown", nil))
		if response.Code != want {
			t.Fatalf("invalid download status = %d, want %d", response.Code, want)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		response := httptest.NewRecorder()
		ScreenshotOriginalHandler(response, httptest.NewRequest(http.MethodDelete, "/api/screenshot-original?token="+token, nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("delete status = %d, want 204", response.Code)
		}
	}
	response := httptest.NewRecorder()
	ScreenshotOriginalHandler(response, httptest.NewRequest(http.MethodGet, "/api/screenshot-original?token="+token, nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("deleted original must return 404, got %d", response.Code)
	}
	if _, err := os.Stat(cachedPath); !os.IsNotExist(err) {
		t.Fatalf("deleted original still exists on disk: %v", err)
	}
}

func TestOriginalDownloadURLReachesJobSnapshots(t *testing.T) {
	url := "/api/screenshot-original?token=example"
	item := buildTransportImageLinkItem(screenshot.UploadedImage{URL: "https://example.com/shot.png", OriginalDownloadURL: url})
	job := &screenshotJob{linkItems: []transport.ImageLinkItem{item}}
	if got := job.snapshot().LinkItems[0].OriginalDownloadURL; got != url {
		t.Fatalf("original download URL lost in snapshot: %q", got)
	}
}
