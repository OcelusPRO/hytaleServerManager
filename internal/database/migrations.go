package database

// migrations are applied in order; the database's user_version is the
// number already applied. Never edit a released migration: add a new one.
var migrations = []string{
	// 1: error reports and players.
	`
CREATE TABLE error_issues (
	id          TEXT PRIMARY KEY,
	title       TEXT NOT NULL,
	culprit     TEXT NOT NULL DEFAULT '',
	plugin      TEXT NOT NULL DEFAULT '',
	level       TEXT NOT NULL,
	count       INTEGER NOT NULL,
	first_seen  INTEGER NOT NULL,
	last_seen   INTEGER NOT NULL,
	resolved    INTEGER NOT NULL DEFAULT 0,
	regressed   INTEGER NOT NULL DEFAULT 0,
	resolved_at INTEGER,
	last_event  TEXT NOT NULL -- JSON of the latest occurrence
);
CREATE INDEX error_issues_last_seen ON error_issues (last_seen);

CREATE TABLE error_occurrences (
	id       INTEGER PRIMARY KEY,
	issue_id TEXT NOT NULL REFERENCES error_issues (id) ON DELETE CASCADE,
	time     INTEGER NOT NULL,
	event_id TEXT NOT NULL DEFAULT '',
	message  TEXT NOT NULL DEFAULT '',
	release  TEXT NOT NULL DEFAULT '',
	launch   INTEGER
);
CREATE INDEX error_occurrences_issue ON error_occurrences (issue_id, id);
CREATE INDEX error_occurrences_time ON error_occurrences (time);

CREATE TABLE players (
	uuid             TEXT PRIMARY KEY, -- lower case
	name             TEXT NOT NULL DEFAULT '',
	first_seen       INTEGER,
	last_seen        INTEGER,
	connection_count INTEGER NOT NULL DEFAULT 0,
	playtime_seconds INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX players_name ON players (name COLLATE NOCASE);

CREATE TABLE player_names (
	uuid       TEXT NOT NULL REFERENCES players (uuid) ON DELETE CASCADE,
	name       TEXT NOT NULL,
	first_seen INTEGER,
	last_seen  INTEGER,
	PRIMARY KEY (uuid, name)
);

CREATE TABLE player_sessions (
	id          INTEGER PRIMARY KEY,
	uuid        TEXT NOT NULL REFERENCES players (uuid) ON DELETE CASCADE,
	name        TEXT NOT NULL DEFAULT '',
	save_id     TEXT NOT NULL DEFAULT '',
	joined_at   INTEGER NOT NULL,
	left_at     INTEGER,
	-- the manager stopped while the player was connected: the end is unknown
	interrupted INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX player_sessions_player ON player_sessions (uuid, joined_at);
CREATE INDEX player_sessions_time ON player_sessions (joined_at);

CREATE TABLE player_bans (
	id          INTEGER PRIMARY KEY,
	uuid        TEXT NOT NULL REFERENCES players (uuid) ON DELETE CASCADE,
	save_id     TEXT NOT NULL DEFAULT '',
	reason      TEXT NOT NULL DEFAULT '',
	banned_at   INTEGER NOT NULL,
	expires_on  INTEGER,
	unbanned_at INTEGER
);
CREATE INDEX player_bans_player ON player_bans (uuid, banned_at);

-- Moderation actions: made from the panel, or found in the world's files
-- (changes made through the game console).
CREATE TABLE player_actions (
	id      INTEGER PRIMARY KEY,
	uuid    TEXT, -- NULL when the target could not be identified
	name    TEXT NOT NULL DEFAULT '',
	action  TEXT NOT NULL,
	reason  TEXT NOT NULL DEFAULT '',
	save_id TEXT NOT NULL DEFAULT '',
	source  TEXT NOT NULL, -- panel | server
	at      INTEGER NOT NULL
);
CREATE INDEX player_actions_player ON player_actions (uuid, at);
CREATE INDEX player_actions_time ON player_actions (at);

-- Last known operators and whitelist of each world, to notice changes.
CREATE TABLE player_access (
	save_id TEXT NOT NULL,
	list    TEXT NOT NULL,
	uuid    TEXT NOT NULL,
	PRIMARY KEY (save_id, list, uuid)
);
`,

	// 2: whitelist, bans and operators kept by the manager for every world.
	`
CREATE TABLE access_entries (
	list       TEXT NOT NULL, -- bans | operators | whitelist
	uuid       TEXT NOT NULL,
	name       TEXT NOT NULL DEFAULT '',
	reason     TEXT NOT NULL DEFAULT '',
	added_at   INTEGER NOT NULL,
	expires_on INTEGER,
	PRIMARY KEY (list, uuid)
);

-- Flags of the shared lists (scope '') and of each world (scope = save id):
-- whitelist_enabled, and initialized for the shared lists.
CREATE TABLE access_flags (
	scope TEXT NOT NULL,
	key   TEXT NOT NULL,
	value TEXT NOT NULL,
	PRIMARY KEY (scope, key)
);
`,
}
