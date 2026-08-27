package diagnostic

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

var state struct {
	sync.RWMutex
	w io.Writer
}

var (
	sensitivePair = regexp.MustCompile(`(?i)(access_token|authorization|client_secret|code|developer_key|id_token|refresh_token|token)=([^&\s]+)`)
	bearerToken   = regexp.MustCompile(`(?i)bearer\s+[^\s"']+`)
)

// Enable directs diagnostic events to w. Diagnostics are disabled by default.
func Enable(w io.Writer) {
	state.Lock()
	defer state.Unlock()
	state.w = w
}

func Enabled() bool {
	state.RLock()
	defer state.RUnlock()
	return state.w != nil
}

func Log(component, event string, fields ...any) {
	state.RLock()
	w := state.w
	state.RUnlock()
	if w == nil {
		return
	}
	var line strings.Builder
	fmt.Fprintf(&line, "%s component=%s event=%s", time.Now().UTC().Format(time.RFC3339Nano), safe(component), safe(event))
	for index := 0; index+1 < len(fields); index += 2 {
		fmt.Fprintf(&line, " %s=%s", safe(fmt.Sprint(fields[index])), quote(fields[index+1]))
	}
	line.WriteByte('\n')
	state.Lock()
	_, _ = io.WriteString(w, line.String())
	state.Unlock()
}

// Start records an operation and returns a completion callback.
func Start(component, operation string, fields ...any) func(error) {
	started := time.Now()
	Log(component, operation+".start", fields...)
	return func(err error) {
		result := append([]any{}, fields...)
		result = append(result, "duration", time.Since(started).Round(time.Millisecond))
		if err != nil {
			result = append(result, "error", err)
			Log(component, operation+".error", result...)
			return
		}
		Log(component, operation+".complete", result...)
	}
}

// HTTPContext makes OAuth and API traffic observable while preserving OAuth's
// own authenticated transport. Request bodies and sensitive headers are never
// inspected.
func HTTPContext(ctx context.Context, component string) context.Context {
	if !Enabled() {
		return ctx
	}
	client := &http.Client{Transport: Transport(http.DefaultTransport, component)}
	return context.WithValue(ctx, oauth2.HTTPClient, client)
}

func Transport(base http.RoundTripper, component string) http.RoundTripper {
	if !Enabled() {
		return base
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return roundTripper{base: base, component: component}
}

type roundTripper struct {
	base      http.RoundTripper
	component string
}

func (transport roundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	started := time.Now()
	requestURL := sanitizedURL(request.URL)
	Log(transport.component, "http.request", "method", request.Method, "url", requestURL, "content_length", request.ContentLength)
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		Log(transport.component, "http.error", "method", request.Method, "url", requestURL, "duration", time.Since(started).Round(time.Millisecond), "error", err)
		return nil, err
	}
	Log(transport.component, "http.response",
		"method", request.Method,
		"url", requestURL,
		"status", response.StatusCode,
		"duration", time.Since(started).Round(time.Millisecond),
		"content_length", response.ContentLength,
		"request_id", firstHeader(response.Header, "X-GUploader-UploadID", "X-Goog-Request-Id", "X-Request-Id"),
	)
	return response, nil
}

func sanitizedURL(value *url.URL) string {
	if value == nil {
		return ""
	}
	return value.Scheme + "://" + value.Host + value.EscapedPath()
}

func firstHeader(header http.Header, names ...string) string {
	for _, name := range names {
		if value := header.Get(name); value != "" {
			return value
		}
	}
	return ""
}

func quote(value any) string {
	text := sanitize(fmt.Sprint(value))
	return fmt.Sprintf("%q", text)
}

func sanitize(value string) string {
	value = sensitivePair.ReplaceAllString(value, "$1=[REDACTED]")
	value = bearerToken.ReplaceAllString(value, "Bearer [REDACTED]")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\r", "\\r")
	return value
}

func safe(value string) string {
	value = sanitize(value)
	value = strings.Map(func(r rune) rune {
		if r == ' ' || r == '=' || r == '\t' {
			return '_'
		}
		return r
	}, value)
	return value
}

func EnabledByEnvironment() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("STAMP_VERBOSE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
