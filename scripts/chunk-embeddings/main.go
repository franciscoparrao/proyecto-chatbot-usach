package main

import (
	"bytes"
	"context" // Necesario para el rate limiter
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync" // Necesario para WaitGroup
	"time"

	"golang.org/x/time/rate" // Importar el paquete rate limiter
)

// --- Constantes ---
const (
	inputFile          = "processed_chunks.json"
	outputFile         = "chunks_with_embeddings.json"
	googleApiKeyEnvVar = "GOOGLE_API_KEY"
	embeddingModel     = "models/text-embedding-004"
	googleApiEndpoint  = "https://generativelanguage.googleapis.com/v1beta/"

	// --- NUEVAS CONSTANTES PARA CONCURRENCIA ---
	numWorkers = 10 // Número de workers concurrentes (ajusta según tu QPM y máquina)
	// QPM (Queries Per Minute) permitido por la API. Ejemplo: 60 QPM
	// Ajusta esto al límite real de tu clave API (text-embedding suele permitir más)
	apiQPM = 600
	// Calculamos el límite por segundo para el rate limiter
	// rate.Limit es float64(eventos) / segundo
	rateLimitPerSecond = rate.Limit(float64(apiQPM) / 60.0)
	// Burst permite ráfagas cortas por encima del límite promedio (útil al inicio)
	// Un valor igual al número de workers suele ser razonable.
	rateLimitBurst = numWorkers
)

// --- Estructuras ---

type ProcessedChunk struct {
	OriginalURL     string    `json:"original_url"`
	OriginalTitle   string    `json:"original_title"`
	ChunkText       string    `json:"chunk_text"`
	EmbeddingVector []float32 `json:"embedding_vector,omitempty"`
}

// Estructura para enviar trabajos a los workers
type Job struct {
	Index int // Índice original del chunk en allChunks
	Text  string
}

// Estructura para recibir resultados de los workers
type Result struct {
	Index     int // Índice original
	Embedding []float32
	Err       error
}

// Se mantienen las estructuras de solicitud/respuesta de la API
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

