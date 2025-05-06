package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8" // Importar cliente ES
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive" // Para convertir IDs
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// --- Constantes ---
const (
	embeddingModel    = "models/text-embedding-004"
	llmModel          = "models/gemini-1.5-flash-latest" // O el modelo que elegiste
	googleApiEndpoint = "https://generativelanguage.googleapis.com/v1beta/"
	// vectorIndexName -> Ahora se pasa al handler
	numCandidates = 100 // num_candidates para k-NN en ES
	numResults    = 3   // Cuántos resultados finales queremos
)

// --- Estructuras API Chat ---
type ChatRequest struct {
	Query string `json:"query" binding:"required"`
}
type ChatResponse struct {
	Response      string   `json:"response"`
	RetrievedDocs []string `json:"retrieved_docs,omitempty"`
}

// --- Estructuras Google Embedding --- (Sin cambios)
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

// --- Estructuras Google Gemini API --- (Sin cambios)
// --- Estructuras Google Gemini API ---
type GeminiApiRequest struct {
	Contents         []GeminiContent        `json:"contents"` // <--- AÑADE O CORRIGE ESTA ETIQUETA
	SafetySettings   []GeminiSafetySetting  `json:"safetySettings,omitempty"`
	GenerationConfig GeminiGenerationConfig `json:"generationConfig,omitempty"`
}

