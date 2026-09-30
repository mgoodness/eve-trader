package esi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// wireHistoryRecord mirrors one record of the market-history wire format
// (docs/research/eve-market-mechanics-and-esi.md §6.2): "date, order_count,
// volume, highest, average, lowest". OrderCount and Average aren't needed
// by the filter layer and are dropped in toEngineHistoryRecord.
type wireHistoryRecord struct {
	Date       string  `json:"date"`
	OrderCount int64   `json:"order_count"`
	Volume     int64   `json:"volume"`
	Highest    float64 `json:"highest"`
	Average    float64 `json:"average"`
	Lowest     float64 `json:"lowest"`
}

func (r wireHistoryRecord) toEngineHistoryRecord() engine.HistoryRecord {
	return engine.HistoryRecord{
		Date:    r.Date,
		Volume:  r.Volume,
		Highest: r.Highest,
		Lowest:  r.Lowest,
	}
}

// HistoryResult is one response of GET /markets/{regionID}/history/: its
// body and ETag. NotModified reports a 304 (ifNoneMatch matched the
// server's current ETag); Body is then empty, and the caller should reuse
// its previously cached body.
type HistoryResult struct {
	Body        []byte
	ETag        string
	NotModified bool
}

// FetchHistory fetches GET /markets/{regionID}/history/?type_id={typeID}
// (docs/research/eve-market-mechanics-and-esi.md §6.2), sending
// If-None-Match: ifNoneMatch if non-empty, and backing off and retrying on
// 429/420 like FetchRegionOrdersPage. The response has no pagination: one
// call returns the type's whole history. Callers are responsible for
// caching the result (spec §6: until 11:05 daily).
func (c *Client) FetchHistory(ctx context.Context, regionID, typeID int32, ifNoneMatch string) (HistoryResult, error) {
	reqURL := fmt.Sprintf("%s/markets/%d/history/?type_id=%d", c.baseURL, regionID, typeID)

	resp, err := c.getWithBackoff(ctx, reqURL, ifNoneMatch)
	if err != nil {
		return HistoryResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return HistoryResult{NotModified: true, ETag: ifNoneMatch}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return HistoryResult{}, fmt.Errorf("GET %s: unexpected status %s", reqURL, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return HistoryResult{}, err
	}

	return HistoryResult{Body: body, ETag: resp.Header.Get("ETag")}, nil
}

// DecodeHistory decodes one GET /markets/{regionID}/history/ response body
// into engine.HistoryRecord values. Exported so a caller that fetches and
// caches the response itself can decode a server-fetched or cache-reused
// body the same way FetchHistory's caller does.
func DecodeHistory(body []byte) ([]engine.HistoryRecord, error) {
	var wire []wireHistoryRecord
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	records := make([]engine.HistoryRecord, 0, len(wire))
	for _, r := range wire {
		records = append(records, r.toEngineHistoryRecord())
	}
	return records, nil
}
