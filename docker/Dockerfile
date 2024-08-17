# Use an official Python 3.11 runtime as a parent image
FROM python:3.11-slim

# Set the working directory in the container
WORKDIR /app

# Copy the current directory contents into the container at /app
COPY . /app

# Install Ollama and any needed packages specified in requirements.txt
RUN pip install --no-cache-dir -r requirements.txt

# Install Ollama CLI (if Ollama is a Python package; otherwise, adjust as needed)
RUN pip install ollama

# Download and set Llama 3.1 as the default model
RUN ollama install llama-3.1 && \
    ollama set-default llama-3.1

# Expose the API port
EXPOSE 8000

# Run the Ollama server when the container launches
CMD ["ollama", "run", "--host", "0.0.0.0", "--port", "8000"]

