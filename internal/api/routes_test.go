package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestValidateRoutingTableToken(t *testing.T) {
	for _, value := range []string{"", "0", "254", "4294967295", "main", "wan_fiber", "A.b-1", "A" + strings.Repeat("x", 63)} {
		if err := validateRoutingTableToken(value); err != nil {
			t.Errorf("valid token %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{
		"4294967296", "9999999999", "12345678901", "-1", "-wan", "_wan", ".wan", "wan table",
		"wan;id", "wan\nid", "wan\rid", "$(id)", "`id`", "wan/route", "A" + strings.Repeat("x", 64),
	} {
		if err := validateRoutingTableToken(value); err == nil {
			t.Errorf("unsafe token %q unexpectedly accepted", value)
		}
	}
}

func TestInterfaceListRejectsNonBooleanFilters(t *testing.T) {
	for _, value := range []string{"1", "TRUE", "yes", " false ", "null"} {
		request := httptest.NewRequest(http.MethodGet, "http://multispeed.local/api/v1/interfaces?includeDown="+url.QueryEscape(value), nil)
		response := httptest.NewRecorder()
		testHandler(t).ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("value %q status=%d body=%s", value, response.Code, response.Body.String())
		}
	}
}
