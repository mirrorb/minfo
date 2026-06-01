package handlers

import "testing"

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
