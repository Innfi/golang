package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/joho/godotenv"
)

type TestDocument struct {
	Id        int32     `json:"id"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

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
	index_name := "my_index"
	_, err = client.Indices.Create(index_name)
	if err != nil {
		log.Fatalf("Error creating index: %s", err)
	}

	log.Printf("Index '%s' created successfully", index_name)

	test_doc := TestDocument{
		Id:        1,
		Message:   "document for index test",
		CreatedAt: time.Now(),
	}
	serialized, err := json.Marshal(test_doc)
	if err != nil {
		log.Fatalf("Error from Marshal(): %s\n", err)
	}

	_, index_err := client.Index(index_name, bytes.NewReader(serialized))
	if index_err != nil {
		log.Fatalf("Error writing document: %s\n", err)
	}

	/* not working */
	//
	// search_result, search_err := client.Search().
	// 	Index(index_name).
	// 	Request(&search.Request{
	// 		Query: &types.Query{
	// 			Match: map[string]types.MatchQuery{
	// 				"message": {Query: "Hello"},
	// 			},
	// 		},
	// 	}).Do(context.Background())
}
