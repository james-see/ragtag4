# what

RAG is easy! Run ollama llama3.1 in golang with a postgres database.

This is a simple example of how to use ollama with a postgres database to create a RAG system. It should be considered a starting point and not a full-featured system. It can be used and adapted for any data related use case for using llm's to answer questions about data.

- Cool feature:  
You can use the title as a category filter and if you add an '@' to the query, it will filter vector docs by that matched title, so for example: 

`Is @antibionic a good company?` and it sets only docs and vectors with antibionic in the title field as the ones for the entire chat session moving forward. You can see this in the screenshots below. This means you can have different types of documents and not be forced to chat with all of them at once.

It is setup on docker compose and is ready to go if you skip to that section below.

## gui

The gui includes a document manager to add and remove documents from a database and a chat interface to interact with the system. It is in the format of a single page application and is built with html, css, and javascript. The style is in the format of an emulated terminal with a black background and white / green text.

![Screenshot 2024-08-18 at 5 04 10 PM](https://github.com/user-attachments/assets/ea0b8b04-2dba-4e5c-88fd-037fe296be87)

![PNG image](https://github.com/user-attachments/assets/da8a5c78-7365-459d-9f69-76956dc276df)

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

`docker-compose up --build`  
This will pull the llama3.1 model and start the ollama server. It will also start the go server and the gui and connect to postgres and tie it all together.

## ports and versions

ollama - 11434  
go server - 8080  
postgres - 5432  

go version - 1.23.0  
ollama version - 1.10.0  
postgres version - 16.1 as the pgvector:pg16 docker image

This has not been tested on other versions but should work on other versions of the software if you know what you are doing.

## curl test no stream

```bash
curl http://localhost:11434/api/generate -d '{
  "model": "llama3.1",
  "prompt":"Why is the sky blue?",
  "stream": false
}'
```

### Helpers

- [downloader.py](screenplays/downloader.py) - downloads a screenplay from a given URL
- [send_screenplay.py](screenplays/send_screenplay.py) - sends a screenplay to the database
