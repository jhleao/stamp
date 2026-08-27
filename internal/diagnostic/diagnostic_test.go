package diagnostic

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiagnosticsRedactSecretsAcrossEventsAndHTTP(t *testing.T) {
	var output bytes.Buffer
	Enable(&output)
	t.Cleanup(func() { Enable(nil) })

	secret := "do-not-print"
	done := Start("drive", "login", "url", "https://example.test/callback?code="+secret)
	done(errors.New("access_token=" + secret + " Authorization: Bearer " + secret))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Goog-Request-Id", "request-123")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	request, _ := http.NewRequest(http.MethodGet, server.URL+"?key="+secret, nil)
	request.Header.Set("Authorization", "Bearer "+secret)
	_, _ = Transport(http.DefaultTransport, "drive").RoundTrip(request)

	log := output.String()
	if strings.Contains(log, secret) {
		t.Fatalf("diagnostics leaked a secret: %s", log)
	}
	for _, expected := range []string{"event=login.start", "event=login.error", "event=http.request", "status=\"403\"", "request_id=\"request-123\""} {
		if !strings.Contains(log, expected) {
			t.Fatalf("diagnostics omitted %q: %s", expected, log)
		}
	}
}
