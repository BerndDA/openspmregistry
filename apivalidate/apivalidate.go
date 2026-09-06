// Package apivalidate checks real HTTP request/response pairs made in tests against a vendored
// copy of the official Swift Package Registry OpenAPI spec (openapi/registry.openapi.yaml).
// Deviations are logged as warnings via slog — never as failures — so spec drift is visible
// without making conformance checking a build-breaking dependency on an external, evolving
// document.
//
// This server's /collection extension is not part of the official spec, so requests to it are
// silently skipped rather than flagged; /login IS part of the spec and is validated normally.
package apivalidate

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// unsupportedRegexConstructs are ECMA-262 regex features Go's RE2-based regexp package can't
// compile (lookahead/lookbehind). Apple's spec uses these in the Scope/PackageName parameter
// patterns; stripUnsupportedPatterns removes just those patterns rather than disabling pattern
// validation for the whole document, so any other (RE2-compatible) pattern the spec defines is
// still checked.
var unsupportedRegexConstructs = []string{"(?=", "(?!", "(?<=", "(?<!"}

// stripUnsupportedPatterns clears Schema.Pattern on any component parameter whose regex Go can't
// compile. The server's own handlers still enforce scope/name format independently
// (controller/publish.go); this only affects what this OpenAPI conformance layer can check.
func stripUnsupportedPatterns(doc *openapi3.T) {
	if doc.Components == nil {
		return
	}
	for name, paramRef := range doc.Components.Parameters {
		if paramRef == nil || paramRef.Value == nil || paramRef.Value.Schema == nil || paramRef.Value.Schema.Value == nil {
			continue
		}
		schema := paramRef.Value.Schema.Value
		for _, construct := range unsupportedRegexConstructs {
			if strings.Contains(schema.Pattern, construct) {
				slog.Debug("apivalidate: clearing RE2-incompatible pattern", "parameter", name, "pattern", schema.Pattern)
				schema.Pattern = ""
				break
			}
		}
	}
}

// Validator matches real requests against operations defined in a loaded OpenAPI document and
// validates request/response pairs against it.
type Validator struct {
	router  routers.Router
	options *openapi3filter.Options
}

// Load parses and validates specPath (the OpenAPI document itself must be well-formed), then
// builds a router used to match real requests to spec operations.
func Load(specPath string) (*Validator, error) {
	doc, err := openapi3.NewLoader().LoadFromFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("load OpenAPI spec %s: %w", specPath, err)
	}
	stripUnsupportedPatterns(doc)
	if err := doc.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("invalid OpenAPI spec %s: %w", specPath, err)
	}

	// The spec defines no `servers`, so give it one relative server; otherwise gorillamux's host
	// matching rejects every request with ErrPathNotFound (see routers.Router's doc comment).
	if len(doc.Servers) == 0 {
		doc.Servers = openapi3.Servers{{URL: "/"}}
	}

	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		return nil, fmt.Errorf("build router for OpenAPI spec %s: %w", specPath, err)
	}

	return &Validator{
		router: router,
		options: &openapi3filter.Options{
			// Flag response codes the spec doesn't document, not just malformed bodies for
			// codes it does — that's exactly the kind of drift this exists to catch.
			IncludeResponseStatus: true,
			// Report every issue found in one pair, not just the first — a warning log line is
			// cheap; missing all-but-one deviation per request isn't.
			MultiError: true,
		},
	}, nil
}

// Check validates one request/response pair against the spec and logs (via slog.Warn) any
// deviation found. It never returns an error and never panics: unexpected failures inside the
// validation library itself are recovered and logged the same way, since this is a diagnostic
// aid, not a correctness gate. Requests to paths the spec doesn't define at all are silently
// skipped (e.g. /login is in the spec and IS validated; /collection is this server's own
// extension and is not).
//
// req must have a fresh, readable Body containing reqBody (if non-empty) — see RoundTripper,
// the intended way to drive this, which takes care of that.
func (v *Validator) Check(req *http.Request, reqBody []byte, resp *http.Response, respBody []byte) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("OpenAPI conformance check panicked", "method", req.Method, "path", req.URL.Path, "recover", r)
		}
	}()

	// /collection and /collection/{scope} are this server's own extension (package collections),
	// not part of the official spec — but their shape structurally collides with the spec's
	// /{scope}/{name} template (e.g. "/collection/example" parses as scope="collection",
	// name="example"), so the router would otherwise misidentify them as a real spec operation
	// and flag them for missing fields that were never meant to apply here.
	if req.URL.Path == "/collection" || strings.HasPrefix(req.URL.Path, "/collection/") {
		return
	}

	route, pathParams, err := v.router.FindRoute(req)
	if err != nil {
		// Not part of the official spec — nothing to validate.
		return
	}

	reqInput := &openapi3filter.RequestValidationInput{
		Request:    req,
		PathParams: pathParams,
		Route:      route,
		Options:    v.options,
	}

	if err := openapi3filter.ValidateRequest(req.Context(), reqInput); err != nil {
		slog.Warn("OpenAPI spec: request does not conform", "method", req.Method, "path", req.URL.Path, "error", err)
	}

	respInput := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: reqInput,
		Status:                 resp.StatusCode,
		Header:                 resp.Header,
		Options:                v.options,
	}
	respInput.SetBodyBytes(respBody)

	if err := openapi3filter.ValidateResponse(req.Context(), respInput); err != nil {
		slog.Warn("OpenAPI spec: response does not conform", "method", req.Method, "path", req.URL.Path, "status", resp.StatusCode, "error", err)
	}
}

// RoundTripper wraps an http.RoundTripper, running every request/response pair it sees through a
// Validator before returning the (unmodified) response to the caller. Drop it into an
// http.Client's Transport (see WrapClient) to get automatic conformance checking for every
// request that client makes, with zero changes to calling code.
type RoundTripper struct {
	Next      http.RoundTripper
	Validator *Validator
}

func (t *RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	next := t.Next
	if next == nil {
		next = http.DefaultTransport
	}

	var reqBody []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err == nil {
			reqBody = b
		}
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}

	resp, err := next.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}

	respBody, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	if readErr != nil {
		// Body couldn't be buffered for validation; still return the response as-is.
		return resp, nil
	}

	validationReq := req.Clone(req.Context())
	validationReq.Body = io.NopCloser(bytes.NewReader(reqBody))

	t.Validator.Check(validationReq, reqBody, resp, respBody)

	return resp, nil
}

// WrapClient loads the OpenAPI spec at specPath and wraps client's Transport so every request it
// makes is checked for conformance. Best-effort: if the spec can't be loaded, it logs a warning
// and leaves client unmodified rather than failing — conformance checking is a diagnostic aid, not
// a prerequisite for the tests using this client.
func WrapClient(client *http.Client, specPath string) {
	validator, err := Load(specPath)
	if err != nil {
		slog.Warn("OpenAPI conformance checking disabled: failed to load spec", "path", specPath, "error", err)
		return
	}
	client.Transport = &RoundTripper{Next: client.Transport, Validator: validator}
}
