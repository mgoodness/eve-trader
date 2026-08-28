package esi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func newTestRealClient(t *testing.T, handler http.HandlerFunc) *RealClient {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := NewRealClient("test-client-id", "test-client-secret")
	c.TokenURL = srv.URL
	return c
}

func TestExchangeCode(t *testing.T) {
	var gotForm url.Values
	var gotUser, gotPass string

	client := newTestRealClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		gotForm = r.PostForm
		gotUser, gotPass, _ = r.BasicAuth()

		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", ct)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    1199,
		})
	})

	token, err := client.ExchangeCode(context.Background(), "auth-code-123")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}

	if gotUser != "test-client-id" || gotPass != "test-client-secret" {
		t.Errorf("basic auth = %q:%q, want test-client-id:test-client-secret", gotUser, gotPass)
	}
	if gotForm.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q, want authorization_code", gotForm.Get("grant_type"))
	}
	if gotForm.Get("code") != "auth-code-123" {
		t.Errorf("code = %q, want auth-code-123", gotForm.Get("code"))
	}

	if token.AccessToken != "new-access-token" {
		t.Errorf("AccessToken = %q, want new-access-token", token.AccessToken)
	}
	if token.RefreshToken != "new-refresh-token" {
		t.Errorf("RefreshToken = %q, want new-refresh-token", token.RefreshToken)
	}
	if token.ExpiresIn != 1199*time.Second {
		t.Errorf("ExpiresIn = %v, want %v", token.ExpiresIn, 1199*time.Second)
	}
}

func TestRefreshAccessToken(t *testing.T) {
	var gotForm url.Values

	client := newTestRealClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		gotForm = r.PostForm

		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "rotated-access-token",
			"refresh_token": "rotated-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    1199,
		})
	})

	token, err := client.RefreshAccessToken(context.Background(), "old-refresh-token")
	if err != nil {
		t.Fatalf("RefreshAccessToken: %v", err)
	}

	if gotForm.Get("grant_type") != "refresh_token" {
		t.Errorf("grant_type = %q, want refresh_token", gotForm.Get("grant_type"))
	}
	if gotForm.Get("refresh_token") != "old-refresh-token" {
		t.Errorf("refresh_token = %q, want old-refresh-token", gotForm.Get("refresh_token"))
	}
	if token.RefreshToken != "rotated-refresh-token" {
		t.Errorf("RefreshToken = %q, want rotated-refresh-token", token.RefreshToken)
	}
}

func newTestDataClient(t *testing.T, handler http.HandlerFunc) *RealClient {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := NewRealClient("test-client-id", "test-client-secret")
	c.DataBaseURL = srv.URL
	c.Tokens = func(ctx context.Context) (string, error) {
		return "the-access-token", nil
	}
	return c
}

func TestCharacterOrdersReal(t *testing.T) {
	var gotPath, gotAuth string

	client := newTestDataClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")

		json.NewEncoder(w).Encode([]map[string]any{
			{
				"order_id":      int64(1),
				"type_id":       int32(34),
				"region_id":     int32(10000002),
				"location_id":   int64(60003760),
				"is_buy_order":  false,
				"price":         5.5,
				"volume_total":  int32(10000),
				"volume_remain": int32(8000),
				"min_volume":    int32(1),
				"duration":      int32(90),
				"issued":        "2026-08-20T12:00:00Z",
				"escrow":        0.0,
				"range":         "region",
			},
		})
	})

	orders, err := client.CharacterOrders(context.Background(), 95465499)
	if err != nil {
		t.Fatalf("CharacterOrders: %v", err)
	}

	if gotPath != "/characters/95465499/orders/" {
		t.Errorf("path = %q, want /characters/95465499/orders/", gotPath)
	}
	if gotAuth != "Bearer the-access-token" {
		t.Errorf("Authorization = %q, want Bearer the-access-token", gotAuth)
	}
	if len(orders) != 1 {
		t.Fatalf("len(orders) = %d, want 1", len(orders))
	}
	if orders[0].OrderID != 1 || orders[0].TypeID != 34 || orders[0].LocationID != 60003760 {
		t.Errorf("orders[0] = %+v, want OrderID=1 TypeID=34 LocationID=60003760", orders[0])
	}
	wantIssued := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	if !orders[0].Issued.Equal(wantIssued) {
		t.Errorf("Issued = %v, want %v", orders[0].Issued, wantIssued)
	}
}

