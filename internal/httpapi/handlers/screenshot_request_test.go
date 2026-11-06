package handlers

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScreenshotSizeModeFormAndQuery(t *testing.T) {
	for input, want := range map[string]string{
		"": "auto", "invalid": "auto", "auto": "auto", "original": "original", "display": "auto",
	} {
		t.Run(input, func(t *testing.T) {
			values := url.Values{"size_mode": {input}}
			form, err := http.NewRequest(http.MethodPost, "/api/screenshots", strings.NewReader(values.Encode()))
			if err != nil {
				t.Fatal(err)
			}
			form.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			query, err := http.NewRequest(http.MethodGet, "/api/screenshots?"+values.Encode(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := normalizeScreenshotFormOptions(form).SizeMode; got != want {
				t.Errorf("form size mode = %q, want %q", got, want)
			}
			if got := normalizeScreenshotQueryOptions(query).SizeMode; got != want {
				t.Errorf("query size mode = %q, want %q", got, want)
			}
		})
	}
}

func TestParseScreenshotRequestKeepsSizeMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.mkv")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	values := url.Values{"path": {path}, "size_mode": {"original"}, "hdr_processor": {"off"}, "timestamp": {"00:01:02"}}
	request, err := http.NewRequest(http.MethodPost, "/api/screenshot-jobs", strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	parsed, err := parseScreenshotFormRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	defer parsed.Cleanup()
	if parsed.SizeMode != "original" || parsed.HDRProcessor != "off" || parsed.Count != 1 || len(parsed.Timestamps) != 1 {
		t.Fatalf("unexpected screenshot request: %#v", parsed)
	}
}

func TestNormalizeProxyURLAllowsEmptyValue(t *testing.T) {
	proxyURL, err := normalizeProxyURL(" ")
	if err != nil {
		t.Fatalf("normalizeProxyURL returned error: %v", err)
	}
	if proxyURL != "" {
		t.Fatalf("proxyURL = %q, want empty", proxyURL)
	}
}

func TestNormalizeProxyURLAllowsHTTPProxy(t *testing.T) {
	proxyURL, err := normalizeProxyURL(" http://host.docker.internal:7890 ")
	if err != nil {
		t.Fatalf("normalizeProxyURL returned error: %v", err)
	}
	if proxyURL != "http://host.docker.internal:7890" {
		t.Fatalf("proxyURL = %q, want normalized HTTP proxy URL", proxyURL)
	}
}

func TestNormalizeProxyURLRejectsUnsupportedScheme(t *testing.T) {
	if _, err := normalizeProxyURL("ftp://127.0.0.1:7890"); err == nil {
		t.Fatal("expected unsupported proxy scheme error")
	}
}

func TestNormalizePixhostDomainAllowsEmptyValue(t *testing.T) {
	domain, err := normalizePixhostDomain(" ")
	if err != nil {
		t.Fatalf("normalizePixhostDomain returned error: %v", err)
	}
	if domain != "" {
		t.Fatalf("domain = %q, want empty", domain)
	}
}

func TestNormalizePixhostDomainAllowsPixhostCC(t *testing.T) {
	domain, err := normalizePixhostDomain(" PIXHOST.CC ")
	if err != nil {
		t.Fatalf("normalizePixhostDomain returned error: %v", err)
	}
	if domain != "pixhost.cc" {
		t.Fatalf("domain = %q, want pixhost.cc", domain)
	}
}

func TestNormalizePixhostDomainRejectsUnsupportedDomain(t *testing.T) {
	if _, err := normalizePixhostDomain("pixhost.example"); err == nil {
		t.Fatal("expected unsupported pixhost domain error")
	}
}

func TestNormalizeScreenshotFormTimestampsAllowsRepeatedTimestampFields(t *testing.T) {
	request := &http.Request{Form: url.Values{
		"timestamp": {"00:01:02", "01:02:03"},
	}}

	timestamps, err := normalizeScreenshotFormTimestamps(request)
	if err != nil {
		t.Fatalf("normalizeScreenshotFormTimestamps returned error: %v", err)
	}
	if len(timestamps) != 2 || timestamps[0] != "00:01:02" || timestamps[1] != "01:02:03" {
		t.Fatalf("timestamps = %#v", timestamps)
	}
}

func TestNormalizeScreenshotFormTimestampsAllowsDelimitedList(t *testing.T) {
	request := &http.Request{Form: url.Values{
		"timestamps": {"00:01:02,01:02:03\n02:03:04"},
	}}

	timestamps, err := normalizeScreenshotFormTimestamps(request)
	if err != nil {
		t.Fatalf("normalizeScreenshotFormTimestamps returned error: %v", err)
	}
	if len(timestamps) != 3 || timestamps[2] != "02:03:04" {
		t.Fatalf("timestamps = %#v", timestamps)
	}
}

func TestNormalizeScreenshotFormTimestampsRejectsInvalidValue(t *testing.T) {
	request := &http.Request{Form: url.Values{
		"timestamp": {"00h01m02s"},
	}}

	if _, err := normalizeScreenshotFormTimestamps(request); err == nil {
		t.Fatal("expected invalid timestamp error")
	}
}

func TestNormalizeScreenshotFormTimestampsRejectsTooManyValues(t *testing.T) {
	request := &http.Request{Form: url.Values{
		"timestamp": {
			"00:00:01",
			"00:00:02",
			"00:00:03",
			"00:00:04",
			"00:00:05",
			"00:00:06",
			"00:00:07",
			"00:00:08",
			"00:00:09",
			"00:00:10",
			"00:00:11",
		},
	}}

	if _, err := normalizeScreenshotFormTimestamps(request); err == nil {
		t.Fatal("expected too many timestamps error")
	}
}
