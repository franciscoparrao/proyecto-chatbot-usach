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

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// --- Constantes ---
const (
	embeddingModel    = "models/text-embedding-004"
	llmModel          = "models/gemini-1.5-flash-latest" // Modelo de Gemini a usar para generar respuesta
	googleApiEndpoint = "https://generativelanguage.googleapis.com/v1beta/"
	vectorIndexName   = "idx_vector_embedding"
	numCandidates     = 100
	numResults        = 3 // Recuperar hasta 3 chunks de contexto
)

// --- Estructuras API Chat ---
type ChatRequest struct {
	Query string `json:"query" binding:"required"`
}
type ChatResponse struct {
	Response      string   `json:"response"`
	RetrievedDocs []string `json:"retrieved_docs,omitempty"` // Opcional: devolver chunks usados
}

// --- Estructuras Google Embedding ---
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

// --- Estructuras Google Gemini API (generateContent) ---
type GeminiApiRequest struct {
	Contents         []GeminiContent        `json:"contents"`
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
	// StopSequences   []string `json:"stopSequences,omitempty"`
}
type GeminiApiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []GeminiPart `json:"parts"`
			Role  string       `json:"role"`
		} `json:"content"`
		FinishReason  string                `json:"finishReason"`
		SafetyRatings []GeminiSafetySetting `json:"safetyRatings"`
	} `json:"candidates"`
	PromptFeedback struct {
		SafetyRatings []GeminiSafetySetting `json:"safetyRatings"`
	} `json:"promptFeedback"`
}

// --- Estructura para Decodificar Resultados de MongoDB ---
type VectorSearchResult struct {
	ChunkText     string  `bson:"chunktext"`
	OriginalTitle string  `bson:"originaltitle"`
	Score         float64 `bson:"score"`
}

// --- ChatHandler ---
type ChatHandler struct {
	mongoClient    *mongo.Client
	dbName         string
	collectionName string
	googleAPIKey   string
}

func NewChatHandler(client *mongo.Client, db string, coll string, apiKey string) *ChatHandler {
	return &ChatHandler{
		mongoClient:    client,
		dbName:         db,
		collectionName: coll,
		googleAPIKey:   apiKey,
	}
}

