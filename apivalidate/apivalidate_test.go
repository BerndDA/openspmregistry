package apivalidate

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

const specPath = "../openapi/registry.openapi.yaml"

// captureLogs redirects the default slog logger to a buffer for the duration of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func Test_Load_ValidSpec_Succeeds(t *testing.T) {
	if _, err := Load(specPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func Test_Load_MissingFile_ReturnsError(t *testing.T) {
	if _, err := Load("does-not-exist.yaml"); err == nil {
		t.Error("expected error for missing spec file")
	}
}

func Test_Check_ConformantResponse_NoWarning(t *testing.T) {
	v, err := Load(specPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	buf := captureLogs(t)

	req, _ := http.NewRequest("GET", "http://127.0.0.1:8080/example/SamplePackage", nil)
	req.Header.Set("Accept", "application/vnd.swift.registry.v1+json")
	resp := &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type":    []string{"application/json"},
			"Content-Version": []string{"1"},
		},
	}
	body := []byte(`{"releases":{"1.0.0":{"url":"http://127.0.0.1:8080/example/SamplePackage/1.0.0"}}}`)

	v.Check(req, nil, resp, body)

	if buf.Len() != 0 {
		t.Errorf("expected no warnings for a conformant response, got: %s", buf.String())
	}
}

func Test_Check_MissingRequiredHeader_LogsWarning(t *testing.T) {
	v, err := Load(specPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	buf := captureLogs(t)

	req, _ := http.NewRequest("GET", "http://127.0.0.1:8080/example/SamplePackage", nil)
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}}, // Content-Version missing
	}
	body := []byte(`{"releases":{}}`)

	v.Check(req, nil, resp, body)

	if !strings.Contains(buf.String(), "does not conform") {
		t.Errorf("expected a conformance warning, got: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "Content-Version") {
		t.Errorf("expected warning to mention the missing header, got: %s", buf.String())
	}
}

func Test_Check_PathOutsideSpec_SilentlySkipped(t *testing.T) {
	v, err := Load(specPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	buf := captureLogs(t)

	// /collection is this server's own extension, not part of the official registry spec.
	req, _ := http.NewRequest("GET", "http://127.0.0.1:8080/collection", nil)
	resp := &http.Response{StatusCode: 200, Header: http.Header{}}

	v.Check(req, nil, resp, []byte(`not even valid json for this path`))

	if buf.Len() != 0 {
		t.Errorf("expected no warnings for a path outside the spec, got: %s", buf.String())
	}
}

// Test_Check_CollectionScopePath_SilentlySkipped guards against a real false positive found while
// wiring this up: "/collection/{scope}" structurally collides with the spec's "/{scope}/{name}"
// template (scope="collection", name="{scope}"), so without an explicit exclusion the router
// misidentifies collection responses as release-list responses and flags them for missing fields
// (e.g. the Content-Version header) that were never meant to apply to this extension endpoint.
func Test_Check_CollectionScopePath_SilentlySkipped(t *testing.T) {
	v, err := Load(specPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	buf := captureLogs(t)

	req, _ := http.NewRequest("GET", "http://127.0.0.1:8080/collection/example", nil)
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}}

	v.Check(req, nil, resp, []byte(`{"name":"example Packages","packages":[]}`))

	if buf.Len() != 0 {
		t.Errorf("expected no warnings for /collection/{scope}, got: %s", buf.String())
	}
}

func Test_RoundTrip_PreservesResponseBodyForCaller(t *testing.T) {
	v, err := Load(specPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	wantBody := []byte(`{"releases":{"1.0.0":{"url":"http://127.0.0.1:8080/example/SamplePackage/1.0.0"}}}`)
	next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header: http.Header{
				"Content-Type":    []string{"application/json"},
				"Content-Version": []string{"1"},
			},
			Body: io.NopCloser(bytes.NewReader(wantBody)),
		}, nil
	})

	rt := &RoundTripper{Next: next, Validator: v}
	req, _ := http.NewRequest("GET", "http://127.0.0.1:8080/example/SamplePackage", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("unexpected error reading response body: %v", err)
	}
	if !bytes.Equal(gotBody, wantBody) {
		t.Errorf("expected caller to still see the original response body %q, got %q", wantBody, gotBody)
	}
}

func Test_RoundTrip_NonConformantResponse_LogsWarning(t *testing.T) {
	v, err := Load(specPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	buf := captureLogs(t)

	next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}}, // no Content-Version
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"releases":{}}`))),
		}, nil
	})

	rt := &RoundTripper{Next: next, Validator: v}
	req, _ := http.NewRequest("GET", "http://127.0.0.1:8080/example/SamplePackage", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), "does not conform") {
		t.Errorf("expected a conformance warning, got: %s", buf.String())
	}
}

func Test_WrapClient_ValidSpec_WrapsTransport(t *testing.T) {
	client := &http.Client{}
	WrapClient(client, specPath)
	if _, ok := client.Transport.(*RoundTripper); !ok {
		t.Errorf("expected client.Transport to be wrapped, got %T", client.Transport)
	}
}

func Test_WrapClient_MissingSpec_LeavesClientUnwrapped(t *testing.T) {
	client := &http.Client{}
	buf := captureLogs(t)

	WrapClient(client, "does-not-exist.yaml")

	if client.Transport != nil {
		t.Errorf("expected client.Transport to be left unmodified, got %T", client.Transport)
	}
	if !strings.Contains(buf.String(), "conformance checking disabled") {
		t.Errorf("expected a warning about disabled conformance checking, got: %s", buf.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
