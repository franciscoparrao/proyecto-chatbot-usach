package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/franciscoparrao/proyecto-chatbot-usach/backend/handlers" // Ajusta a tu path de módulo
	"github.com/gin-contrib/cors"                                        // Importar CORS
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	// "github.com/joho/godotenv" // Descomentar si usas .env
)

// Constantes y variables globales (simplificado para empezar)
// En una app más grande, esto iría en un paquete de config o similar
var (
	mongoClient    *mongo.Client
	mongoURI       string
	googleAPIKey   string
	dbName         = "investigacion_usach_db" // Nombre BD
	collectionName = "notas_investigacion"    // Nombre Colección
)

const (
	googleApiKeyEnvVar = "GOOGLE_API_KEY"
	mongoUriEnvVar     = "MONGO_URI"
)

func init() {
	// --- Cargar Configuración (Variables de Entorno) ---
	// godotenv.Load() // Descomentar si usas .env

	mongoURI = os.Getenv(mongoUriEnvVar)
	if mongoURI == "" {
		log.Fatalf("FATAL: Environment variable %s not set.", mongoUriEnvVar)
	}

	googleAPIKey = os.Getenv(googleApiKeyEnvVar)
	if googleAPIKey == "" {
		log.Fatalf("FATAL: Environment variable %s not set.", googleApiKeyEnvVar)
	}
	log.Println("Configuration loaded.")

	// --- Conectar a MongoDB ---
	log.Println("Connecting to MongoDB Atlas...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("FATAL: Failed to connect to MongoDB: %v", err)
	}

	// Verificar conexión
	err = client.Ping(ctx, readpref.Primary())
	if err != nil {
		log.Fatalf("FATAL: Failed to ping MongoDB: %v", err)
	}

	mongoClient = client // Guardar cliente para usar en handlers
	log.Println("Successfully connected and pinged MongoDB Atlas!")

	// NOTA: No desconectamos aquí, la conexión debe permanecer abierta para el servidor.
	// La desconexión elegante se maneja al apagar el servidor (más avanzado).
}

func main() {
	log.Println("Starting API server...")

	// --- Configurar Router Gin ---
	router := gin.Default()

	// Configurar CORS (Middleware) - ¡Importante para desarrollo con Vue!
	// Ajusta AllowOrigins según dónde corra tu frontend Vue (ej. localhost:8080, :5173, etc.)
	router.Use(cors.New(cors.Config{
		AllowAllOrigins:  true,                                                                  // Permite cualquier origen (NO USAR EN PRODUCCIÓN)
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},          // Asegúrate que POST y OPTIONS estén
		AllowHeaders:     []string{"Origin", "Content-Length", "Content-Type", "Authorization"}, // Mantén los headers necesarios
		AllowCredentials: true,                                                                  // Puedes probar quitando esto si no usas credenciales/cookies
		MaxAge:           12 * time.Hour,
	}))

	// --- Definir Rutas API ---
	api := router.Group("/api")
	{
		// Pasar el cliente Mongo y la API key al handler (una forma de hacerlo)
		chatHandler := handlers.NewChatHandler(mongoClient, dbName, collectionName, googleAPIKey)
		api.POST("/chat", chatHandler.HandleChatRequest)
	}

	// Ruta de prueba simple
	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	// --- Iniciar Servidor ---
	port := os.Getenv("PORT") // Para despliegues en la nube
	if port == "" {
		port = "8000" // Puerto por defecto para desarrollo local
	}
	log.Printf("Server listening on port %s\n", port)
	err := router.Run(":" + port)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