type GeminiContent struct {
	Parts []GeminiPart `json:"parts"`
}
type GeminiPart struct {
	Text string `json:"text"`
}
type GeminiSafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}
type GeminiGenerationConfig struct {
	Temperature     float32 `json:"temperature,omitempty"`
	TopK            int     `json:"topK,omitempty"`
	TopP            float32 `json:"topP,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}
type GeminiApiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []GeminiPart `json:"parts"`
			Role  string       `json:"role"`
		} `json:"content"`
		FinishReason  string
		SafetyRatings []GeminiSafetySetting
	}
	PromptFeedback struct{ SafetyRatings []GeminiSafetySetting }
}

// --- Estructura para Resultados de Elasticsearch ---
// Ajustada para la respuesta de ES Search API
type EsHit struct {
	ID     string          `json:"_id"` // ID del documento en ES (será el MongoDocID)
	Score  float64         `json:"_score"`
	Source json.RawMessage `json:"_source"` // Dejamos _source como Raw JSON por ahora
}
type EsSearchResponse struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		MaxScore float64 `json:"max_score"`
		Hits     []EsHit `json:"hits"`
	} `json:"hits"`
}

// Struct auxiliar para extraer metadatos del _source de ES si los incluimos
type EsSourceData struct {
	OriginalTitle string `json:"OriginalTitle"`
	OriginalURL   string `json:"OriginalURL"`
}

// --- Estructura para Resultados de MongoDB ---
// Usaremos esta para recuperar el texto basado en el ID
type MongoResult struct {
	ID            primitive.ObjectID `bson:"_id"`
	ChunkText     string             `bson:"chunktext"`
	OriginalTitle string             `bson:"originaltitle"` // Nombres en minúscula como probablemente se guardaron
}

// --- ChatHandler ---
// Modificado para incluir cliente ES y nombre de índice ES
type ChatHandler struct {
	mongoClient    *mongo.Client
	esClient       *elasticsearch.Client // Añadido cliente ES
	dbName         string
	collectionName string
	esIndexName    string // Añadido nombre índice ES
	googleAPIKey   string
}

// NewChatHandler - Modificado para recibir cliente ES y nombre índice ES
func NewChatHandler(mongoCli *mongo.Client, esCli *elasticsearch.Client, db string, coll string, esIndex string, apiKey string) *ChatHandler {
	return &ChatHandler{
		mongoClient:    mongoCli,
		esClient:       esCli,
		dbName:         db,
		collectionName: coll,
		esIndexName:    esIndex,
		googleAPIKey:   apiKey,
	}
}

// HandleChatRequest - Lógica principal modificada para Mongo+ES
func (h *ChatHandler) HandleChatRequest(c *gin.Context) {
	var request ChatRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Printf("Error binding JSON: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}
	log.Printf("Received query: %s\n", request.Query)

	// --- Lógica RAG ---
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// 1. Obtener Embedding para la pregunta del usuario (sin cambios)
	queryVector, err := h.getEmbedding(request.Query)
	if err != nil {
		log.Printf("Error getting query embedding: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process query embedding"})
		return
	}
	log.Printf("Successfully obtained query vector (size: %d)\n", len(queryVector))

	// --- 2. Búsqueda Vectorial en Elasticsearch ---
	log.Println("Performing Elasticsearch k-NN search...")

	// Construir la query k-NN para Elasticsearch
	esQuery := map[string]interface{}{
		"knn": map[string]interface{}{
			"field":          "EmbeddingVector", // Campo vectorial en ES
			"query_vector":   queryVector,
			"k":              numCandidates, // k > numResults
			"num_candidates": numCandidates,
		},
		"_source": false,                                                  // No necesitamos _source aquí, solo _id y _score
		"fields":  []string{"MongoDocID", "OriginalTitle", "OriginalURL"}, // Pedir campos específicos si están en ES source
		"size":    numResults,                                             // Limitar resultados finales
	}

	// Si no incluiste metadatos en ES, usa _source: false y pide solo score
	// esQuery := map[string]interface{}{
	// 	"knn": map[string]interface{}{
	// 		"field":         "EmbeddingVector",
	// 		"query_vector":  queryVector,
	// 		"k":             numCandidates,
	// 		"num_candidates": numCandidates,
	// 	},
	// 	"_source": false,
	//  "fields": []string{}, // No pedir campos si no están
	// 	"size":    numResults,
	// }

	var esBuffer bytes.Buffer
	if err := json.NewEncoder(&esBuffer).Encode(esQuery); err != nil {
		log.Printf("Error encoding Elasticsearch query: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to build search query"})
		return
	}

	// Ejecutar la búsqueda en Elasticsearch
	res, err := h.esClient.Search(
		h.esClient.Search.WithContext(ctx),
		h.esClient.Search.WithIndex(h.esIndexName),
		h.esClient.Search.WithBody(&esBuffer),
		h.esClient.Search.WithTrackTotalHits(true),
		// h.esClient.Search.WithPretty(), // Para debug
	)
	if err != nil {
		log.Printf("Error during Elasticsearch search request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed during document search"})
		return
	}
	defer res.Body.Close()

	if res.IsError() {
		bodyBytes, _ := io.ReadAll(res.Body)
		log.Printf("Elasticsearch search returned error: %s\nBody: %s", res.String(), string(bodyBytes))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error retrieving documents"})
		return
	}

	// Decodificar respuesta de Elasticsearch
	var esResponse EsSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&esResponse); err != nil {
		log.Printf("Error decoding Elasticsearch response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed processing search results"})
		return
	}

	log.Printf("Elasticsearch found %d potential hits.\n", esResponse.Hits.Total.Value)

	// Extraer los IDs de MongoDB de los resultados de ES
	mongoIDs := []primitive.ObjectID{}
	esHitsMap := make(map[string]EsHit) // Para guardar score y otros datos de ES
	for _, hit := range esResponse.Hits.Hits {
		if hit.ID == "" {
			log.Println("WARN: Elasticsearch hit found without _id")
			continue
		}
		objID, err := primitive.ObjectIDFromHex(hit.ID)
		if err != nil {
			log.Printf("WARN: Could not convert Elasticsearch hit ID '%s' to MongoDB ObjectID: %v\n", hit.ID, err)
			continue
		}
		mongoIDs = append(mongoIDs, objID)
		esHitsMap[hit.ID] = hit // Guardar hit por ID de Mongo
	}

	log.Printf("Extracted %d valid Mongo document IDs from ES results.\n", len(mongoIDs))

	// --- 3. Recuperar Texto de MongoDB usando los IDs ---
	var retrievedChunksText []string
	var retrievedDocsForResponse []string // Para la respuesta final

	if len(mongoIDs) > 0 {
		log.Printf("Fetching documents from MongoDB for %d IDs...\n", len(mongoIDs))
		collection := h.mongoClient.Database(h.dbName).Collection(h.collectionName)

		// Crear filtro $in para buscar por los IDs recuperados
		filter := bson.M{"_id": bson.M{"$in": mongoIDs}}
		// Proyectar solo los campos necesarios
		opts := options.Find().SetProjection(bson.M{"originaltitle": 1, "chunktext": 1})

		mongoCursor, err := collection.Find(ctx, filter, opts)
		if err != nil {
			log.Printf("Error finding documents in MongoDB: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed retrieving document content"})
			return
		}
		defer mongoCursor.Close(ctx)

		var mongoResults []MongoResult
		if err = mongoCursor.All(ctx, &mongoResults); err != nil {
			log.Printf("Error decoding MongoDB results: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed processing document content"})
			return
		}
		log.Printf("Successfully retrieved %d documents from MongoDB.\n", len(mongoResults))

		// Construir el contexto y la lista para la respuesta final
		// (Intentamos mantener el orden de ES si es posible, aunque $in no garantiza orden)
		retrievedChunksText = make([]string, 0, len(mongoIDs))
		retrievedDocsForResponse = make([]string, 0, len(mongoIDs))
		// Crear un mapa para búsqueda rápida de resultados de Mongo
		mongoResultsMap := make(map[string]MongoResult)
		for _, doc := range mongoResults {
			mongoResultsMap[doc.ID.Hex()] = doc
		}

		// Iterar sobre los IDs en el orden que los devolvió ES
		for _, hitID := range mongoIDs {
			hexID := hitID.Hex()
			if mongoDoc, ok := mongoResultsMap[hexID]; ok {
				if esHit, okEs := esHitsMap[hexID]; okEs {
					contextFragment := fmt.Sprintf("Fragmento (del artículo: %s):\n%s", mongoDoc.OriginalTitle, mongoDoc.ChunkText)
					retrievedChunksText = append(retrievedChunksText, contextFragment)
					retrievedDocsForResponse = append(retrievedDocsForResponse, fmt.Sprintf("[Score: %.4f] %s", esHit.Score, mongoDoc.ChunkText))
				}
			}
		}

	} else {
		log.Println("No relevant document IDs found in Elasticsearch to query MongoDB.")
	}

	// --- 4. Construir Prompt para LLM ---
	var contextBuilder strings.Builder
	if len(retrievedChunksText) > 0 {
		contextBuilder.WriteString("Contexto:\n---\n")
		contextBuilder.WriteString(strings.Join(retrievedChunksText, "\n---\n")) // Unir chunks con separador
		contextBuilder.WriteString("\n---")
	} else {
		contextBuilder.WriteString("No se encontró contexto relevante en la base de conocimiento disponible.")
	}
	contextString := contextBuilder.String()
	log.Printf("Retrieved Context for LLM:\n%s\n", contextString) // Loguear contexto final

	prompt := fmt.Sprintf(`Eres un asistente virtual experto exclusivamente en la investigación realizada en la Universidad de Santiago, basado en notas de prensa internas. Tu tarea es responder la pregunta del usuario utilizando ÚNICA Y EXCLUSIVAMENTE la información proporcionada en el siguiente contexto. No añadas información externa, opiniones personales ni datos que no estén explícitamente en el contexto. Si la respuesta a la pregunta no se encuentra en el contexto, indica claramente que no tienes información sobre ese tema específico en la documentación proporcionada. Sé conciso y directo.

%s

Pregunta del usuario:
%s

Respuesta:`, contextString, request.Query)

	log.Println("Constructed Prompt for LLM.")

	// --- 5. Llamar a Gemini API ---
	log.Println("Calling Gemini API...")
	llmResponseText, err := h.getLLMCompletion(prompt) // La función getLLMCompletion no cambia
	if err != nil {
		log.Printf("Error calling LLM API: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate response from LLM"})
		return
	}
	log.Println("Successfully received response from LLM.")

	// --- 6. Devolver Respuesta Final ---
	c.JSON(http.StatusOK, ChatResponse{
		Response:      llmResponseText,
		RetrievedDocs: retrievedDocsForResponse, // Devolver chunks recuperados (opcional)
	})
}

// getEmbedding (sin cambios)
func (h *ChatHandler) getEmbedding(text string) ([]float32, error) {
	// ... (igual que antes) ...
	apiURL := googleApiEndpoint + embeddingModel + ":embedContent?key=" + h.googleAPIKey

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

// getLLMCompletion (sin cambios)
func (h *ChatHandler) getLLMCompletion(prompt string) (string, error) {
	// ... (igual que antes) ...
	apiURL := googleApiEndpoint + llmModel + ":generateContent?key=" + h.googleAPIKey

	geminiReqBody := GeminiApiRequest{
		Contents:         []GeminiContent{{Parts: []GeminiPart{{Text: prompt}}}},
		GenerationConfig: GeminiGenerationConfig{Temperature: 0.7, MaxOutputTokens: 800},
		SafetySettings: []GeminiSafetySetting{
			{Category: "HARM_CATEGORY_HARASSMENT", Threshold: "BLOCK_MEDIUM_AND_ABOVE"},
			{Category: "HARM_CATEGORY_HATE_SPEECH", Threshold: "BLOCK_MEDIUM_AND_ABOVE"},
			{Category: "HARM_CATEGORY_SEXUALLY_EXPLICIT", Threshold: "BLOCK_MEDIUM_AND_ABOVE"},
			{Category: "HARM_CATEGORY_DANGEROUS_CONTENT", Threshold: "BLOCK_MEDIUM_AND_ABOVE"},
		},
	}

	jsonData, err := json.Marshal(geminiReqBody)
	if err != nil {
		return "", fmt.Errorf("error marshalling gemini request body: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("error creating gemini http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("error making gemini POST request: %w", err)
	}
	defer resp.Body.Close()

	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading gemini response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errorResp map[string]interface{}
		jsonErr := json.Unmarshal(respBodyBytes, &errorResp)
		if jsonErr == nil {
			return "", fmt.Errorf("Gemini API error: status %d, response: %v", resp.StatusCode, errorResp)
		}
		return "", fmt.Errorf("Gemini API error: status %d, response: %s", resp.StatusCode, string(respBodyBytes))
	}

	var apiResp GeminiApiResponse
	err = json.Unmarshal(respBodyBytes, &apiResp)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling gemini response JSON: %w. Response body: %s", err, string(respBodyBytes))
	}

	if len(apiResp.Candidates) > 0 && len(apiResp.Candidates[0].Content.Parts) > 0 {
		if apiResp.Candidates[0].FinishReason != "STOP" && apiResp.Candidates[0].FinishReason != "" {
			log.Printf("WARN: Gemini response finishReason was %s", apiResp.Candidates[0].FinishReason)
			if apiResp.Candidates[0].FinishReason == "SAFETY" {
				return "(Respuesta bloqueada por configuración de seguridad)", nil
			}
		}
		return apiResp.Candidates[0].Content.Parts[0].Text, nil
	}

	if len(apiResp.PromptFeedback.SafetyRatings) > 0 {
		log.Printf("WARN: Prompt feedback received safety ratings: %v", apiResp.PromptFeedback.SafetyRatings)
		return "(El prompt inicial pudo haber activado filtros de seguridad)", nil
	}

	log.Printf("WARN: Gemini response was empty or structure was unexpected. Full response: %s", string(respBodyBytes))
	return "", fmt.Errorf("no valid response text found in Gemini API candidates")
}

// Helper (sin cambios)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
