package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"bytes"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/ledongthuc/pdf"
	"github.com/ollama/ollama/api"
	"github.com/pgvector/pgvector-go"
)

type Session struct {
	Messages    []api.Message
	TitleFilter string
}

var sessions = make(map[string]*Session)

func generateEmbedding(docText string) ([]float32, error) {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return nil, err
	}

	// Create an embedding request
	req := &api.EmbedRequest{
		Model: "llama3.1", // Ensure this is an embedding-capable model
		Input: docText,
	}

	// Call the Embed function
	resp, err := client.Embed(context.Background(), req)
	if err != nil {
		return nil, err
	}

	return resp.Embeddings[0], nil
}

func insertItem(conn *pgx.Conn, title string, docText string, embedding []float32) error {
	// Combine title and docText for embedding
	combinedText := title + " " + docText

	_, err := conn.Exec(context.Background(),
		"INSERT INTO items (title, doc, embedding) VALUES ($1, $2, $3)",
		title, combinedText, pgvector.NewVector(embedding))

	return err
}

func queryEmbeddings(conn *pgx.Conn, query string, session *Session) (string, []string, error) {
	// Generate embedding for the query
	queryEmbedding, err := generateEmbedding(query)
	if err != nil {
		return "", nil, err
	}

	// Prepare the SQL query
	sqlQuery := "SELECT doc, COALESCE(title, 'Untitled') FROM items"
	if session.TitleFilter != "" {
		sqlQuery += fmt.Sprintf(" WHERE title LIKE '%%%s%%'", session.TitleFilter)
	}
	sqlQuery += " ORDER BY embedding <-> $1 LIMIT 5"

	// Query the database for similar documents
	rows, err := conn.Query(context.Background(), sqlQuery, pgvector.NewVector(queryEmbedding))
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()

	var docs []string
	var sources []string
	for rows.Next() {
		var doc, title string
		if err := rows.Scan(&doc, &title); err != nil {
			return "", nil, err
		}
		docs = append(docs, doc)
		sources = append(sources, fmt.Sprintf("Source: %s", title))
	}

	// Combine the retrieved documents
	contextText := strings.Join(docs, "\n\n")

	// Create a chat request
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return "", nil, err
	}

	// Add the new query to the session
	session.Messages = append(session.Messages, api.Message{Role: "user", Content: query})

	// Prepare the messages for the chat request
	messages := []api.Message{
		{Role: "system", Content: "You are an assistant that answers questions based on the given context."},
		{Role: "user", Content: "Here's the context:\n" + contextText},
	}
	messages = append(messages, session.Messages...)

	req := &api.ChatRequest{
		Model:    "llama3.1",
		Messages: messages,
	}

	// Call the Chat function
	var response strings.Builder
	err = client.Chat(context.Background(), req, func(resp api.ChatResponse) error {
		response.WriteString(resp.Message.Content)
		return nil
	})
	if err != nil {
		return "", nil, err
	}

	// Add the AI response to the session
	session.Messages = append(session.Messages, api.Message{Role: "assistant", Content: response.String()})

	return response.String(), sources, nil
}

func getDocuments(conn *pgx.Conn) ([]map[string]interface{}, error) {
	rows, err := conn.Query(context.Background(), "SELECT DISTINCT ON (SPLIT_PART(title, '_chunk_', 1)) SPLIT_PART(title, '_chunk_', 1) as title, COUNT(*) as count FROM items GROUP BY SPLIT_PART(title, '_chunk_', 1)")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var documents []map[string]interface{}
	for rows.Next() {
		var title string
		var count int
		if err := rows.Scan(&title, &count); err != nil {
			return nil, err
		}
		documents = append(documents, map[string]interface{}{
			"title": title,
			"count": count,
		})
	}

	return documents, nil
}

func deleteDocument(conn *pgx.Conn, title string) error {
	_, err := conn.Exec(context.Background(), "DELETE FROM items WHERE title LIKE $1 || '%'", title)
	return err
}

