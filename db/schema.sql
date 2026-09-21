-- v1 SQLite schema, plus the v2 ledger tables appended below. See
-- docs/spec/v1.md §5 and docs/spec/v2.md §5 for the full narrative.
--
-- v1's five tables: market_order and character_skill/esi_token are
-- current-state caches; only market_history retains a rolling window.
-- No order-level history is kept; market_history carries each day's
-- aggregate price figures alongside its volume.

-- Current Rens order book. Upserted + hard-pruned every poll (~5 min).
CREATE TABLE IF NOT EXISTS market_order (
  order_id       INTEGER PRIMARY KEY,
  type_id        INTEGER NOT NULL REFERENCES item_type(type_id),
  is_buy_order   INTEGER NOT NULL,   -- 0/1
  price          REAL    NOT NULL,
  volume_remain  INTEGER NOT NULL,
  volume_total   INTEGER NOT NULL,
  min_volume     INTEGER NOT NULL,
  issued         TEXT    NOT NULL,   -- ISO8601
  duration       INTEGER NOT NULL,
  updated_at     TEXT    NOT NULL    -- timestamp of the poll that last saw this row
);
CREATE INDEX IF NOT EXISTS idx_market_order_type_id ON market_order(type_id);

-- 30-day rolling window of Heimatar-region daily volume and prices,
-- refreshed once/day. V_d in the ranking formula is the average `volume`
-- over this window, computed at query time (not pre-aggregated).
--
-- average/highest/lowest were added after the initial v1 schema (see the
-- migration in db.go). They are nullable so an existing database upgrades
-- in place: rows written before the change keep NULL price fields until
-- that type's next history refresh replaces its window. A temporarily
-- shorter opportunity list for up to one history cycle after deploy is
-- expected, not a bug, and needs no manual backfill.
CREATE TABLE IF NOT EXISTS market_history (
  type_id      INTEGER NOT NULL REFERENCES item_type(type_id),
  date         TEXT    NOT NULL,  -- ISO8601 date
  volume       INTEGER NOT NULL,
  order_count  INTEGER NOT NULL,
  average      REAL,
  highest      REAL,
  lowest       REAL,
  PRIMARY KEY (type_id, date)
);

-- Lazy display-name cache. Populated the first time a type_id is seen in a
-- Rens pull; refreshed on a long cadence (e.g. weekly). Also stands in for
-- the "Rens-tradable" item set -- no separate curated catalog.
CREATE TABLE IF NOT EXISTS item_type (
  type_id     INTEGER PRIMARY KEY,
  name        TEXT    NOT NULL,
  updated_at  TEXT    NOT NULL
);

-- Single row: the one trading character's fee/tax skill levels.
CREATE TABLE IF NOT EXISTS character_skill (
  character_id             INTEGER PRIMARY KEY,
  broker_relations_level   INTEGER NOT NULL,
  accounting_level         INTEGER NOT NULL,
  updated_at               TEXT    NOT NULL
);

-- Single row: OAuth refresh token (see docs/spec/v1.md §6).
CREATE TABLE IF NOT EXISTS esi_token (
  character_id              INTEGER PRIMARY KEY,
  owner_hash                TEXT    NOT NULL,
  encrypted_refresh_token   BLOB    NOT NULL,
  updated_at                TEXT    NOT NULL
);

-- v2 ledger: append-only raw ESI wallet records (docs/spec/v2.md §5).
-- Added to the v1.1 schema by the same idempotent CREATE this file already
-- runs on every db.Open, so an existing database gains these tables with
-- no data loss and no separate migration step.

-- The character's wallet transactions (buys and sells), keyed by ESI's own
-- transaction_id. journal_ref_id is stored exactly as ESI returns it but
-- must never be used to find the matching wallet_journal row -- it is
-- unreliable (docs/spec/v2.md §3). Join on wallet_journal.context_id
-- instead.
CREATE TABLE IF NOT EXISTS wallet_transaction (
  transaction_id  INTEGER PRIMARY KEY,
  date            TEXT    NOT NULL,
  type_id         INTEGER NOT NULL,
  quantity        INTEGER NOT NULL,
  unit_price      REAL    NOT NULL,
  is_buy          INTEGER NOT NULL,   -- 0/1
  is_personal     INTEGER NOT NULL,   -- 0/1
  journal_ref_id  INTEGER NOT NULL,
  location_id     INTEGER NOT NULL,
  client_id       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_wallet_transaction_type_id ON wallet_transaction(type_id);

-- The character's wallet journal, keyed by ESI's own journal id. A
-- market_transaction entry's context_id equals the matching
-- wallet_transaction.transaction_id when context_id_type is
-- 'market_transaction_id' -- the load-bearing link back to
-- wallet_transaction (docs/spec/v2.md §3).
CREATE TABLE IF NOT EXISTS wallet_journal (
  id               INTEGER PRIMARY KEY,
  date             TEXT    NOT NULL,
  ref_type         TEXT    NOT NULL,
  amount           REAL    NOT NULL,
  balance          REAL    NOT NULL,
  context_id       INTEGER,
  context_id_type  TEXT,
  description      TEXT    NOT NULL,
  first_party_id   INTEGER,
  second_party_id  INTEGER,
  reason           TEXT,
  tax              REAL,
  tax_receiver_id  INTEGER
);
CREATE INDEX IF NOT EXISTS idx_wallet_journal_context ON wallet_journal(context_id_type, context_id);

-- Sync progress per ledger stream (e.g. 'wallet_transaction',
-- 'wallet_journal'), bounding incremental fetch and first-run backfill
-- (docs/spec/v2.md §5). oldest_id/newest_id are the lowest/highest record
-- ID this stream has ever stored; backfilled marks that a full backward
-- walk (wallet transactions' from_id paging) has reached the end of ESI's
-- retained window, so later polls need only fetch forward.
CREATE TABLE IF NOT EXISTS ledger_sync (
  stream       TEXT    PRIMARY KEY,
  oldest_id    INTEGER,
  newest_id    INTEGER,
  backfilled   INTEGER NOT NULL DEFAULT 0,   -- 0/1
  updated_at   TEXT    NOT NULL
);
