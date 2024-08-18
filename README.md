# what

RAG is easy! Run ollama llama3.1 in golang with a postgres database.

This is a simple example of how to use ollama with a postgres database to create a RAG system. It should be considered a starting point and not a full-featured system. It can be used and adapted for any data related use case for using llm's to answer questions about data.

## gui

The gui includes a document manager to add and remove documents from a database and a chat interface to interact with the system. It is in the format of a single page application and is built with html, css, and javascript. The style is in the format of an emulated terminal with a black background and white / green text.

## key files

- [index.html](index.html) - a simple html gui to interact with the system
- [main.go](main.go) - the main go file to interact with the system

## how

- create a table with a vector column
- create a function to generate an embedding for a given text
- create a function to query the table with an embedding and return the most similar texts
-- create a script to download screenplays
-- create a script to send screenplays to the database and auto-embed against with the llm

## curl add embedding example with title and doc

```bash
curl -X POST http://localhost:8080/add_document \
     -H "Content-Type: application/json" \
     -d '{"title": "Screenplay Title", "doc_text": "INT. COFFEE SHOP - DAY\n\nJANE, 30s, sits at a corner table, typing furiously on her laptop. The cafe buzzes with quiet conversation.\n\nJOHN, 40s, enters, scanning the room. He spots Jane and approaches.\n\nJOHN\nMind if I join you?\n\nJane looks up, startled."}'
```

## curl upload document example

```bash
curl -X POST http://localhost:8080/upload_document \
  -H "Content-Type: multipart/form-data" \
  -F "title=Example Document" \
  -F "file=@/path/to/your/document.pdf"
```

## curl query example

```bash
curl -X POST http://localhost:8080/query \
     -H "Content-Type: application/json" \
     -d '{"query": "What are the main characters in the screenplays that are in the coffeeshop?"}'
```

## filter query example by title

```bash
curl -X POST http://localhost:8080/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "@screenplay Tell me about the main characters",
    "sessionId": "1234567890"
  }'
```

## sql table creation

```sql
CREATE DATABASE IF NOT EXISTS ragtag
CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE IF NOT EXISTS items (
    id SERIAL PRIMARY KEY,
    title TEXT,
    doc TEXT,
    embedding vector(4096)
);
```

## docker compose

`docker-compose up`  
`docker exec -it ollama ollama pull llama3.1`  
_note: ollama runs on port 11434 and the gui is on port 8080_

## curl test no stream

```bash
curl http://localhost:11434/api/generate -d '{
  "model": "llama3.1",
  "prompt":"Why is the sky blue?",
  "stream": false
}'
```

## Helpers

- [downloader.py](screenplays/downloader.py) - downloads a screenplay from a given URL
- [send_screenplay.py](screenplays/send_screenplay.py) - sends a screenplay to the database

