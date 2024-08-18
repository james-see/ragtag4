CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS items (
    id SERIAL PRIMARY KEY,
    title TEXT,
    doc TEXT,
    embedding vector(4096)
);