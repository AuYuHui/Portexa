CREATE TABLE server_directory_evidence (
    sha256 BLOB PRIMARY KEY CHECK (length(sha256) = 32),
    source_url TEXT NOT NULL,
    fetched_at_ms INTEGER NOT NULL,
    body BLOB NOT NULL CHECK (length(body) BETWEEN 1 AND 2097152)
) STRICT;
