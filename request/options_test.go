package request

import "testing"

func TestContentType_SetsHeader(t *testing.T) {
	p := &Params{}
	opt := ContentType(JSON)
	p = opt(p)

	if p.Headers == nil {
		t.Fatal("expected headers to be initialized")
	}
	if p.Headers["Content-Type"] != "application/json" {
		t.Fatalf("expected application/json, got %s", p.Headers["Content-Type"])
	}
}

func TestContentType_Multipart(t *testing.T) {
	p := &Params{Headers: map[string]string{"X-Existing": "yes"}}
	p = ContentType(MULTIPART)(p)

	if p.Headers["Content-Type"] != "multipart/form-data" {
		t.Fatalf("expected multipart/form-data, got %s", p.Headers["Content-Type"])
	}
	if p.Headers["X-Existing"] != "yes" {
		t.Fatal("existing headers should be preserved")
	}
}

func TestContentType_CustomValue(t *testing.T) {
	p := &Params{}
	p = ContentType(RequestContentType("text/plain"))(p)
	if p.Headers["Content-Type"] != "text/plain" {
		t.Fatalf("expected text/plain, got %s", p.Headers["Content-Type"])
	}
}

func TestContentType_OverridesPrevious(t *testing.T) {
	p := &Params{Headers: map[string]string{"Content-Type": "old"}}
	p = ContentType(JSON)(p)
	if p.Headers["Content-Type"] != "application/json" {
		t.Fatalf("expected override to application/json, got %s", p.Headers["Content-Type"])
	}
}

func TestMultipleOptions(t *testing.T) {
	addHeader := func(key, value string) RequestOption {
		return func(p *Params) *Params {
			if p.Headers == nil {
				p.Headers = make(map[string]string)
			}
			p.Headers[key] = value
			return p
		}
	}

	p := &Params{}
	opts := []RequestOption{
		ContentType(JSON),
		addHeader("X-Request-ID", "123"),
		addHeader("Authorization", "Bearer tok"),
	}
	for _, opt := range opts {
		p = opt(p)
	}

	if p.Headers["Content-Type"] != "application/json" {
		t.Fatal("content-type not set")
	}
	if p.Headers["X-Request-ID"] != "123" {
		t.Fatal("X-Request-ID not set")
	}
	if p.Headers["Authorization"] != "Bearer tok" {
		t.Fatal("Authorization not set")
	}
}