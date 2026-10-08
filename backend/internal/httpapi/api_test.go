package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCSRFFilterBeforeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &API{Origin: "http://localhost:8090", Shutdown: context.Background()}
	router := api.Router()
	for _, tc := range []struct {
		name, origin, header string
		expected             int
	}{
		{"missing header", "", "", 403},
		{"foreign origin", "https://foreign.example", "1", 403},
		{"trusted origin without session", "http://localhost:8090", "1", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/api/workspaces", strings.NewReader(`{"name":"Test"}`))
			request.Header.Set("Origin", tc.origin)
			request.Header.Set("X-Flowdesk-Request", tc.header)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.expected {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if response.Header().Get("X-Request-ID") == "" {
				t.Fatal("нет request ID")
			}
		})
	}
}
