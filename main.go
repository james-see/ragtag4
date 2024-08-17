package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/ollama/ollama/api"
	"github.com/pgvector/pgvector-go"
)

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
	_, err := conn.Exec(context.Background(),
		"INSERT INTO items (title, doc, embedding) VALUES ($1, $2, $3)",
		title, docText, pgvector.NewVector(embedding))

	return err
}

func queryEmbeddings(conn *pgx.Conn, query string) (string, []string, error) {
	// Generate embedding for the query
	queryEmbedding, err := generateEmbedding(query)
	if err != nil {
		return "", nil, err
	}

	// Query the database for similar documents across all entries
	rows, err := conn.Query(context.Background(),
		"SELECT doc, COALESCE(title, 'Untitled') FROM items ORDER BY embedding <-> $1 LIMIT 5",
		pgvector.NewVector(queryEmbedding))
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
		sources = append(sources, fmt.Sprintf("Source: %s\n%s\n", title, doc[:100]+"..."))
	}

	// Combine the retrieved documents
	contextText := strings.Join(docs, "\n\n")

	// Create a chat request
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return "", nil, err
	}

	req := &api.ChatRequest{
		Model: "llama3.1",
		Messages: []api.Message{
			{Role: "system", Content: "You are an assistant that answers questions based on the given context."},
			{Role: "user", Content: contextText},
			{Role: "user", Content: query},
		},
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

	return response.String(), sources, nil
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
			Query string `json:"query"`
		}
		if err := c.BindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		fmt.Printf("Received query: %s\n", request.Query)

		result, sources, err := queryEmbeddings(conn, request.Query)
		if err != nil {
			fmt.Printf("Error in queryEmbeddings: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		fmt.Printf("Result: %s\n", result)
		fmt.Printf("Sources: %v\n", sources)

		c.JSON(http.StatusOK, gin.H{
			"response": result,
			"sources": sources,
		})
	})

	// Serve the index.html file
	r.GET("/", func(c *gin.Context) {
		c.File("index.html")
	})

	// Run the Gin server
	r.Run(":8080")
}