package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/joho/godotenv"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esutil"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// --- Constantes y Configuracion ---
const (
	// Constantes relativas al archivo de entrada
	inputFile = "files/scraped_articles_usach.json"

	// Constantes relativas a los nombres de las variables de entorno
	mongoLocalURIEnvVar = "MONGO_URI"
	googleApiKeyEnvVar  = "GOOGLE_API_KEY"

	// Constantes relativas a ES
	esLocalURL  = "http://localhost:9200"
	esIndexName = "usach_chatbot_vectors"

	// Constantes relativas a Mongo
	dbName         = "investigacion_usach_db"
	collectionName = "notas_investigacion"

	// Constantes relativas a la configuracion de Gemini
	embeddingModel     = "models/text-embedding-004"
	googleApiEndpoint  = "https://generativelanguage.googleapis.com/v1beta/"
	minChunkLength     = 50
	rateLimitDelay     = 1 * time.Second
	bulkIndexerWorkers = 4
	bulkIndexerSize    = 100
)

// --- Estructuras ---
type ScrapedArticle struct {
	Title           string `json:"title"`
	Authors         string `json:"authors"`
	PublicationDate string `json:"publication_date"`
	RawText         string `json:"raw_text"`
}

type MongoDocument struct {
	OriginalTitle           string `bson:"originaltitle"`
	OriginalAuthors         string `bson:"originalauthors"`
	OriginalPublicationDate string `bson:"originalpublicationdate"`
	ChunkText               string `bson:"chunktext"`
}

type ElasticsearchDocument struct {
	MongoDocID              string    `json:"MongoDocID"`
	EmbeddingVector         []float32 `json:"EmbeddingVector"`
	OriginalTitle           string    `json:"OriginalTitle,omitempty"`
	OriginalAuthors         string    `json:"OriginalAuthors,omitempty"`
	OriginalPublicationDate string    `json:"OriginalPublicationDate,omitempty"`
}

