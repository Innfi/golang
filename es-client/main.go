package main

import (
	"log"
	"os"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(".env"); err != nil {
		log.Fatalf("Error read from .env")
	}

	cfg := elasticsearch.Config{
		CloudID: os.Getenv("ES_CLOUD_ID"),
		APIKey:  os.Getenv("ES_API_KEY"),
	}

	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		log.Fatalf("Error creating the client: %s", err)
	}

	// Create a new index
	indexName := "my_index"
	_, err = client.Indices.Create(indexName)
	if err != nil {
		log.Fatalf("Error creating index: %s", err)
	}

	log.Printf("Index '%s' created successfully", indexName)
}
