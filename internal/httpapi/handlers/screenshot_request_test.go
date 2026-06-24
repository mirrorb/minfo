package handlers

import (
	"net/http"
	"net/url"
	"testing"
)

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