type GoogleApiEmbeddingRequest struct {
	Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"content"`
}
type GoogleApiEmbeddingResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
}

// --- Funciones ---
func getEmbedding(text string, apiKey string) ([]float32, error) {
	apiURL := googleApiEndpoint + embeddingModel + ":embedContent?key=" + apiKey
	reqBody := GoogleApiEmbeddingRequest{}
	reqBody.Content.Parts = append(reqBody.Content.Parts, struct {
		Text string `json:"text"`
	}{Text: text})
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("error marshalling request body: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("error creating http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("error making POST request: %w", err)
	}
	defer resp.Body.Close()
	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var errorResp map[string]interface{}
		jsonErr := json.Unmarshal(respBodyBytes, &errorResp)
		if jsonErr == nil {
			return nil, fmt.Errorf("API error: status %d, response: %v", resp.StatusCode, errorResp)
		}
		return nil, fmt.Errorf("API error: status %d, response: %s", resp.StatusCode, string(respBodyBytes))
	}
	var apiResp GoogleApiEmbeddingResponse
	err = json.Unmarshal(respBodyBytes, &apiResp)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response JSON: %w. Response body: %s", err, string(respBodyBytes))
	}
	if len(apiResp.Embedding.Values) == 0 {
		return nil, fmt.Errorf("embedding vector not found or empty in API response")
	}
	return apiResp.Embedding.Values, nil
}

// --- Funcion Principal ---
func main() {
	// obtencion de variables de entorno
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("FATAL: Error loading .env file: %v\n", err)
	}

	mongoURI := os.Getenv(mongoLocalURIEnvVar)
	if mongoURI == "" {
		log.Printf("WARN: Environment variable %s not set. Defaulting to mongodb://localhost:27017\n", mongoLocalURIEnvVar)
		mongoURI = "mongodb://localhost:27017"
	} else {
		log.Println("Local MongoDB URI found from environment variable.")
	}

	googleAPIKey := os.Getenv(googleApiKeyEnvVar)
	if googleAPIKey == "" {
		log.Fatalf("FATAL: Environment variable %s not set.", googleApiKeyEnvVar)
	}
	log.Println("Google AI API Key found.")

	// lectura del archivo

	log.Printf("Reading scraper output file: %s\n", inputFile)
	jsonData, err := os.ReadFile(inputFile)
	if err != nil {
		log.Fatalf("FATAL: Error reading file %s: %v\n", inputFile, err)
	}
	var scrapedArticles []ScrapedArticle
	err = json.Unmarshal(jsonData, &scrapedArticles)
	if err != nil {
		log.Fatalf("FATAL: Error unmarshalling JSON from %s: %v\n", inputFile, err)
	}
	log.Printf("Successfully read %d articles from scraper output.\n", len(scrapedArticles))
	if len(scrapedArticles) == 0 {
		log.Println("No articles to process. Exiting.")
		return
	}

	// conexion a mongo

	log.Println("Connecting to local MongoDB...")
	mongoCtx, mongoCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer mongoCancel()
	mongoClient, err := mongo.Connect(mongoCtx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("FATAL: Failed to connect to MongoDB: %v", err)
	}
	defer func() {
		log.Println("Disconnecting from MongoDB...")
		disconnectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := mongoClient.Disconnect(disconnectCtx); err != nil {
			log.Printf("Error disconnecting from MongoDB: %v", err)
		} else {
			log.Println("Successfully disconnected from MongoDB.")
		}
	}()
	err = mongoClient.Ping(mongoCtx, readpref.Primary())
	if err != nil {
		log.Fatalf("FATAL: Failed to ping MongoDB: %v", err)
	}
	log.Println("Successfully connected to MongoDB!")
	collection := mongoClient.Database(dbName).Collection(collectionName)
	log.Printf("Using MongoDB database '%s' and collection '%s'\n", dbName, collectionName)

	// conexion a es
	log.Println("Connecting to local Elasticsearch...")
	esCfg := elasticsearch.Config{Addresses: []string{esLocalURL}}
	esClient, err := elasticsearch.NewClient(esCfg)
	if err != nil {
		log.Fatalf("FATAL: Error creating Elasticsearch client: %s", err)
	}
	res, err := esClient.Info()
	if err != nil {
		log.Fatalf("FATAL: Error getting Elasticsearch info: %s", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		log.Fatalf("FATAL: Elasticsearch connection error: %s", res.String())
	}
	log.Printf("Successfully connected to Elasticsearch: %s\n", res.String())

	bi, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{Index: esIndexName, Client: esClient, NumWorkers: bulkIndexerWorkers, FlushBytes: int(5e6), FlushInterval: 30 * time.Second})
	if err != nil {
		log.Fatalf("FATAL: Error creating Elasticsearch Bulk Indexer: %s", err)
	}
	defer func() {
		log.Println("Closing Elasticsearch Bulk Indexer...")
		if err := bi.Close(context.Background()); err != nil {
			log.Printf("ERROR: Failed to close Bulk Indexer: %s", err)
		} else {
			log.Println("Bulk Indexer closed.")
			stats := bi.Stats()
			log.Printf("  Elasticsearch Bulk Indexer Stats: Indexed: %d, Failed: %d\n", stats.NumIndexed, stats.NumFailed)
		}
	}()

	// procesamiento de articulos
	// lectura -> embedding -> mongo -> es

	var countSuccess uint64
	var countError uint64
	totalChunksProcessed := 0

	log.Printf("Starting processing pipeline for %d articles...\n", len(scrapedArticles))
	for articleIdx, article := range scrapedArticles {
		log.Printf("  Processing Article %d/%d: '%s'\n", articleIdx+1, len(scrapedArticles), article.Title)

		cleanedText := strings.TrimSpace(article.RawText)
		if cleanedText == "" {
			log.Printf("	ERROR: Empty text for article %d. Skipping.\n", articleIdx+1)
			continue
		}

		paragraphs := strings.Split(cleanedText, "\n\n")

		for chunkIdx, para := range paragraphs {
			chunkText := strings.TrimSpace(para)

			if len(chunkText) >= minChunkLength {
				totalChunksProcessed++
				log.Printf("    Chunk %d: Processing (Length: %d)...\n", chunkIdx+1, len(chunkText))

				// obtencion de embedding para el chunk del articulo leido
				log.Println("      Getting embedding...")
				vector, errEmbed := getEmbedding(chunkText, googleAPIKey)
				if errEmbed != nil {
					log.Printf("      ERROR getting embedding for chunk %d of article %d: %v. Skipping.\n", chunkIdx+1, articleIdx+1, errEmbed)
					atomic.AddUint64(&countError, 1)
					time.Sleep(rateLimitDelay)
					continue
				}
				log.Printf("      Embedding obtained (Vector size: %d)\n", len(vector))
				time.Sleep(rateLimitDelay)

				// insercion en mongo del documento
				log.Println("      Inserting text into MongoDB...")
				mongoDoc := MongoDocument{
					OriginalTitle:           article.Title,
					OriginalAuthors:         article.Authors,
					OriginalPublicationDate: article.PublicationDate,
					ChunkText:               chunkText,
				}
				insertCtx, insertMongoCancel := context.WithTimeout(context.Background(), 5*time.Second)
				resMongo, errMongo := collection.InsertOne(insertCtx, mongoDoc)
				insertMongoCancel()
				if errMongo != nil {
					log.Printf("      ERROR inserting chunk %d into MongoDB: %v. Skipping ES index.\n", chunkIdx+1, errMongo)
					atomic.AddUint64(&countError, 1)
					continue
				}
				mongoID := resMongo.InsertedID.(primitive.ObjectID)
				mongoIDString := mongoID.Hex()
				log.Printf("      Inserted into MongoDB with ID: %s\n", mongoIDString)

				// indexacion en mongo del documento
				log.Println("      Adding vector to Elasticsearch bulk indexer...")
				esDoc := ElasticsearchDocument{
					MongoDocID:              mongoIDString,
					EmbeddingVector:         vector,
					OriginalTitle:           article.Title,
					OriginalAuthors:         article.Authors,
					OriginalPublicationDate: article.PublicationDate,
				}
				data, errJson := json.Marshal(esDoc)
				if errJson != nil {
					log.Printf("      ERROR marshalling ES document for chunk %d (MongoID: %s): %v. Skipping ES index.\n", chunkIdx+1, mongoIDString, errJson)
					atomic.AddUint64(&countError, 1)
					continue
				}

				errBulk := bi.Add(context.Background(), esutil.BulkIndexerItem{Action: "index", Index: esIndexName, DocumentID: mongoIDString, Body: bytes.NewReader(data),
					OnFailure: func(ctx context.Context, item esutil.BulkIndexerItem, resp esutil.BulkIndexerResponseItem, err error) {
						atomic.AddUint64(&countError, 1)
						if err != nil {
							log.Printf("      ES BULK ERROR (Item %s): %s", item.DocumentID, err)
						} else {
							log.Printf("      ES BULK ERROR (Item %s): %s: %s", item.DocumentID, resp.Error.Type, resp.Error.Reason)
						}
					},
				})
				if errBulk != nil {
					log.Printf("      ERROR adding document ID %s to ES Bulk Indexer buffer: %v\n", mongoIDString, errBulk)
					atomic.AddUint64(&countError, 1)
				} else {
					atomic.AddUint64(&countSuccess, 1)
				}
				log.Println("      Added to ES bulk indexer.")

			}
		}
	}

	log.Println("Finished processing all articles.")
	log.Printf("Total Chunks Processed: %d. Added to ES Buffer: %d, Errors Logged: %d\n", totalChunksProcessed, countSuccess, countError)
	log.Println("Full Pipeline Script finished.")
}
