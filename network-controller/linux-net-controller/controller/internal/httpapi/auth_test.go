package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBasicAuth(t *testing.T) {
	handler := WithBasicAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), "admin", "correct-password")

	for _, test := range []struct {
		name     string
		user     string
		password string
		want     int
	}{
		{"missing", "", "", http.StatusUnauthorized},
		{"wrong", "admin", "wrong-password", http.StatusUnauthorized},
		{"valid", "admin", "correct-password", http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			if test.user != "" {
				request.SetBasicAuth(test.user, test.password)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
}
