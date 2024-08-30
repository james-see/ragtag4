package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	got "github.com/joho/godotenv"

	"bytes"
	"encoding/base64"
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/ollama/ollama/api"
	"github.com/pgvector/pgvector-go"
)

type Session struct {
	Messages    []api.Message
	TitleFilter string
}

var sessions = make(map[string]*Session)

func generateEmbedding(input string) ([]float32, error) {
	ollamaHost := os.Getenv("OLLAMA_HOST")
	if ollamaHost == "" {
		ollamaHost = "localhost" // fallback to localhost if not set
	}
	ollamaURL, err := url.Parse(fmt.Sprintf("http://%s:11434", ollamaHost))
	if err != nil {
		return nil, err
	}
	client := api.NewClient(ollamaURL, http.DefaultClient)

	// Create an embedding request
	req := &api.EmbedRequest{
		Model: "llama3.1", // Ensure this is an embedding-capable model
		Input: input,
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

func queryEmbeddings(conn *pgx.Conn, query string, session *Session, c *gin.Context) error {
	// Generate embedding for the query
	queryEmbedding, err := generateEmbedding(query)
	if err != nil {
		return err
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
		return err
	}
	defer rows.Close()

	var docs []string
	var sources []string
	for rows.Next() {
		var doc, title string
		if err := rows.Scan(&doc, &title); err != nil {
			return err
		}
		docs = append(docs, doc)
		sources = append(sources, fmt.Sprintf("Source: %s", title))
	}

	// Combine the retrieved documents
	contextText := strings.Join(docs, "\n\n")

	// Create a chat request
	ollamaHost := os.Getenv("OLLAMA_HOST")
	if ollamaHost == "" {
		ollamaHost = "localhost" // fallback to localhost if not set
	}
	ollamaURL, err := url.Parse(fmt.Sprintf("http://%s:11434", ollamaHost))
	if err != nil {
		return err
	}
	client := api.NewClient(ollamaURL, http.DefaultClient)

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
		Stream:   new(bool), // Use new(bool) to create a pointer to a boolean
	}
	*req.Stream = true // Set the value to true

	// Call the Chat function with streaming
	err = client.Chat(context.Background(), req, func(resp api.ChatResponse) error {
		// Send the raw content without any modifications
		if resp.Message.Content != "" {
			c.SSEvent("message", resp.Message.Content)
			c.Writer.Flush() // Ensure the content is sent immediately
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Add the AI response to the session
	session.Messages = append(session.Messages, api.Message{Role: "assistant", Content: "Response sent via streaming"})

	return nil
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

	// Create uploads directory if it doesn't exist
	uploadsDir := "uploads"
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		log.Printf("Error creating uploads directory: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create uploads directory"})
		return
	}

	filename := filepath.Join(uploadsDir, header.Filename)
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
	if filepath.Ext(filename) == ".jpg" || filepath.Ext(filename) == ".jpeg" || filepath.Ext(filename) == ".png" {
		// Generate image summary using the llava model
		summary, err := generateImageSummary(filename)
		if err != nil {
			log.Printf("Error generating image summary: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		textContent = summary
	} else {
		// Handle other file types (txt, pdf) as before
		content, err := os.ReadFile(filename)
		if err != nil {
			log.Printf("Error reading file: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		textContent = string(content)
	}

	// Generate embedding for the text content using llama3.1
	embedding, err := generateEmbedding(textContent)
	if err != nil {
		log.Printf("Error generating embedding: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Insert the document into the database
	err = insertItem(conn, title, textContent, embedding)
	if err != nil {
		log.Printf("Error inserting item: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
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

func generateImageSummary(imagePath string) (string, error) {
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to read image file: %w", err)
	}

	base64Image := base64.StdEncoding.EncodeToString(imageData)

	payload := map[string]interface{}{
		"model":  "llava",
		"prompt": "Describe this image in detail:",
		"images": []string{base64Image},
		"stream": true,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON payload: %w", err)
	}

	ollamaHost := os.Getenv("OLLAMA_HOST")
	if ollamaHost == "" {
		ollamaHost = "localhost"
	}
	url := fmt.Sprintf("http://%s:11434/api/generate", ollamaHost)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", fmt.Errorf("failed to send POST request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected response status: %d, body: %s", resp.StatusCode, string(body))
	}

	var summary strings.Builder
	decoder := json.NewDecoder(resp.Body)
	for {
		var result struct {
			Response string `json:"response"`
			Done     bool   `json:"done"`
		}
		if err := decoder.Decode(&result); err != nil {
			if err == io.EOF {
				break
			}
			return "", fmt.Errorf("failed to decode JSON response: %w", err)
		}
		summary.WriteString(result.Response)
		if result.Done {
			break
		}
	}

	if summary.Len() == 0 {
		return "", fmt.Errorf("empty response from llava model")
	}

	fmt.Println("The summary of the image is: ", summary.String())

	return summary.String(), nil
}

func main() {
	// Set up the database connection
	// load env variables
	got.Load()
	conn, err := pgx.Connect(context.Background(), os.Getenv("DB_URL"))
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

		// Check for @title in the query
		if strings.Contains(request.Query, "@") {
			parts := strings.Split(request.Query, "@")
			if len(parts) > 1 {
				session.TitleFilter = strings.Split(parts[1], " ")[0]
				request.Query = strings.Replace(request.Query, "@"+session.TitleFilter, "", 1)
			}
		}

		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		c.Header("Access-Control-Allow-Methods", "POST")
		c.Header("encoding", "chunked")

		err := queryEmbeddings(conn, request.Query, session, c)
		if err != nil {
			c.SSEvent("error", err.Error())
		}
		c.SSEvent("done", "")
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

	// Serve the describer.html file
	r.GET("/describer", func(c *gin.Context) {
		c.File("describer.html")
	})

	// Handle image description
	r.POST("/describe_image", func(c *gin.Context) {
		file, _, err := c.Request.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		defer file.Close()

		// Create a temporary file to store the uploaded image
		tempFile, err := os.CreateTemp("", "uploaded-*.jpg")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer os.Remove(tempFile.Name())
		defer tempFile.Close()

		// Copy the uploaded file to the temporary file
		_, err = io.Copy(tempFile, file)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		c.Header("Access-Control-Allow-Methods", "POST")
		c.Header("encoding", "chunked")

		imageData, err := os.ReadFile(tempFile.Name())
		if err != nil {
			c.SSEvent("error", err.Error())
			return
		}

		base64Image := base64.StdEncoding.EncodeToString(imageData)

		payload := map[string]interface{}{
			"model":  "llava",
			"prompt": "Describe this image in detail:",
			"images": []string{base64Image},
			"stream": true,
		}

		jsonPayload, err := json.Marshal(payload)
		if err != nil {
			c.SSEvent("error", err.Error())
			return
		}

		ollamaHost := os.Getenv("OLLAMA_HOST")
		if ollamaHost == "" {
			ollamaHost = "localhost"
		}
		url := fmt.Sprintf("http://%s:11434/api/generate", ollamaHost)

		resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
		if err != nil {
			c.SSEvent("error", err.Error())
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			c.SSEvent("error", fmt.Sprintf("Unexpected response status: %d, body: %s", resp.StatusCode, string(body)))
			return
		}

		decoder := json.NewDecoder(resp.Body)
		for {
			var result struct {
				Response string `json:"response"`
				Done     bool   `json:"done"`
			}
			if err := decoder.Decode(&result); err != nil {
				if err == io.EOF {
					break
				}
				c.SSEvent("error", err.Error())
				return
			}
			if result.Response != "" {
				c.SSEvent("message", result.Response)
				c.Writer.Flush() // Ensure the content is sent immediately
			}
			if result.Done {
				break
			}
		}

		c.SSEvent("done", "")
	})

	// Run the Gin server
	r.Run(":8080")
}
