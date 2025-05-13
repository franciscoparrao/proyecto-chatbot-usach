package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/elastic/go-elasticsearch/v8" 
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/franciscoparrao/proyecto-chatbot-usach/backend/handlers"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// --- Variables Globales ---
var (
	// variables de entorno
	mongoURI       string
	googleAPIKey   string
	esURL          string

	// clientes de MongoDB y Elasticsearch
	mongoClient *mongo.Client
	esClient    *elasticsearch.Client 
)

const (
	// constantes relativas a MongoDB
	dbName         = "investigacion_usach_db" // nombre bd 
	collectionName = "notas_investigacion"    // nombre coleccion bd

	// constantes relativas a Elasticsearch
	esIndexName    = "usach_chatbot_vectors"  // nombre del indice a buscar

	// constantes relativas a los nombres de las variables de entorno
	googleApiKeyEnvVar = "GOOGLE_API_KEY"
	mongoUriEnvVar     = "MONGO_URI" 
	esURLEnvVar        = 	"ES_URL"
)

// -- Funcion init --
func init() {
	// obtencion de variables de entorno 
	mongoURI = os.Getenv(mongoUriEnvVar)
	if mongoURI == "" {
		log.Printf("WARN: Environment variable %s not set. Defaulting to mongodb://localhost:27017\n", mongoUriEnvVar)
		mongoURI = "mongodb://localhost:27017" // Usar default si no está la variable
	} else {
		log.Println("Local MongoDB URI found from environment variable.")
	}

	googleAPIKey = os.Getenv(googleApiKeyEnvVar)
	if googleAPIKey == "" {
		log.Fatalf("FATAL: Environment variable %s not set.", googleApiKeyEnvVar)
	}
	log.Println("Configuration loaded.")

	esURL = os.Getenv(esURLEnvVar)
	if esURL == "" {
		log.Printf("WARN: Environment variable %s not set.", esURL)
	} else {
		log.Println("Local ES found from environment variable.")
	}

	// conexion a MongoDB
	log.Println("Connecting to local MongoDB...")
	mongoCtx, mongoCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer mongoCancel()
	client, err := mongo.Connect(mongoCtx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("FATAL: Failed to connect to MongoDB: %v", err)
	}
	err = client.Ping(mongoCtx, readpref.Primary())
	if err != nil {
		log.Fatalf("FATAL: Failed to ping MongoDB: %v. Check URI: %s", err, mongoURI)
	}
	mongoClient = client // Guardar cliente Mongo local
	log.Println("Successfully connected and pinged local MongoDB!")

	// --- Conexion a Elasticsearch ---

	log.Println("Connecting to local Elasticsearch...")

	esCfg := elasticsearch.Config{
		Addresses: []string{esURL},
		// No se necesita usuario/pass si lo deshabilitamos en docker-compose
	}
	esCl, err := elasticsearch.NewClient(esCfg)
	if err != nil {
		log.Fatalf("FATAL: Error creating Elasticsearch client: %s", err)
	}
	res, err := esCl.Info()
	if err != nil {
		log.Fatalf("FATAL: Error getting Elasticsearch info: %s", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		log.Fatalf("FATAL: Elasticsearch connection error: %s", res.String())
	}
	esClient = esCl // Guardar cliente Elasticsearch
	log.Printf("Successfully connected to Elasticsearch: %s\n", res.String())

}

// -- Funcion main --
func main() {
	log.Println("Starting API server using local Mongo+ES backend...")

	// obtencion de variable de entorno PORT
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	// configuracion gin
	router := gin.Default()

	// configuracion cors
	router.Use(cors.New(cors.Config{ 
		AllowOrigins:     []string{"http://localhost:8080", "http://localhost:5173", "http://localhost:80", "http://localhost", "http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Length", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// definicion de la ruta 
	api := router.Group("/api")
	{
		chatHandler := handlers.NewChatHandler(mongoClient, esClient, dbName, collectionName, esIndexName, googleAPIKey)
		api.POST("/chat", chatHandler.HandleChatRequest)
	}

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})


	log.Printf("Server listening on port %s\n", port)
	err := router.Run(":" + port)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
