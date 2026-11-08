package store

// migrations run in order; PRAGMA user_version records how many have applied.
var migrations = []string{`
CREATE TABLE meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE documents (
	id                TEXT PRIMARY KEY,
	collection        TEXT NOT NULL,
	rel_path          TEXT NOT NULL,
	title             TEXT NOT NULL,
	file_type         TEXT NOT NULL,
	size              INTEGER NOT NULL,
	mtime_ns          INTEGER NOT NULL,
	sha256            TEXT NOT NULL,
	status            TEXT NOT NULL,
	error             TEXT NOT NULL DEFAULT '',
	truncated         INTEGER NOT NULL DEFAULT 0,
	meta              TEXT NOT NULL DEFAULT '{}',
	extractor_version INTEGER NOT NULL,
	indexed_at        INTEGER NOT NULL,
	UNIQUE (collection, rel_path)
);
CREATE INDEX documents_status ON documents (collection, status);

-- Chunks are non-overlapping, so joining them in seq order rebuilds the text.
CREATE TABLE chunks (
	id        INTEGER PRIMARY KEY,
	doc_id    TEXT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
	seq       INTEGER NOT NULL,
	section   TEXT NOT NULL,
	title     TEXT NOT NULL,
	body      TEXT NOT NULL,
	embedding BLOB,
	UNIQUE (doc_id, seq)
);
CREATE INDEX chunks_unembedded ON chunks (id) WHERE embedding IS NULL;

CREATE VIRTUAL TABLE chunks_fts USING fts5(
	title, body,
	content = 'chunks', content_rowid = 'id',
	tokenize = 'porter unicode61 remove_diacritics 2'
);
CREATE TRIGGER chunks_ai AFTER INSERT ON chunks BEGIN
	INSERT INTO chunks_fts (rowid, title, body) VALUES (new.id, new.title, new.body);
END;
CREATE TRIGGER chunks_ad AFTER DELETE ON chunks BEGIN
	INSERT INTO chunks_fts (chunks_fts, rowid, title, body) VALUES ('delete', old.id, old.title, old.body);
END;

CREATE TABLE tokens (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	hash        TEXT NOT NULL UNIQUE,
	collections TEXT NOT NULL,
	created_at  INTEGER NOT NULL,
	expires_at  INTEGER,
	revoked_at  INTEGER
);

CREATE TABLE audit (
	id         INTEGER PRIMARY KEY,
	ts         INTEGER NOT NULL,
	token_id   TEXT NOT NULL,
	token_name TEXT NOT NULL,
	transport  TEXT NOT NULL,
	remote     TEXT NOT NULL,
	op         TEXT NOT NULL,
	query      TEXT NOT NULL,
	doc_ids    TEXT NOT NULL,
	outcome    TEXT NOT NULL
);
CREATE INDEX audit_ts ON audit (ts);
`}
