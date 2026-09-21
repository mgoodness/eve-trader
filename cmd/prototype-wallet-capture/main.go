// Command prototype-wallet-capture is a throwaway one-off: it runs an EVE SSO
// PKCE flow, then dumps a redacted sample of this character's wallet
// transactions, wallet journal, open orders, order history, and item-exchange
// contracts to JSON files.
//
// It exists to satisfy the wayfinder task "Capture a real redacted
// wallet/journal/orders sample" (eve-trader#93) — to validate the P/L model
// and fee-attribution design against real data. Not production code.
//
// Run:
//
//	go run ./cmd/prototype-wallet-capture \
//	  --client-id <EVE dev app client id> \
//	  --callback http://localhost:8098/callback \
//	  --out ./wallet-sample
//
// Register `--callback` as a redirect URI on an EVE developer application
// whose scopes include the three below, then open the printed URL.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgoodness/eve-trader/esi"
)

const (
	authorizeEndpoint = "https://login.eveonline.com/v2/oauth/authorize"
	esiBase           = "https://esi.evetech.net/latest"
	scopes            = "esi-wallet.read_character_wallet.v1 esi-markets.read_character_orders.v1 esi-contracts.read_character_contracts.v1"
)

// sensitiveKeys are the ID fields that identify other players/corps/alliances.
// They are replaced with stable pseudonyms so the sample keeps internal joins
// but carries no personal identifiers. Market/type/order/transaction IDs are
// deliberately kept.
var sensitiveKeys = map[string]bool{
	"client_id":             true,
	"first_party_id":        true,
	"second_party_id":       true,
	"issuer_id":             true,
	"issuer_corporation_id": true,
	"assignee_id":           true,
	"acceptor_id":           true,
	"character_id":          true,
}

func main() {
	clientID := flag.String("client-id", os.Getenv("EVE_TRADER_ESI_CLIENT_ID"), "EVE developer app client ID (or EVE_TRADER_ESI_CLIENT_ID)")
	callback := flag.String("callback", "http://localhost:8098/callback", "redirect URI registered on the app; its host:port is where this listens")
	out := flag.String("out", "wallet-sample", "output directory for the redacted JSON sample")
	maxPages := flag.Int("max-pages", 5, "max pages to fetch per paginated endpoint")
	flag.Parse()

	if *clientID == "" {
		log.Fatal("need --client-id or EVE_TRADER_ESI_CLIENT_ID")
	}
	cb, err := url.Parse(*callback)
	if err != nil || cb.Host == "" {
		log.Fatalf("bad --callback %q: %v", *callback, err)
	}

	verifier, challenge, err := newPKCE()
	if err != nil {
		log.Fatal(err)
	}
	state, err := randomState()
	if err != nil {
		log.Fatal(err)
	}

	authURL := authorizeEndpoint + "?" + url.Values{
		"response_type":         {"code"},
		"redirect_uri":          {*callback},
		"client_id":             {*clientID},
		"scope":                 {scopes},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode()
	fmt.Println("Open this URL and authorize:")
	fmt.Println()
	fmt.Println("  " + authURL)
	fmt.Println()

	mux := http.NewServeMux()
	mux.HandleFunc(cb.Path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("error") != "" {
			http.Error(w, "SSO error: "+q.Get("error"), http.StatusBadRequest)
			return
		}
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		gw := &esi.HTTPGateway{ClientID: *clientID, CallbackURL: *callback}
		tok, err := gw.ExchangeCode(r.Context(), q.Get("code"), verifier)
		if err != nil {
			http.Error(w, "token exchange failed", http.StatusInternalServerError)
			log.Printf("token exchange: %v", err)
			return
		}
		fmt.Fprintln(w, "Captured. You can close this tab.")
		go func() {
			if err := capture(tok, *out, *maxPages); err != nil {
				log.Printf("capture: %v", err)
				os.Exit(1)
			}
			fmt.Println("Sample written to", *out)
			time.Sleep(500 * time.Millisecond)
			os.Exit(0)
		}()
	})

	log.Printf("listening on %s for the SSO callback", cb.Host)
	if err := http.ListenAndServe(cb.Host, mux); err != nil {
		log.Fatal(err)
	}
}

func newPKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func capture(tok esi.Token, outDir string, maxPages int) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	ctx := context.Background()
	cl := &http.Client{Timeout: 60 * time.Second}
	tbl := &redactor{next: 900000000, seen: map[int64]int64{}}

	type source struct{ key, path, mode string }
	sources := []source{
		{"transactions", fmt.Sprintf("/characters/%d/wallet/transactions", tok.CharacterID), "from_id"},
		{"journal", fmt.Sprintf("/characters/%d/wallet/journal", tok.CharacterID), "page"},
		{"orders", fmt.Sprintf("/characters/%d/orders", tok.CharacterID), "page"},
		{"order_history", fmt.Sprintf("/characters/%d/orders/history", tok.CharacterID), "page"},
		{"contracts", fmt.Sprintf("/characters/%d/contracts", tok.CharacterID), "page"},
	}

	bundle := map[string]any{}
	for _, src := range sources {
		raw, err := fetchPaginated(ctx, cl, tok.AccessToken, esiBase+src.path, maxPages, src.mode)
		if err != nil {
			return fmt.Errorf("%s: %w", src.key, err)
		}
		redacted := tbl.redact(raw)
		bundle[src.key] = redacted
		if err := writeJSON(filepath.Join(outDir, src.key+".json"), redacted); err != nil {
			return err
		}
	}

	// Contract items for item-exchange contracts, capped so an old contracts
	// list can't fan out into thousands of calls.
	var items []any
	if contracts, ok := bundle["contracts"].([]any); ok {
		fetched := 0
		for _, c := range contracts {
			cm, _ := c.(map[string]any)
			if cm == nil || cm["type"] != "item_exchange" {
				continue
			}
			id, _ := cm["contract_id"].(float64)
			if id == 0 || fetched >= 50 {
				continue
			}
			raw, err := fetchPaginated(ctx, cl, tok.AccessToken, fmt.Sprintf("%s/characters/%d/contracts/%d/items", esiBase, tok.CharacterID, int64(id)), 1, "page")
			if err != nil {
				log.Printf("contract %d items: %v", int64(id), err)
				continue
			}
			items = append(items, map[string]any{"contract_id": int64(id), "items": tbl.redact(raw)})
			fetched++
		}
	}
	bundle["contract_items"] = items
	if err := writeJSON(filepath.Join(outDir, "contract_items.json"), items); err != nil {
		return err
	}

	meta := map[string]any{
		"captured_at":        time.Now().UTC().Format(time.RFC3339),
		"scopes":             scopes,
		"character_redacted": true,
		"counts": map[string]int{
			"transactions":   count(bundle["transactions"]),
			"journal":        count(bundle["journal"]),
			"orders":         count(bundle["orders"]),
			"order_history":  count(bundle["order_history"]),
			"contracts":      count(bundle["contracts"]),
			"contract_items": len(items),
		},
	}
	if err := writeJSON(filepath.Join(outDir, "meta.json"), meta); err != nil {
		return err
	}
	return writeJSON(filepath.Join(outDir, "bundle.json"), bundle)
}

// fetchPaginated reads an ESI route into one slice, using either page/X-Pages
// pagination or the inclusive from_id pagination used by wallet transactions.
func fetchPaginated(ctx context.Context, cl *http.Client, token, endpoint string, maxPages int, mode string) ([]any, error) {
	var all []any
	seen := map[float64]bool{}
	for i := 0; i < maxPages; i++ {
		body, pages, err := getJSON(ctx, cl, token, endpoint)
		if err != nil {
			return nil, err
		}
		var chunk []map[string]any
		if err := json.Unmarshal(body, &chunk); err != nil {
			return nil, fmt.Errorf("decoding %s: %w", endpoint, err)
		}
		if len(chunk) == 0 {
			break
		}
		fresh := chunk
		if mode == "from_id" {
			// ESI includes the referenced record, so drop the boundary duplicate.
			fresh = nil
			for _, rec := range chunk {
				id, _ := rec["transaction_id"].(float64)
				if seen[id] {
					continue
				}
				seen[id] = true
				fresh = append(fresh, rec)
			}
			if len(fresh) == 0 {
				break
			}
		}
		all = append(all, toAny(fresh)...)
		switch mode {
		case "from_id":
			id, ok := chunk[len(chunk)-1]["transaction_id"].(float64)
			if !ok {
				return all, nil
			}
			endpoint = fmt.Sprintf("%s?from_id=%d", endpoint, int64(id))
		case "page":
			if i+1 >= pages {
				return all, nil
			}
			sep := "?"
			if strings.Contains(endpoint, "?") {
				sep = "&"
			}
			endpoint = fmt.Sprintf("%s%spage=%d", endpoint, sep, i+2)
		}
	}
	return all, nil
}

func toAny(in []map[string]any) []any {
	out := make([]any, len(in))
	for i := range in {
		out[i] = in[i]
	}
	return out
}

func getJSON(ctx context.Context, cl *http.Client, token, endpoint string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := cl.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("GET %s: %s: %.200s", endpoint, resp.Status, body)
	}
	pages := 1
	if p := resp.Header.Get("X-Pages"); p != "" {
		fmt.Sscanf(p, "%d", &pages)
	}
	return body, pages, nil
}

type redactor struct {
	next int64
	seen map[int64]int64
}

// redact walks decoded JSON and replaces counterparty IDs with stable
// pseudonyms.
func (r *redactor) redact(v any) any {
	switch t := v.(type) {
	case []any:
		for i := range t {
			t[i] = r.redact(t[i])
		}
		return t
	case []map[string]any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = r.redact(t[i])
		}
		return out
	case map[string]any:
		for k, val := range t {
			if sensitiveKeys[k] {
				if f, ok := val.(float64); ok {
					t[k] = r.alias(int64(f))
					continue
				}
			}
			t[k] = r.redact(val)
		}
		return t
	default:
		return v
	}
}

func (r *redactor) alias(id int64) int64 {
	if id == 0 {
		return 0
	}
	if a, ok := r.seen[id]; ok {
		return a
	}
	r.next++
	r.seen[id] = r.next
	return r.next
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func count(v any) int {
	if s, ok := v.([]any); ok {
		return len(s)
	}
	return 0
}