func TestCharacterOrdersTokenSourceError(t *testing.T) {
	client := newTestDataClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request should not have been made when the token source fails")
	})
	client.Tokens = func(ctx context.Context) (string, error) {
		return "", errors.New("no token")
	}

	if _, err := client.CharacterOrders(context.Background(), 1); err == nil {
		t.Fatal("CharacterOrders: want error when token source fails, got nil")
	}
}

func TestCharacterOrderHistoryReal(t *testing.T) {
	var gotQuery url.Values

	client := newTestDataClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"order_id":      int64(2),
				"type_id":       int32(35),
				"region_id":     int32(10000002),
				"location_id":   int64(60003760),
				"is_buy_order":  true,
				"price":         12.0,
				"volume_total":  int32(5000),
				"volume_remain": int32(0),
				"min_volume":    int32(1),
				"duration":      int32(30),
				"issued":        "2026-07-20T12:00:00Z",
				"state":         "expired",
			},
		})
	})

	entries, err := client.CharacterOrderHistory(context.Background(), 95465499, 2)
	if err != nil {
		t.Fatalf("CharacterOrderHistory: %v", err)
	}

	if gotQuery.Get("page") != "2" {
		t.Errorf("page query param = %q, want 2", gotQuery.Get("page"))
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].State != "expired" {
		t.Errorf("State = %q, want expired", entries[0].State)
	}
	if entries[0].TypeID != 35 {
		t.Errorf("TypeID = %d, want 35", entries[0].TypeID)
	}
}

func TestResolveNamesReal(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody []int32

	client := newTestDataClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		json.NewEncoder(w).Encode([]map[string]any{
			{"id": int32(34), "name": "Tritanium", "category": "inventory_type"},
			{"id": int32(35), "name": "Pyerite", "category": "inventory_type"},
		})
	})

	names, err := client.ResolveNames(context.Background(), []int32{34, 35})
	if err != nil {
		t.Fatalf("ResolveNames: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/universe/names/" {
		t.Errorf("path = %q, want /universe/names/", gotPath)
	}
	if len(gotBody) != 2 || gotBody[0] != 34 || gotBody[1] != 35 {
		t.Errorf("request body = %v, want [34 35]", gotBody)
	}
	if names[34] != "Tritanium" || names[35] != "Pyerite" {
		t.Errorf("names = %v, want map[34:Tritanium 35:Pyerite]", names)
	}
}

func TestResolveNamesEmptyInput(t *testing.T) {
	client := newTestDataClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request should not have been made for an empty id list")
	})

	names, err := client.ResolveNames(context.Background(), nil)
	if err != nil {
		t.Fatalf("ResolveNames: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("names = %v, want empty", names)
	}
}

func TestResolveNamesChunksOverESILimit(t *testing.T) {
	var calls [][]int32

	client := newTestDataClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body []int32
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		calls = append(calls, body)

		resp := make([]map[string]any, len(body))
		for i, id := range body {
			resp[i] = map[string]any{"id": id, "name": fmt.Sprintf("Item %d", id), "category": "inventory_type"}
		}
		json.NewEncoder(w).Encode(resp)
	})

	ids := make([]int32, 1500)
	for i := range ids {
		ids[i] = int32(i + 1)
	}

	names, err := client.ResolveNames(context.Background(), ids)
	if err != nil {
		t.Fatalf("ResolveNames: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("made %d requests, want 2 (chunked at ESI's 1000-id limit)", len(calls))
	}
	if len(names) != 1500 {
		t.Errorf("len(names) = %d, want 1500", len(names))
	}
}

func TestTokenRequestErrorStatus(t *testing.T) {
	client := newTestRealClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	})

	if _, err := client.ExchangeCode(context.Background(), "bad-code"); err == nil {
		t.Fatal("ExchangeCode: want error on non-200 response, got nil")
	}
}
