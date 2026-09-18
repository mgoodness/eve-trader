-- v1 SQLite schema. See docs/spec/v1.md §5 for the full narrative.
--
-- Five tables. market_order and character_skill/esi_token are
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
