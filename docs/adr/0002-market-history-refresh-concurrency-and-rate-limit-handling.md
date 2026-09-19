# Market-history refresh uses a bounded worker pool, chunked commits, and reactive rate-limit handling

A sequential history refresh over ~8,000 types took 25–75 minutes, delaying recovery after a restart and holding a single SQLite write transaction open until every fetch finished. We fetch with a fixed pool of 12 concurrent workers (stdlib only), commit every 500 successful types in its own transaction, cap each request at 15 s, and cap a whole refresh at 30 minutes — committing the remainder on expiry. ESI rate limiting reaches the poller as a typed error (`esi.RateLimited`) carrying `Retry-After`; on 420/429 the whole pool pauses until `Retry-After`, 429/420/5xx retry up to three times with backoff, and 404 never retries. The history refresh does not share a rate-limit budget with the order poller. ([#49](https://github.com/mgoodness/eve-trader/issues/49))

## Considered Options

- **Adaptive concurrency (AIMD / headroom-capped)** — deferred, not rejected: the history route exposes no `X-Ratelimit-*` headers, so there is no request-rate ceiling to hunt, and a refresh is a fixed batch rather than a sustained stream. Keep as "option B" if a fixed pool proves insufficient.
- **Preemptive guard on `X-Esi-Error-Limit-Remain < 20`** — rejected: a normal refresh spends only ~8 of the 100-error budget on permanent 404s, so the guard would never fire. ESI's own 420 + `Retry-After` is the authoritative signal, and surfacing every response's error-limit header would widen the `ESIGateway` seam for no benefit.
- **One transaction after all fetches** — rejected: a restart loses the entire refresh, and the write lock is held for the whole sweep.
- **A batch/concurrent `FetchHistory` on `ESIGateway`** — rejected: keeping the gateway per-type leaves the existing fake and black-box test seam simple; the poller owns concurrency and persistence.
