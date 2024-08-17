# what

run ollama llama3.1 in golang

## how

- create a table with a vector column
- create a function to generate an embedding for a given text
- create a function to query the table with an embedding and return the most similar texts

## curl add embedding example

```bash
curl -X POST http://localhost:8080/add_document \
     -H "Content-Type: application/json" \
     -d '{"doc_text": "INT. COFFEE SHOP - DAY\n\nJANE, 30s, sits at a corner table, typing furiously on her laptop. The cafe buzzes with quiet conversation.\n\nJOHN, 40s, enters, scanning the room. He spots Jane and approaches.\n\nJOHN\nMind if I join you?\n\nJane looks up, startled."}'
```

## curl query example

```bash
curl -X POST http://localhost:8080/query \
     -H "Content-Type: application/json" \
     -d '{"query": "What are the main characters in the screenplays that are in the coffeeshop?"}'
```

## sql table creation

```sql
CREATE DATABASE IF NOT EXISTS ragtag
CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE IF NOT EXISTS items (
    id SERIAL PRIMARY KEY,
    doc TEXT,
    embedding vector(4096)
);
```

## docker

`docker-compose up`
`docker exec -it ollama ollama pull llama3.1`

## curl test no stream

```bash
curl http://localhost:11434/api/generate -d '{
  "model": "llama3.1",
  "prompt":"Why is the sky blue?",
  "stream": false
}'
```
