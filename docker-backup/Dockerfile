# golang image and setup for the gin server
FROM golang:1.23.0

# Set the working directory in the container
WORKDIR /app

# Copy the current directory contents into the container at /app
COPY . /app

# Install dependencies
RUN go mod download

# Build with verbose output
RUN go build -v -o ragtag

EXPOSE 8080

CMD ["./ragtag"]