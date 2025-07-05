package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
)

const indexName = "usach_chatbot_vectors_wos"

func main() {
	// Get Elasticsearch URL
	esURL := os.Getenv("ES_URL")
	if esURL == "" && len(os.Args) > 1 {
		esURL = os.Args[1]
	}
	if esURL == "" {
		log.Fatal("Please provide ES_URL as environment variable or first argument")
	}

	fmt.Printf("🔗 Connecting to Elasticsearch: %s\n", maskPassword(esURL))

	// Create ES client
	cfg := elasticsearch.Config{
		Addresses: []string{esURL},
	}
	
	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		log.Fatal("Error creating ES client:", err)
	}

	// Test connection
	res, err := es.Info()
	if err != nil {
		log.Fatal("Error connecting to ES:", err)
	}
	defer res.Body.Close()

	var info map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		log.Fatal("Error parsing ES info:", err)
	}

	fmt.Printf("✅ Connected to Elasticsearch %s\n", info["version"].(map[string]interface{})["number"])

	// Check if index exists
	res, err = es.Indices.Exists([]string{indexName})
	if err != nil {
		log.Fatal("Error checking index:", err)
	}
	
	if res.StatusCode == 200 {
		fmt.Printf("⚠️  Index '%s' already exists. Delete it? (y/N): ", indexName)
		var response string
		fmt.Scanln(&response)
		
		if strings.ToLower(response) == "y" {
			// Delete existing index
			res, err = es.Indices.Delete([]string{indexName})
			if err != nil {
				log.Fatal("Error deleting index:", err)
			}
			if res.IsError() {
				log.Fatal("Error deleting index:", res.String())
			}
			fmt.Println("🗑️  Index deleted")
		} else {
			fmt.Println("Keeping existing index. Exiting...")
			return
		}
	}

	// Create index with mapping
	mapping := `{
		"settings": {
			"number_of_shards": 1,
			"number_of_replicas": 0,
			"analysis": {
				"analyzer": {
					"spanish_analyzer": {
						"type": "standard",
						"stopwords": "_spanish_"
					}
				}
			}
		},
		"mappings": {
			"properties": {
				"MongoDocID": {
					"type": "keyword"
				},
				"ChunkText": {
					"type": "text",
					"analyzer": "spanish_analyzer"
				},
				"EmbeddingVector": {
					"type": "dense_vector",
					"dims": 768,
					"index": true,
					"similarity": "cosine"
				},
				"Title": {
					"type": "text",
					"analyzer": "spanish_analyzer"
				},
				"Authors": {
					"type": "text",
					"analyzer": "spanish_analyzer"
				},
				"PublicationDate": {
					"type": "date",
					"format": "yyyy-MM-dd||yyyy-MM||yyyy||strict_date_optional_time"
				},
				"Journal": {
					"type": "text",
					"analyzer": "spanish_analyzer"
				},
				"DOI": {
					"type": "keyword"
				},
				"Abstract": {
					"type": "text",
					"analyzer": "spanish_analyzer"
				},
				"Keywords": {
					"type": "text",
					"analyzer": "spanish_analyzer"
				},
				"FundingText": {
					"type": "text",
					"analyzer": "spanish_analyzer"
				},
				"AuthorDetails": {
					"type": "nested",
					"properties": {
						"Name": {
							"type": "text",
							"analyzer": "spanish_analyzer"
						},
						"Email": {
							"type": "keyword"
						},
						"Affiliation": {
							"type": "text",
							"analyzer": "spanish_analyzer"
						},
						"IsCorresponding": {
							"type": "boolean"
						},
						"IsUSACH": {
							"type": "boolean"
						}
					}
				}
			}
		}
	}`

	// Create index
	res, err = es.Indices.Create(
		indexName,
		es.Indices.Create.WithBody(strings.NewReader(mapping)),
		es.Indices.Create.WithContext(context.Background()),
	)
	if err != nil {
		log.Fatal("Error creating index:", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		body, _ := ioutil.ReadAll(res.Body)
		log.Fatal("Error creating index:", string(body))
	}

	fmt.Printf("✅ Index '%s' created successfully!\n", indexName)

	// Get index info
	res, err = es.Indices.Get([]string{indexName})
	if err != nil {
		log.Fatal("Error getting index info:", err)
	}
	defer res.Body.Close()

	var indexInfo map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&indexInfo); err != nil {
		log.Fatal("Error parsing index info:", err)
	}

	// Pretty print settings
	if idxData, ok := indexInfo[indexName].(map[string]interface{}); ok {
		if mappings, ok := idxData["mappings"].(map[string]interface{}); ok {
			if props, ok := mappings["properties"].(map[string]interface{}); ok {
				fmt.Printf("\n📊 Index properties:\n")
				for field := range props {
					fmt.Printf("   - %s\n", field)
				}
			}
		}
	}

	fmt.Println("\n🎉 Elasticsearch setup completed!")
	fmt.Println("Next steps:")
	fmt.Println("1. Update ES_URL in Railway variables")
	fmt.Println("2. Run the reindex script to populate with data from MongoDB")
}

func maskPassword(url string) string {
	if strings.Contains(url, "@") {
		parts := strings.Split(url, "@")
		if len(parts) == 2 && strings.Contains(parts[0], "://") {
			protocolParts := strings.Split(parts[0], "://")
			if len(protocolParts) == 2 {
				return protocolParts[0] + "://***:***@" + parts[1]
			}
		}
	}
	return url
}