package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time" // Necesitamos time para el context

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// --- Constantes ---
const (
	inputFile      = "chunks_with_embeddings.json" // Leemos el archivo con los vectores
	mongoUriEnvVar = "MONGO_URI"                   // Variable de entorno para la cadena de conexión de Atlas
	dbName         = "investigacion_usach_db"      // Nombre de nuestra BD
	collectionName = "notas_investigacion"         // Nombre de nuestra colección
)

// --- Estructuras ---

// ProcessedChunk debe coincidir con la estructura guardada en chunks_with_embeddings.json
type ProcessedChunk struct {
	OriginalURL     string    `json:"original_url"`
	OriginalTitle   string    `json:"original_title"`
	ChunkText       string    `json:"chunk_text"`
	EmbeddingVector []float32 `json:"embedding_vector,omitempty" bson:"EmbeddingVector"` // Añadimos tag bson para MongoDB
}

// --- Funciones ---
// Ya no necesitamos getEmbedding aquí

func main() {
	log.Println("Starting MongoDB insertion script...")

	// --- Leer Variable de Entorno para MongoDB URI ---
	mongoURI := os.Getenv(mongoUriEnvVar)
	if mongoURI == "" {
		log.Fatalf("FATAL: Environment variable %s not set. Please set your MongoDB Atlas connection string.", mongoUriEnvVar)
	}
	log.Println("MongoDB connection string found.")

	// --- 1. Leer el archivo JSON con chunks y embeddings ---
	log.Printf("Reading input file: %s\n", inputFile)
	jsonData, err := os.ReadFile(inputFile)
	if err != nil {
		log.Fatalf("FATAL: Error reading file %s: %v\n", inputFile, err)
	}

	// --- 2. Decodificar JSON en structs ---
	var allChunks []ProcessedChunk
	err = json.Unmarshal(jsonData, &allChunks)
	if err != nil {
		log.Fatalf("FATAL: Error unmarshalling JSON from %s: %v\n", inputFile, err)
	}
	log.Printf("Successfully read %d chunks with embeddings from JSON.\n", len(allChunks))

	if len(allChunks) == 0 {
		log.Println("No chunks to insert. Exiting.")
		return
	}

	// --- 3. Conectar a MongoDB Atlas ---
	log.Println("Connecting to MongoDB Atlas...")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second) // Timeout de conexión
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("FATAL: Failed to connect to MongoDB: %v", err)
	}

	// Deferir desconexión para que se ejecute al final de main
	defer func() {
		log.Println("Disconnecting from MongoDB...")
		disconnectCtx, disconnectCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer disconnectCancel()
		if err := client.Disconnect(disconnectCtx); err != nil {
			log.Printf("Error disconnecting from MongoDB: %v", err)
		} else {
			log.Println("Successfully disconnected from MongoDB.")
		}
	}()

	// Verificar la conexión haciendo ping
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second) // Timeout para el ping
	defer pingCancel()
	err = client.Ping(pingCtx, readpref.Primary())
	if err != nil {
		log.Fatalf("FATAL: Failed to ping MongoDB: %v. Check connection string, network access, and credentials.", err)
	}
	log.Println("Successfully connected and pinged MongoDB Atlas!")

	// --- 4. Obtener manejador de la colección ---
	collection := client.Database(dbName).Collection(collectionName)
	log.Printf("Using database '%s' and collection '%s'\n", dbName, collectionName)

	// --- 5. Insertar cada chunk en la colección ---
	log.Printf("Starting insertion of %d chunks into MongoDB...\n", len(allChunks))
	insertedCount := 0
	for i, chunk := range allChunks {
		insertCtx, insertCancel := context.WithTimeout(context.Background(), 10*time.Second) // Timeout por inserción

		_, err := collection.InsertOne(insertCtx, chunk)
		if err != nil {
			// Loguear error pero continuar con los siguientes chunks
			log.Printf("  ERROR inserting chunk %d/%d (URL: %s): %v\n", i+1, len(allChunks), chunk.OriginalURL, err)
		} else {
			insertedCount++
			if (i+1)%10 == 0 { // Loguear progreso cada 10 chunks
				log.Printf("  Inserted %d/%d chunks...\n", i+1, len(allChunks))
			}
		}
		insertCancel() // Cancelar el contexto de esta inserción específica
	}

	log.Printf("Insertion complete. Successfully inserted %d out of %d chunks.\n", insertedCount, len(allChunks))
	log.Println("MongoDB insertion script finished.")
}
