package server_test

import "github.com/mgoodness/eve-trader/server"

// testAuthConfig returns a fixed AuthConfig for tests that don't
// exercise the auth flow itself but still need a valid Server (server.New
// requires one). Tests of the auth flow (see auth_test.go) may build
// their own.
func testAuthConfig() server.AuthConfig {
	return server.AuthConfig{
		ClientID:     "test-client-id",
		CallbackURL:  "http://example.com/auth/callback",
		CookieSecret: "test-cookie-secret",
		TokenKey:     "test-token-key",
	}
}
