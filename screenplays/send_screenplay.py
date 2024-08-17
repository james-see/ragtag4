import os
import requests
import psycopg2
import math

def chunk_text(text, chunk_size=4096):
    words = text.split()
    return [' '.join(words[i:i+chunk_size]) for i in range(0, len(words), chunk_size)]

def is_screenplay_in_db(cursor, title):
    cursor.execute("SELECT COUNT(*) FROM items WHERE title LIKE %s", (f"{title}%",))
    return cursor.fetchone()[0] > 0

def send_screenplay(file_path, cursor):
    with open(file_path, 'r', encoding='utf-8') as file:
        content = file.read()

    title = os.path.splitext(os.path.basename(file_path))[0]
    
    if is_screenplay_in_db(cursor, title):
        print(f"Screenplay '{title}' already exists in the database. Skipping.")
        return

    chunks = chunk_text(content)

    for i, chunk in enumerate(chunks):
        payload = {
            "title": f"{title}_chunk_{i+1}",
            "doc_text": chunk
        }
        response = requests.post("http://localhost:8080/add_document", json=payload)
        print(f"Chunk {i+1} response: {response.status_code}")

def process_scripts_folder():
    conn = psycopg2.connect("dbname=ragtag user=jc password=!1newmedia host=localhost")
    cursor = conn.cursor()

    scripts_folder = "scripts"
    for filename in os.listdir(scripts_folder):
        if filename.endswith(".txt"):
            file_path = os.path.join(scripts_folder, filename)
            send_screenplay(file_path, cursor)

    cursor.close()
    conn.close()

if __name__ == "__main__":
    process_scripts_folder()