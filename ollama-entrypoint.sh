#!/bin/bash
set -e

# Start Ollama in the background
ollama serve &

# Wait for Ollama to be ready
until curl -s -o /dev/null -w "%{http_code}" http://localhost:11434/api/tags | grep -q "200"; do
  echo "Waiting for Ollama to be ready..."
  sleep 5
done

# Pull the llama3.1 model
ollama pull llama3.1

# Keep the container running
wait