// getEmbedding se mantiene igual, es llamada por los workers
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

	// Considerar añadir un timeout al cliente HTTP para producción
	// client := http.Client{Timeout: 30 * time.Second}
	// resp, err := client.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("error making POST request: %w", err)
	}
	defer resp.Body.Close()

	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	// Manejo de errores de API, incluyendo 429 Too Many Requests
	if resp.StatusCode != http.StatusOK {
		errorMsg := fmt.Sprintf("API error: status %d", resp.StatusCode)
		var errorResp map[string]interface{}
		jsonErr := json.Unmarshal(respBodyBytes, &errorResp)
		if jsonErr == nil {
			errorMsg = fmt.Sprintf("%s, response: %v", errorMsg, errorResp)
		} else {
			errorMsg = fmt.Sprintf("%s, response: %s", errorMsg, string(respBodyBytes))
		}
		// Específicamente útil si obtenemos 429
		if resp.StatusCode == http.StatusTooManyRequests {
			log.Printf("WARN: Received 429 Too Many Requests. Consider reducing numWorkers or apiQPM.")
		}
		return nil, fmt.Errorf(errorMsg)
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

// worker es la función que ejecutará cada goroutine
func worker(id int, apiKey string, limiter *rate.Limiter, jobs <-chan Job, results chan<- Result, wg *sync.WaitGroup) {
	defer wg.Done() // Asegura que decrementamos el contador del WaitGroup al salir
	log.Printf("Worker %d started\n", id)
	for job := range jobs {
		log.Printf("Worker %d processing job for chunk %d...\n", id, job.Index)

		// Esperar permiso del rate limiter antes de hacer la llamada
		err := limiter.Wait(context.Background()) // Usamos context.Background simple
		if err != nil {
			log.Printf("Worker %d rate limiter error: %v\n", id, err)
			// Enviar resultado con error
			results <- Result{Index: job.Index, Err: fmt.Errorf("rate limiter error: %w", err)}
			continue // Pasar al siguiente job
		}

		// Llamar a la función de embedding
		vector, err := getEmbedding(job.Text, apiKey)

		// Crear y enviar el resultado (sea éxito o error)
		result := Result{
			Index:     job.Index,
			Embedding: vector,
			Err:       err, // err será nil si getEmbedding tuvo éxito
		}
		results <- result // Enviar resultado de vuelta al canal results

		if err != nil {
			log.Printf("Worker %d ERROR embedding chunk %d: %v\n", id, job.Index, err)
		} else {
			log.Printf("Worker %d successfully embedded chunk %d (Vector size: %d)\n", id, job.Index, len(vector))
		}
	}
	log.Printf("Worker %d finished\n", id)
}

func main() {
	log.Println("Starting concurrent embedding script...")

	apiKey := os.Getenv(googleApiKeyEnvVar)
	if apiKey == "" {
		log.Fatalf("FATAL: Environment variable %s not set. Please set your Google AI API Key.", googleApiKeyEnvVar)
	}
	log.Println("Google AI API Key found.")

	log.Printf("Reading input file: %s\n", inputFile)
	jsonData, err := os.ReadFile(inputFile)
	if err != nil {
		log.Fatalf("FATAL: Error reading file %s: %v\n", inputFile, err)
	}

	var allChunks []ProcessedChunk
	err = json.Unmarshal(jsonData, &allChunks)
	if err != nil {
		log.Fatalf("FATAL: Error unmarshalling JSON from %s: %v\n", inputFile, err)
	}
	numChunks := len(allChunks)
	log.Printf("Successfully read %d chunks from JSON.\n", numChunks)

	if numChunks == 0 {
		log.Println("No chunks to process. Exiting.")
		return
	}

	// Crear el rate limiter global
	// rateLimitPerSecond eventos por segundo, permitiendo ráfagas de hasta rateLimitBurst
	limiter := rate.NewLimiter(rateLimitPerSecond, rateLimitBurst)
	log.Printf("Rate limiter configured: %.2f calls/sec, burst %d\n", rateLimitPerSecond, rateLimitBurst)

	// Crear canales para jobs y results
	// jobs: Buffer pequeño (e.g., numWorkers) para no bloquear al enviar si los workers están ocupados.
	jobs := make(chan Job, numWorkers)
	// results: Buffer grande (numChunks) para no bloquear a los workers al escribir resultados.
	results := make(chan Result, numChunks)

	// WaitGroup para esperar a que terminen los workers
	var wg sync.WaitGroup

	// --- Lanzar los Workers ---
	log.Printf("Starting %d workers...\n", numWorkers)
	for w := 1; w <= numWorkers; w++ {
		wg.Add(1) // Incrementar contador por cada worker lanzado
		// Pasar id, apiKey, limiter, canales y wg a cada worker
		go worker(w, apiKey, limiter, jobs, results, &wg)
	}

	// --- Enviar Jobs a los Workers ---
	log.Printf("Sending %d jobs to workers...\n", numChunks)
	startTime := time.Now()
	// Iteramos sobre los chunks originales y enviamos un Job por cada uno
	for i, chunk := range allChunks {
		jobs <- Job{Index: i, Text: chunk.ChunkText}
	}
	close(jobs) // ¡IMPORTANTE! Cerrar el canal jobs indica que no hay más trabajo
	log.Println("All jobs sent.")

	// --- Esperar a que todos los Workers Terminen ---
	// Esto no bloquea la recolección de resultados, sólo espera a que wg.Done() sea llamado
	// el número correcto de veces (numWorkers).
	log.Println("Waiting for workers to finish...")
	wg.Wait()
	close(results) // ¡IMPORTANTE! Cerrar results DESPUÉS de que los workers terminaron
	log.Println("All workers finished.")

	// --- Recolectar y Procesar Resultados ---
	// Ahora podemos leer del canal results hasta que se cierre.
	log.Println("Collecting results...")
	successfulEmbeddings := 0
	failedEmbeddings := 0
	// Iterar sobre los resultados recibidos (el bucle termina cuando 'results' se cierra)
	for result := range results {
		if result.Err != nil {
			// Ya logueamos el error específico en el worker, aquí sólo contamos
			failedEmbeddings++
			// No asignamos nada a allChunks[result.Index].EmbeddingVector, se quedará vacío/nil
		} else {
			// Asignar el embedding al chunk original usando el índice
			allChunks[result.Index].EmbeddingVector = result.Embedding
			successfulEmbeddings++
		}
	}
	processingTime := time.Since(startTime)

	log.Printf("Result collection complete.")
	log.Printf("Embedding complete. Time elapsed: %v\n", processingTime)
	log.Printf("Successfully embedded: %d chunks.\n", successfulEmbeddings)
	log.Printf("Failed to embed: %d chunks.\n", failedEmbeddings)

	// --- Filtrar y Guardar Chunks Exitosos (Igual que antes, pero ahora usando allChunks modificado) ---
	chunksToSave := []ProcessedChunk{}
	for _, chunk := range allChunks {
		if len(chunk.EmbeddingVector) > 0 { // Sólo guardar los que tienen embedding
			chunksToSave = append(chunksToSave, chunk)
		}
	}

	if len(chunksToSave) > 0 {
		log.Printf("Writing %d chunks with embeddings to %s...\n", len(chunksToSave), outputFile)
		chunkData, err := json.MarshalIndent(chunksToSave, "", "  ")
		if err != nil {
			log.Fatalf("FATAL: Failed to marshal chunks with embeddings to JSON: %v\n", err)
		}

		err = os.WriteFile(outputFile, chunkData, 0644)
		if err != nil {
			log.Fatalf("FATAL: Failed to write JSON to file %s: %v\n", outputFile, err)
		}
		log.Printf("Successfully wrote chunks with embeddings to %s\n", outputFile)
	} else {
		log.Println("No chunks were successfully embedded. No output file generated.")
	}

	log.Println("Concurrent embedding script finished.")
}