// HandleChatRequest maneja las solicitudes POST a /api/chat
func (h *ChatHandler) HandleChatRequest(c *gin.Context) {
	var request ChatRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Printf("Error binding JSON: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}
	log.Printf("Received query: %s\n", request.Query)

	// --- Lógica RAG ---
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second) // Aumentar timeout general
	defer cancel()

	// 1. Obtener Embedding para la pregunta del usuario
	queryVector, err := h.getEmbedding(request.Query)
	if err != nil {
		log.Printf("Error getting query embedding: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process query embedding"})
		return
	}
	log.Printf("Successfully obtained query vector (size: %d)\n", len(queryVector))

	// 2. Realizar Búsqueda Vectorial en MongoDB
	log.Println("Performing MongoDB vector search...")
	collection := h.mongoClient.Database(h.dbName).Collection(h.collectionName)
	vectorSearchStage := bson.D{
		{"$vectorSearch", bson.D{
			{"index", vectorIndexName},
			{"path", "EmbeddingVector"},
			{"queryVector", queryVector},
			{"numCandidates", numCandidates},
			{"limit", numResults},
		}},
	}
	projectStage := bson.D{
		{"$project", bson.D{
			{"_id", 0},
			{"originaltitle", 1},
			{"chunktext", 1},
			{"score", bson.D{{"$meta", "vectorSearchScore"}}},
		}},
	}
	pipeline := mongo.Pipeline{vectorSearchStage, projectStage}
	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		log.Printf("Error performing vector search aggregation: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to search relevant documents"})
		return
	}
	defer cursor.Close(ctx)

	var searchResults []VectorSearchResult
	if err = cursor.All(ctx, &searchResults); err != nil {
		log.Printf("Error decoding vector search results: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process search results"})
		return
	}
	log.Printf("Found %d relevant chunks from vector search.\n", len(searchResults))

	// Crear el contexto concatenando los textos encontrados
	var contextBuilder strings.Builder
	retrievedDocsForResponse := []string{} // Para devolver opcionalmente
	if len(searchResults) > 0 {
		contextBuilder.WriteString("Contexto:\n")
		for i, result := range searchResults {
			contextFragment := fmt.Sprintf("Fragmento %d (del artículo: %s):\n%s\n", i+1, result.OriginalTitle, result.ChunkText)
			contextBuilder.WriteString(contextFragment)
			contextBuilder.WriteString("---\n")
			retrievedDocsForResponse = append(retrievedDocsForResponse, fmt.Sprintf("[Score: %.4f] %s", result.Score, result.ChunkText))
		}
	} else {
		contextBuilder.WriteString("No se encontró contexto relevante en las noticias de investigación disponibles.")
	}
	contextString := strings.TrimSpace(contextBuilder.String())
	log.Printf("Retrieved Context for LLM:\n---\n%s\n---\n", contextString)

	// 3. Construir Prompt para el LLM (Gemini)
	prompt := fmt.Sprintf(`Eres un asistente virtual experto exclusivamente en la investigación realizada en la Universidad de Santiago, basado en notas de prensa internas. Tu tarea es responder la pregunta del usuario utilizando ÚNICA Y EXCLUSIVAMENTE la información proporcionada en el siguiente contexto. No añadas información externa, opiniones personales ni datos que no estén explícitamente en el contexto. Si la respuesta a la pregunta no se encuentra en el contexto, indica claramente que no tienes información sobre ese tema específico en la documentación proporcionada. Sé conciso y directo.

%s

Pregunta del usuario:
%s

Respuesta:`, contextString, request.Query) // Inyectamos contexto y pregunta

	log.Println("Constructed Prompt for LLM.")
	// log.Printf("LLM Prompt Preview:\n%s\n", prompt[:min(500,len(prompt))]) // Descomentar para ver el prompt

	// 4. Llamar a la API del LLM (Gemini)
	log.Println("Calling Gemini API...")
	llmResponseText, err := h.getLLMCompletion(prompt)
	if err != nil {
		log.Printf("Error calling LLM API: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate response from LLM"})
		return
	}
	log.Println("Successfully received response from LLM.")

	// 5. Devolver la Respuesta del LLM al Frontend
	c.JSON(http.StatusOK, ChatResponse{
		Response:      llmResponseText,
		RetrievedDocs: retrievedDocsForResponse, // Opcional: enviar los chunks recuperados
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

// getLLMCompletion llama a la API de Gemini para generar texto
func (h *ChatHandler) getLLMCompletion(prompt string) (string, error) {
	apiURL := googleApiEndpoint + llmModel + ":generateContent?key=" + h.googleAPIKey

	// Construir cuerpo de la solicitud para Gemini API
	geminiReqBody := GeminiApiRequest{
		Contents: []GeminiContent{
			{Parts: []GeminiPart{{Text: prompt}}},
		},
		// Configuración opcional de generación y seguridad
		GenerationConfig: GeminiGenerationConfig{
			Temperature:     0.7, // Un valor razonable para respuestas informativas
			MaxOutputTokens: 800, // Limitar longitud de respuesta
		},
		SafetySettings: []GeminiSafetySetting{ // Bloquear contenido más dañino
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

	// Usar un contexto con timeout para la llamada HTTP
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) // Timeout más largo para LLM
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("error creating gemini http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Realizar solicitud POST
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("error making gemini POST request: %w", err)
	}
	defer resp.Body.Close()

	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading gemini response body: %w", err)
	}

	// Chequear código de estado HTTP
	if resp.StatusCode != http.StatusOK {
		var errorResp map[string]interface{}
		jsonErr := json.Unmarshal(respBodyBytes, &errorResp)
		if jsonErr == nil {
			return "", fmt.Errorf("Gemini API error: status %d, response: %v", resp.StatusCode, errorResp)
		}
		return "", fmt.Errorf("Gemini API error: status %d, response: %s", resp.StatusCode, string(respBodyBytes))
	}

	// Decodificar respuesta JSON exitosa
	var apiResp GeminiApiResponse
	err = json.Unmarshal(respBodyBytes, &apiResp)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling gemini response JSON: %w. Response body: %s", err, string(respBodyBytes))
	}

	// Extraer el texto de la respuesta
	if len(apiResp.Candidates) > 0 && len(apiResp.Candidates[0].Content.Parts) > 0 {
		// Verificar si fue bloqueado por seguridad u otra razón
		if apiResp.Candidates[0].FinishReason != "STOP" && apiResp.Candidates[0].FinishReason != "" {
			log.Printf("WARN: Gemini response finishReason was %s", apiResp.Candidates[0].FinishReason)
			// Devolver un mensaje indicando el bloqueo si es relevante (ej. SAFETY)
			if apiResp.Candidates[0].FinishReason == "SAFETY" {
				return "(Respuesta bloqueada por configuración de seguridad)", nil
			}
		}
		return apiResp.Candidates[0].Content.Parts[0].Text, nil
	}

	// Manejar caso de respuesta vacía o inesperada
	if len(apiResp.PromptFeedback.SafetyRatings) > 0 {
		log.Printf("WARN: Prompt feedback received safety ratings: %v", apiResp.PromptFeedback.SafetyRatings)
		return "(El prompt inicial pudo haber activado filtros de seguridad)", nil
	}

	log.Printf("WARN: Gemini response was empty or structure was unexpected. Full response: %s", string(respBodyBytes))
	return "", fmt.Errorf("no valid response text found in Gemini API candidates")
}

// Helper (no es estrictamente necesario ahora)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
