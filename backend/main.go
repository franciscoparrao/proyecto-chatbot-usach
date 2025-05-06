package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/elastic/go-elasticsearch/v8" // Importar cliente ES
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	// Ajusta este path a tu módulo real si es diferente
	"github.com/franciscoparrao/proyecto-chatbot-usach/backend/handlers"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// --- Variables Globales para Clientes ---
// Ahora necesitamos ambos clientes
var (
	mongoClient *mongo.Client
	esClient    *elasticsearch.Client // Cliente Elasticsearch
)

// --- Constantes y Variables de Configuración ---
var (
	mongoURI       string
	googleAPIKey   string
	esURL          string
	dbName         = "investigacion_usach_db" // Nombre BD Mongo
	collectionName = "notas_investigacion"    // Colección Mongo
	esIndexName    = "usach_chatbot_vectors"  // Índice ES
)

const (
	googleApiKeyEnvVar = "GOOGLE_API_KEY"
	mongoUriEnvVar     = "MONGO_URI" // Usaremos esta para la URI local de Mongo
	esURLEnvVar        = "ES_URL"
)

func init() {
	// --- Cargar Configuración ---
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

	// --- Conectar a MongoDB Local ---
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

	// --- Conectar a Elasticsearch Local ---

	log.Println("Connecting to local Elasticsearch...")

	esURL = os.Getenv(esURLEnvVar)
	if esURL == "" {
		log.Printf("WARN: Environment variable %s not set.", esURL)
	} else {
		log.Println("Local ES found from environment variable.")
	}

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

func main() {
	log.Println("Starting API server using local Mongo+ES backend...")

	// --- Configurar Router Gin ---
	router := gin.Default()
	router.Use(cors.New(cors.Config{ // Misma config CORS
		AllowOrigins:     []string{"http://localhost:8080", "http://localhost:5173", "http://localhost:80", "http://localhost"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Length", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// --- Definir Rutas API ---
	api := router.Group("/api")
	{
		// Pasar AMBOS clientes y nombres necesarios al handler
		chatHandler := handlers.NewChatHandler(mongoClient, esClient, dbName, collectionName, esIndexName, googleAPIKey)
		api.POST("/chat", chatHandler.HandleChatRequest)
	}

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	// --- Iniciar Servidor ---
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	log.Printf("Server listening on port %s\n", port)
	err := router.Run(":" + port)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
