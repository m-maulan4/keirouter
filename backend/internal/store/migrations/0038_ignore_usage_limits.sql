-- Per-account opt-in for custom providers: while ON, a 429/402 only parks the
-- account for 10s instead of parking it permanently. Requests keep retrying
-- through quota failures. Built-in provider accounts always ignore this flag
-- (the gateway rejects it).
ALTER TABLE accounts ADD COLUMN ignore_usage_limits INTEGER NOT NULL DEFAULT 0;