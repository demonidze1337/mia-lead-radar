-- Canonical MIA Lead Radar schema (SQLite WAL, UTC fixed-width timestamps).

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT PRIMARY KEY,
    value_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS auth_sessions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash   TEXT NOT NULL UNIQUE,
    label        TEXT NOT NULL DEFAULT '',
    tg_user_id   INTEGER NOT NULL DEFAULT 0,
    role         TEXT NOT NULL DEFAULT 'owner',
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires ON auth_sessions(expires_at);

CREATE TABLE IF NOT EXISTS radars (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    name                    TEXT NOT NULL,
    description             TEXT NOT NULL DEFAULT '',
    tags_json               TEXT NOT NULL DEFAULT '[]',
    target_languages_json   TEXT NOT NULL DEFAULT '["en","ru"]',
    stop_phrases_json       TEXT NOT NULL DEFAULT '[]',
    learned_stop_words_json TEXT NOT NULL DEFAULT '[]',
    allow_hr_vacancies      INTEGER NOT NULL DEFAULT 0,
    allow_partnerships      INTEGER NOT NULL DEFAULT 0,
    quality_score           INTEGER NOT NULL DEFAULT 0,
    quality_notes           TEXT NOT NULL DEFAULT '',
    version                 INTEGER NOT NULL DEFAULT 1,
    is_active               INTEGER NOT NULL DEFAULT 1,
    created_at              TEXT NOT NULL,
    updated_at              TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS radar_versions (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    radar_id                INTEGER NOT NULL REFERENCES radars(id) ON DELETE CASCADE,
    version                 INTEGER NOT NULL,
    description             TEXT NOT NULL DEFAULT '',
    tags_json               TEXT NOT NULL DEFAULT '[]',
    stop_phrases_json       TEXT NOT NULL DEFAULT '[]',
    learned_stop_words_json TEXT NOT NULL DEFAULT '[]',
    change_source           TEXT NOT NULL DEFAULT 'manual',
    audit_summary           TEXT NOT NULL DEFAULT '',
    created_at              TEXT NOT NULL,
    UNIQUE(radar_id, version)
);
CREATE INDEX IF NOT EXISTS idx_radar_versions_lookup ON radar_versions(radar_id, version DESC);

CREATE TABLE IF NOT EXISTS tg_accounts (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    label                TEXT NOT NULL DEFAULT '',
    phone_masked         TEXT NOT NULL DEFAULT '',
    tg_user_id           INTEGER NOT NULL DEFAULT 0,
    username             TEXT NOT NULL DEFAULT '',
    first_name           TEXT NOT NULL DEFAULT '',
    session_path         TEXT NOT NULL DEFAULT '',
    status               TEXT NOT NULL DEFAULT 'pending_auth',
    flood_until          TEXT,
    joined_chats_count   INTEGER NOT NULL DEFAULT 0,
    daily_joins_count    INTEGER NOT NULL DEFAULT 0,
    daily_joins_reset_at TEXT,
    last_active_at       TEXT,
    last_error           TEXT NOT NULL DEFAULT '',
    created_at           TEXT NOT NULL,
    updated_at           TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tg_accounts_status ON tg_accounts(status, flood_until);

CREATE TABLE IF NOT EXISTS chats (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    tg_chat_id            INTEGER NOT NULL DEFAULT 0,
    access_hash           INTEGER NOT NULL DEFAULT 0,
    username              TEXT NOT NULL DEFAULT '',
    invite_hash           TEXT NOT NULL DEFAULT '',
    title                 TEXT NOT NULL DEFAULT '',
    chat_type             TEXT NOT NULL DEFAULT 'group',
    source                TEXT NOT NULL DEFAULT 'seed',
    status                TEXT NOT NULL DEFAULT 'queued',
    member_count          INTEGER NOT NULL DEFAULT 0,
    msg_velocity_per_hour REAL NOT NULL DEFAULT 0,
    has_comments          INTEGER NOT NULL DEFAULT 0,
    linked_chat_id        INTEGER NOT NULL DEFAULT 0,
    assigned_account_id   INTEGER NOT NULL DEFAULT 0,
    last_message_id       INTEGER NOT NULL DEFAULT 0,
    last_read_at          TEXT,
    last_scanned_at       TEXT,
    next_retry_at         TEXT,
    error_reason          TEXT NOT NULL DEFAULT '',
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_chats_tg_id ON chats(tg_chat_id) WHERE tg_chat_id != 0;
CREATE UNIQUE INDEX IF NOT EXISTS idx_chats_username ON chats(LOWER(username)) WHERE username != '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_chats_invite ON chats(invite_hash) WHERE invite_hash != '';
CREATE INDEX IF NOT EXISTS idx_chats_status ON chats(status, assigned_account_id);

CREATE TABLE IF NOT EXISTS join_queue (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    normalized_link TEXT NOT NULL UNIQUE,
    link_type       TEXT NOT NULL,
    raw_link        TEXT NOT NULL,
    source_chat_id  INTEGER NOT NULL DEFAULT 0,
    priority        INTEGER NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT NOT NULL,
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_join_queue_claim ON join_queue(status, priority DESC, next_attempt_at ASC);

CREATE TABLE IF NOT EXISTS leads (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    radar_id           INTEGER NOT NULL REFERENCES radars(id) ON DELETE CASCADE,
    chat_id            INTEGER NOT NULL DEFAULT 0,
    tg_chat_id         INTEGER NOT NULL,
    chat_title         TEXT NOT NULL DEFAULT '',
    chat_username      TEXT NOT NULL DEFAULT '',
    msg_id             INTEGER NOT NULL,
    msg_link           TEXT NOT NULL DEFAULT '',
    author_id          INTEGER NOT NULL DEFAULT 0,
    author_username    TEXT NOT NULL DEFAULT '',
    author_name        TEXT NOT NULL DEFAULT '',
    text               TEXT NOT NULL,
    lang               TEXT NOT NULL DEFAULT 'und',
    score              INTEGER NOT NULL,
    category           TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'new',
    reason             TEXT NOT NULL DEFAULT '',
    matched_tags_json  TEXT NOT NULL DEFAULT '[]',
    tier0_score        REAL NOT NULL DEFAULT 0,
    llm_model          TEXT NOT NULL DEFAULT '',
    icebreakers_json   TEXT NOT NULL DEFAULT '[]',
    viewed_at          TEXT,
    profile_opened_at  TEXT,
    icebreaker_count   INTEGER NOT NULL DEFAULT 0,
    saved_time_seconds INTEGER NOT NULL DEFAULT 0,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    UNIQUE(tg_chat_id, msg_id)
);
CREATE INDEX IF NOT EXISTS idx_leads_feed ON leads(radar_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_leads_category ON leads(category, score DESC, created_at DESC);

CREATE TABLE IF NOT EXISTS trash_samples (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    radar_id          INTEGER NOT NULL REFERENCES radars(id) ON DELETE CASCADE,
    source            TEXT NOT NULL,
    reason            TEXT NOT NULL,
    matched_tags_json TEXT NOT NULL DEFAULT '[]',
    score             INTEGER NOT NULL DEFAULT 0,
    chat_title        TEXT NOT NULL DEFAULT '',
    author_id         INTEGER NOT NULL DEFAULT 0,
    text_excerpt      TEXT NOT NULL,
    lang              TEXT NOT NULL DEFAULT 'und',
    created_at        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_trash_samples_radar ON trash_samples(radar_id, created_at DESC);

CREATE TABLE IF NOT EXISTS tag_stats (
    radar_id            INTEGER NOT NULL REFERENCES radars(id) ON DELETE CASCADE,
    tag                 TEXT NOT NULL,
    total_hits          INTEGER NOT NULL DEFAULT 0,
    trash_hits          INTEGER NOT NULL DEFAULT 0,
    lead_hits           INTEGER NOT NULL DEFAULT 0,
    false_positive_hits INTEGER NOT NULL DEFAULT 0,
    throttled_until     TEXT,
    updated_at          TEXT NOT NULL,
    PRIMARY KEY(radar_id, tag)
);

CREATE TABLE IF NOT EXISTS daily_metrics (
    date                 TEXT NOT NULL,
    radar_id             INTEGER NOT NULL REFERENCES radars(id) ON DELETE CASCADE,
    messages_seen        INTEGER NOT NULL DEFAULT 0,
    tier0_passed         INTEGER NOT NULL DEFAULT 0,
    tier0_rejected       INTEGER NOT NULL DEFAULT 0,
    llm_called           INTEGER NOT NULL DEFAULT 0,
    trash_count          INTEGER NOT NULL DEFAULT 0,
    doubtful_count       INTEGER NOT NULL DEFAULT 0,
    potential_count      INTEGER NOT NULL DEFAULT 0,
    clear_count          INTEGER NOT NULL DEFAULT 0,
    viewed_count         INTEGER NOT NULL DEFAULT 0,
    profile_opened_count INTEGER NOT NULL DEFAULT 0,
    icebreaker_count     INTEGER NOT NULL DEFAULT 0,
    false_positive_count INTEGER NOT NULL DEFAULT 0,
    saved_time_seconds   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(date, radar_id)
);

CREATE TABLE IF NOT EXISTS dedup_fingerprints (
    fingerprint TEXT PRIMARY KEY,
    author_id   INTEGER NOT NULL DEFAULT 0,
    token_sig   TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_dedup_author ON dedup_fingerprints(author_id, expires_at DESC);
CREATE INDEX IF NOT EXISTS idx_dedup_expires ON dedup_fingerprints(expires_at);

CREATE TABLE IF NOT EXISTS consultant_messages (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    session_key   TEXT NOT NULL,
    role          TEXT NOT NULL,
    mode          TEXT NOT NULL DEFAULT 'chat',
    lead_id       INTEGER NOT NULL DEFAULT 0,
    content       TEXT NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_consultant_session ON consultant_messages(session_key, id ASC);