func uploadDocument(c *gin.Context, conn *pgx.Conn) {
	title := c.PostForm("title")
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		log.Printf("Error getting file: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer file.Close()

	filename := filepath.Join("uploads", header.Filename)
	out, err := os.Create(filename)
	if err != nil {
		log.Printf("Error creating file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer out.Close()

	_, err = io.Copy(out, file)
	if err != nil {
		log.Printf("Error copying file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var textContent string
	if filepath.Ext(filename) == ".pdf" {
		f, r, err := pdf.Open(filename)
		if err != nil {
			log.Printf("Error opening PDF: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer f.Close()

		var buf bytes.Buffer
		b, err := r.GetPlainText()
		if err != nil {
			log.Printf("Error extracting text from PDF: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		buf.ReadFrom(b)
		textContent = buf.String()
	} else {
		content, err := os.ReadFile(filename)
		if err != nil {
			log.Printf("Error reading file: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		textContent = string(content)
	}

	chunks := chunkText(textContent, 4096)

	for i, chunk := range chunks {
		chunkTitle := fmt.Sprintf("%s_chunk_%d", title, i+1)
		embedding, err := generateEmbedding(chunk)
		if err != nil {
			log.Printf("Error generating embedding: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		err = insertItem(conn, chunkTitle, chunk, embedding)
		if err != nil {
			log.Printf("Error inserting item: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Document uploaded and processed successfully"})
}

func chunkText(text string, chunkSize int) []string {
	words := strings.Fields(text)
	var chunks []string
	for i := 0; i < len(words); i += chunkSize {
		end := i + chunkSize
		if end > len(words) {
			end = len(words)
		}
		chunks = append(chunks, strings.Join(words[i:end], " "))
	}
	return chunks
}

func main() {
	// Set up the database connection
	conn, err := pgx.Connect(context.Background(), "postgresql://jc:!1newmedia@localhost:5432/ragtag")
	if err != nil {
		log.Fatal("Unable to connect to database:", err)
	}
	defer conn.Close(context.Background())

	// Set up the Gin router
	r := gin.Default()

	// Define the /add_document endpoint
	r.POST("/add_document", func(c *gin.Context) {
		var request struct {
			Title   string `json:"title"`
			DocText string `json:"doc_text"`
		}
		if err := c.BindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Generate the embedding
		embedding, err := generateEmbedding(request.DocText)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Insert the document and its embedding into the items table
		err = insertItem(conn, request.Title, request.DocText, embedding)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Document chunk embedded and stored successfully!"})
	})

	// Add the new /query endpoint
	r.POST("/query", func(c *gin.Context) {
		var request struct {
			Query     string `json:"query"`
			SessionID string `json:"sessionId"`
		}
		if err := c.BindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		fmt.Printf("Received query: %s\n", request.Query)
		fmt.Printf("Session ID: %s\n", request.SessionID)

		session, ok := sessions[request.SessionID]
		if !ok {
			session = &Session{
				Messages: []api.Message{
					{Role: "system", Content: "You are an assistant that answers questions based on the given context."},
				},
				TitleFilter: "",
			}
			sessions[request.SessionID] = session
		}

		fmt.Printf("Current title filter: %s\n", session.TitleFilter)

		// Check for @title in the query
		if strings.Contains(request.Query, "@") {
			parts := strings.Split(request.Query, "@")
			if len(parts) > 1 {
				session.TitleFilter = strings.Split(parts[1], " ")[0]
				request.Query = strings.Replace(request.Query, "@"+session.TitleFilter, "", 1)
				fmt.Printf("New title filter set: %s\n", session.TitleFilter)
			}
		}

		result, sources, err := queryEmbeddings(conn, request.Query, session)
		if err != nil {
			fmt.Printf("Error in queryEmbeddings: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		fmt.Printf("Result: %s\n", result)
		fmt.Printf("Sources: %v\n", sources)

		c.JSON(http.StatusOK, gin.H{
			"response":       result,
			"sources":        sources,
			"titleFilter":    session.TitleFilter,
			"twitter_titles": []string{"Twitter", "Twitter API", "Twitter Terms of Service"},
		})
	})

	// Serve the index.html file
	r.GET("/", func(c *gin.Context) {
		c.File("index.html")
	})

	// Serve the docmanager.html file
	r.GET("/docmanager", func(c *gin.Context) {
		c.File("docmanager.html")
	})

	// Add a new endpoint to fetch documents
	r.GET("/documents", func(c *gin.Context) {
		documents, err := getDocuments(conn)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, documents)
	})

	// Add a new endpoint to delete documents
	r.POST("/delete_document", func(c *gin.Context) {
		var request struct {
			Title string `json:"title"`
		}
		if err := c.BindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		err := deleteDocument(conn, request.Title)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Document deleted successfully"})
	})

	// Add a new endpoint to upload documents
	r.POST("/upload_document", func(c *gin.Context) {
		uploadDocument(c, conn)
	})

	// Add a new endpoint to clear the chat session
	r.POST("/clear_session", func(c *gin.Context) {
		var request struct {
			SessionID string `json:"sessionId"`
		}
		if err := c.BindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		delete(sessions, request.SessionID)
		c.JSON(http.StatusOK, gin.H{"message": "Chat session cleared successfully"})
	})

	// Add a new endpoint to check if Twitter data exists
	r.GET("/check_data", func(c *gin.Context) {
		rows, err := conn.Query(context.Background(), "SELECT DISTINCT title FROM items WHERE title LIKE '%twitter%'")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var titles []string
		for rows.Next() {
			var title string
			if err := rows.Scan(&title); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			titles = append(titles, title)
		}

		c.JSON(http.StatusOK, gin.H{"twitter_titles": titles})
	})

	// Run the Gin server
	r.Run(":8080")
}
