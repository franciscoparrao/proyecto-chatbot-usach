package handlers

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/securecookie"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// --- Constantes ---
const (
	// Modelos y API de Google
	embeddingModel = "models/text-embedding-004"
	// IMPORTANTE: Verifica que este sea el modelo correcto y disponible para tu API Key.
	// Considera "gemini-1.5-pro-latest" si necesitas más capacidad o "gemini-1.0-pro" como alternativa estable.
	llmModel          = "models/gemini-2.5-flash-preview-04-17"
	googleApiEndpoint = "https://generativelanguage.googleapis.com/v1beta/"

	// Parámetros de Búsqueda RAG
	numCandidatesInitial    = 100 // Para kNN en la búsqueda inicial amplia
	initialRetrievalResults = 6   // Documentos para síntesis de temas o respuesta amplia inicial - Reducido para evitar MAX_TOKENS
	numCandidatesFollowUp   = 50  // Para kNN en búsquedas de seguimiento más enfocadas
	// numResultsFollowUp estaba en 3 en la conversación, pero puede ser igual a numResults si esa constante se usa para el tamaño general.
	// Usaremos una nueva constante para mayor claridad.
	retrievalSizeFollowUp = 3 // Documentos para respuesta directa enfocada (después de seleccionar opción o seguimiento específico)

	minResultsForClarification = 3 // Mínimo de documentos recuperados para intentar ofrecer opciones de clarificación

	// Boosts para Búsqueda Híbrida
	// Para consultas iniciales/exploratorias (queremos que el texto domine si hay keywords específicas)
	textBoostInitialAggressive = 3.0 // Boost extremo para dar prioridad al texto
	knnBoostInitialAggressive  = 0.3 // Reducido para dar aún más prioridad al texto
	// Para seguimientos o selecciones de opciones (más balanceado)
	textBoostFollowUpBalanced = 1.5
	knnBoostFollowUpBalanced  = 1.0
)

// --- Estructuras ---

type ChatMessage struct {
	Role string `json:"role"` // 'user' o 'model' / 'asistente'
	Text string `json:"text"`
}

type ChatRequest struct {
	Query         string        `json:"query" binding:"required"`
	History       []ChatMessage `json:"history,omitempty"`
	Intent        string        `json:"intent,omitempty"`          // ej: "select_option", "initial_query"
	SelectedRef   string        `json:"selected_ref,omitempty"`    // Referencia de la opción seleccionada (ej. el texto de la opción)
	IsOptionReply bool          `json:"is_option_reply,omitempty"` // True si es una respuesta a opciones ofrecidas
	HybridMode    bool          `json:"hybrid_mode"`               // Permitir que el cliente envíe esto. Se setea a true por defecto si no se envía.
}

type ChatOption struct {
	Label    string `json:"label"`
	QueryRef string `json:"query_ref"` // Texto que se usará como query si se selecciona esta opción
}

type ChatResponse struct {
	ResponseType  string       `json:"response_type"` // "direct_answer", "clarification_options", "no_context"
	Response      string       `json:"response"`
	Options       []ChatOption `json:"options,omitempty"`
	RetrievedDocs []string     `json:"retrieved_docs,omitempty"` // Para depuración o mostrar fuentes (ej. títulos)
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
	Temperature     float32  `json:"temperature,omitempty"`
	TopK            int      `json:"topK,omitempty"`
	TopP            float32  `json:"topP,omitempty"`
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type GeminiApiRequest struct {
	Contents         []GeminiContent        `json:"contents"`
	SafetySettings   []GeminiSafetySetting  `json:"safetySettings,omitempty"`
	GenerationConfig GeminiGenerationConfig `json:"generationConfig,omitempty"`
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
	PromptFeedback *struct {
		SafetyRatings []GeminiSafetySetting `json:"safetyRatings,omitempty"`
	} `json:"promptFeedback,omitempty"`
}

type EsHit struct {
	ID     string  `json:"_id"`
	Score  float64 `json:"_score"`
	Fields *struct {
		MongoDocID              []string `json:"MongoDocID"`
		OriginalTitle           []string `json:"OriginalTitle"`
		OriginalAuthors         []string `json:"OriginalAuthors"`
		OriginalPublicationDate []string `json:"OriginalPublicationDate"`
	} `json:"fields,omitempty"`
}

type EsSearchResponse struct {
	Hits struct {
		Total struct {
			Value    int    `json:"value"`
			Relation string `json:"relation"`
		} `json:"total"`
		MaxScore float64 `json:"max_score"`
		Hits     []EsHit `json:"hits"`
	} `json:"hits"`
}

type MongoResult struct {
	ID                      primitive.ObjectID `bson:"_id"`
	OriginalTitle           string             `bson:"originaltitle"`
	OriginalAuthors         string             `bson:"originalauthors"`
	OriginalPublicationDate string             `bson:"originalpublicationdate"`
	ChunkText               string             `bson:"chunktext"`
}

type ChatHandler struct {
	mongoClient    *mongo.Client
	esClient       *elasticsearch.Client
	dbName         string
	collectionName string
	esIndexName    string
	googleAPIKey   string
}

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

// --- Estructura para Almacenamiento de Chat en Cookies ---
type Message struct {
	Role    string    `json:"role"` // "user" o "bot"
	Content string    `json:"content"`
	SentAt  time.Time `json:"sent_at"`
}

type Session struct {
	ID        string    `json:"id"`
	History   []Message `json:"history"`
	ExpiresAt time.Time `json:"expires_at"`
}

type GZIPSerializer struct{}

const MaxHistory = 10

var (
	hashKey  = securecookie.GenerateRandomKey(64)
	blockKey = securecookie.GenerateRandomKey(32)
	s        *securecookie.SecureCookie
)

// --- Funciones de almacenamiento maximo en el manejo de sesión ---
func trimHistory(history []Message) []Message {
	if len(history) <= MaxHistory {
		return history
	}
	return history[len(history)-MaxHistory:]
}

// --- Serializador GZIP para SecureCookie ---
func (g GZIPSerializer) Serialize(src interface{}) ([]byte, error) {
	// 1. Serializar a JSON
	jsonData, err := json.Marshal(src)
	if err != nil {
		return nil, err // Devolver error original
	}

	// 2. Comprimir con GZIP
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(jsonData); err != nil {
		return nil, err // Devolver error original
	}
	if err := gz.Close(); err != nil {
		return nil, err // Devolver error original
	}

	return buf.Bytes(), nil
}

// Deserializar GZIP
func (g GZIPSerializer) Deserialize(src []byte, dst interface{}) error {
	// 1. Descomprimir GZIP
	buf := bytes.NewBuffer(src)
	gz, err := gzip.NewReader(buf)
	if err != nil {
		return err // Devolver error original
	}
	defer gz.Close()

	// 2. Leer datos descomprimidos
	jsonData, err := io.ReadAll(gz)
	if err != nil {
		return err // Devolver error original
	}

	// 3. Deserializar JSON
	if err := json.Unmarshal(jsonData, dst); err != nil {
		return err // Devolver error original
	}

	return nil
}

// Inicializar SecureCookie con GZIPSerializer
func init() {
	s = securecookie.New(hashKey, blockKey)
	s.SetSerializer(GZIPSerializer{})
}

// Función para guardar sesión en cookie
func SaveSession(c *gin.Context, session Session) error {
	encoded, err := s.Encode("chat_session", session)
	if err != nil {
		return err
	}

	c.SetCookie("chat_session", encoded, 1800, "/", "", true, true)
	return nil
}

// Función para cargar sesión desde cookie (con recuperación de errores)
func LoadSession(c *gin.Context) Session {
	var session Session
	cookie, err := c.Cookie("chat_session")

	// Caso 1: No hay cookie → nueva sesión
	if err == http.ErrNoCookie {
		return newSession()
	}

	// Caso 2: Error al leer cookie → log y nueva sesión
	if err != nil {
		log.Printf("Error leyendo cookie: %v", err)
		return newSession()
	}

	// Caso 3: Decodificación fallida → log, borra cookie corrupta y nueva sesión
	if err := s.Decode("chat_session", cookie, &session); err != nil {
		log.Printf("Cookie corrupta: %v. Generando nueva sesión...", err)
		clearInvalidCookie(c) // Limpia la cookie inválida
		return newSession()
	}

	return session
}

// Función para crear nueva sesión
func newSession() Session {
	return Session{
		ID:        uuid.New().String(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
		History:   []Message{},
	}
}

func clearInvalidCookie(c *gin.Context) {
	c.SetCookie("chat_session", "", -1, "/", "", false, true)
}

// Función de debug para imprimir conversación
func printSessionFromCookie(session Session) {
	fmt.Println("=== Conversación almacenada en cookies ===")
	for i, msg := range session.History {
		fmt.Printf("%d. [%s][%s] %s\n",
			i+1,
			msg.SentAt.Format("2006-01-02 15:04:05"),
			msg.Role,
			msg.Content)
	}
}

// Devuelve el último mensaje del historial de la sesión.
func GetLastMessageContent(session Session) string {
	if len(session.History) == 0 {
		return ""
	}
	return session.History[len(session.History)-1].Content
}

// Función auxiliar para construir el contexto
func buildContextQuery(history []Message, currentQuery string) string {
	var contextBuilder strings.Builder

	// Tomar solo los últimos 5 mensajes del usuario para el contexto
	start := len(history) - 5
	if start < 0 {
		start = 0
	}

	for _, msg := range history[start:] {
		if msg.Role == "user" {
			contextBuilder.WriteString(msg.Content)
			contextBuilder.WriteString(". ") // Separador entre mensajes
		}
	}

	contextBuilder.WriteString(currentQuery) // Agregar la nueva consulta al final
	return contextBuilder.String()
}

// --- Funciones Auxiliares de Lógica de Chat ---

func (h *ChatHandler) rewriteQueryWithHistory(originalQuery string, history []ChatMessage, c *gin.Context) (string, error) {
	session := LoadSession(c)
	if session.ID == "" {
		session = newSession()
	}

	// Construir contexto concatenando historial + nueva pregunta
	contextQuery := buildContextQuery(session.History, originalQuery)

	rewritePrompt := fmt.Sprintf(`Tu tarea es tomar un historial de conversación y la "Última Pregunta del Usuario". Genera una nueva pregunta que sea autónoma y refleje la intención completa y específica del usuario, resolviendo cualquier referencia contextual del historial. La nueva pregunta se usará para buscar información precisa en una base de datos de investigación.

Consideraciones para la pregunta reescrita:
- Debe ser una pregunta completa e independiente, formulada como si no hubiera historial previo.
- CRÍTICO: Si la "Última Pregunta del Usuario" contiene referencias contextuales como "eso", "este tema", "el primer punto", "ese descubrimiento", "lo que mencionaste", DEBES reemplazar esas referencias con el sujeto o concepto específico discutido en los turnos inmediatamente anteriores del Asistente o del Usuario. Por ejemplo, si el Asistente dijo: "Hemos discutido el impacto de los bio-polímeros en la agricultura" y el usuario pregunta: "¿Qué desafíos presenta eso?", la pregunta reescrita DEBE SER: "¿Qué desafíos presenta el impacto de los bio-polímeros en la agricultura?".
- Si la "Última Pregunta del Usuario" pide más detalles sobre un tema específico mencionado JUSTO ANTES por el Asistente, asegúrate de que la pregunta reescrita incorpore la ESENCIA de ese tema específico.
- Si la "Última Pregunta del Usuario" introduce un tema claramente nuevo o diferente, la pregunta reescrita debe enfocarse en ese nuevo tema.
- Si la "Última Pregunta del Usuario" ya es clara, completa y no depende del historial, devuélvela tal cual.
- La pregunta reescrita debe ser concisa y optimizada para una búsqueda semántica.
- Responde ÚNICAMENTE con la pregunta reescrita, sin preámbulos ni explicaciones adicionales.

Historial de la Conversación:
---
%s
---

Última Pregunta del Usuario:
%s

Pregunta Reescrita Optimizada para Búsqueda:`, contextQuery, originalQuery)

	log.Printf("Calling LLM for query rewriting. Preview of history for rewrite:\n%s\nOriginal query for rewrite: %s\n", contextQuery, originalQuery)

	generationConfigForRewrite := &GeminiGenerationConfig{
		Temperature:     0.1,
		MaxOutputTokens: 100, // Reducido para evitar MAX_TOKENS
		TopP:            0.95,
	}

	rewrittenQueryText, err := h.getLLMCompletion(rewritePrompt, generationConfigForRewrite)
	if err != nil {
		log.Printf("Error rewriting query with LLM: %v. Using original query.\n", err)
		return originalQuery, nil
	}

	finalRewrittenQuery := strings.TrimSpace(rewrittenQueryText)
	finalRewrittenQuery = strings.Trim(finalRewrittenQuery, "\"")

	if finalRewrittenQuery == "" ||
		strings.ToLower(finalRewrittenQuery) == "ninguno" ||
		strings.Contains(strings.ToLower(finalRewrittenQuery), "hubo un inconveniente") ||
		strings.Contains(strings.ToLower(finalRewrittenQuery), "error") ||
		strings.Contains(finalRewrittenQuery, "InvestigaUSACH:") ||
		(strings.EqualFold(finalRewrittenQuery, originalQuery) && len(originalQuery) < 25 && len(history) > 0) || // Si no cambió, era corta y había historial (umbral ajustado)
		len(finalRewrittenQuery) > len(originalQuery)*3+50 || len(finalRewrittenQuery) > 300 { // Ajustado límite superior y multiplicador
		log.Printf("Rewritten query was empty, 'ninguno', contains error message, too similar to short original, or too long/different. Using original query: '%s'", originalQuery)
		return originalQuery, nil
	}
	log.Printf("Original Query: '%s' -> Rewritten Query: '%s'\n", originalQuery, finalRewrittenQuery)
	return finalRewrittenQuery, nil
}

func (h *ChatHandler) synthesizeTopics(query string, mongoResults []MongoResult) ([]ChatOption, error) {
	if len(mongoResults) == 0 {
		log.Println("No documents provided to synthesizeTopics.")
		return nil, fmt.Errorf("no hay documentos para sintetizar")
	}

	var contextForSynthesis strings.Builder
	titlesForLog := []string{}
	for idx, doc := range mongoResults {
		extractLength := 250 // Un poco más de extracto para la síntesis
		if len(doc.ChunkText) < extractLength {
			extractLength = len(doc.ChunkText)
		}
		extract := doc.ChunkText[:extractLength]
		if len(doc.ChunkText) > extractLength {
			extract += "..."
		}
		contextForSynthesis.WriteString(fmt.Sprintf("Documento %d (Título: \"%s\"):\n%s\n\n", idx+1, doc.OriginalTitle, extract))
		titlesForLog = append(titlesForLog, fmt.Sprintf("[%d] ID: %s, Título: \"%s\"", idx+1, doc.ID.Hex(), doc.OriginalTitle))
	}
	log.Printf("Documentos para síntesis de temas (total %d):\n  %s", len(mongoResults), strings.Join(titlesForLog, "\n  "))

	synthesisPrompt := fmt.Sprintf(`Dada la "Pregunta del Usuario" y una "Lista de Documentos Relevantes" (con títulos y extractos), tu tarea es:
1. Analizar CUIDADOSAMENTE cada documento en relación con la "Pregunta del Usuario".
2. Identificar y agrupar aquellos que apunten a líneas de investigación o subtemas DISTINTOS pero DIRECTAMENTE Y ALTAMENTE RELEVANTES a la "Pregunta del Usuario".
3. Describe concisamente de 1 a 3 de estos subtemas principales como opciones claras y accionables para el usuario. Cada opción debe ser auto-contenida y reflejar una faceta específica de la pregunta original.
4. Si un documento parece claramente fuera del tópico principal de la "Pregunta del Usuario", IGNÓRALO para la síntesis.
5. Prioriza la calidad y relevancia sobre la cantidad. Si solo encuentras 1 o 2 subtemas realmente claros y pertinentes, es suficiente. No fuerces 3 opciones si no son válidas.
6. Formato de Respuesta: Responde ÚNICAMENTE con una lista numerada de los subtemas identificados (ej. "1. Descripción del Subtema A.\n2. Descripción del Subtema B."). Cada descripción de subtema debe ser lo suficientemente detallada para que el usuario entienda de qué trata y pueda seleccionarla (aprox. 15-25 palabras).
7. Si, tras un análisis riguroso, no puedes identificar subtemas claros y distintos que sean directamente relevantes a la pregunta, responde con la frase exacta: "NO_SUBTEMAS_CLAROS".

Pregunta del Usuario: "%s"

Lista de Documentos Relevantes (Títulos y Extractos):
---
%s
---

Subtemas Identificados (1-3 opciones, solo los más relevantes a la pregunta, formato: "1. Opción A\n2. Opción B"):`, query, contextForSynthesis.String())

	synthesisConfig := &GeminiGenerationConfig{
		Temperature:     0.25,
		MaxOutputTokens: 300, // Reducido para evitar MAX_TOKENS
		TopP:            0.95,
	}

	log.Println("Calling LLM for topic synthesis...")
	topicsText, err := h.getLLMCompletion(synthesisPrompt, synthesisConfig)
	if err != nil {
		return nil, fmt.Errorf("error en síntesis de temas con LLM: %w", err)
	}

	trimmedTopicsText := strings.TrimSpace(topicsText)
	if trimmedTopicsText == "NO_SUBTEMAS_CLAROS" || trimmedTopicsText == "" {
		log.Println("LLM de síntesis indicó NO_SUBTEMAS_CLAROS o respuesta vacía.")
		return nil, nil
	}

	rawOptions := strings.Split(trimmedTopicsText, "\n")
	var chatOptions []ChatOption
	for _, optStr := range rawOptions {
		optStr = strings.TrimSpace(optStr)
		re := regexp.MustCompile(`^\d+[\.\-\)]?\s*`)
		cleanedOptStr := re.ReplaceAllString(optStr, "")
		if len(cleanedOptStr) > 10 && len(cleanedOptStr) < 150 { // Longitud razonable para una opción
			chatOptions = append(chatOptions, ChatOption{
				Label:    cleanedOptStr,
				QueryRef: cleanedOptStr,
			})
		}
	}

	if len(chatOptions) == 0 {
		log.Println("No se pudieron extraer opciones válidas de la respuesta de síntesis. Respuesta LLM:", trimmedTopicsText)
		return nil, nil
	}
	log.Printf("Síntesis generó %d opciones.", len(chatOptions))
	return chatOptions, nil
}

// --- Handler Principal ---

func (h *ChatHandler) HandleChatRequest(c *gin.Context) {
	var request ChatRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Printf("Error binding JSON: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// Establecer HybridMode a true por defecto si no se envía explícitamente como false
	// El tipo bool en Go se inicializa a false por defecto.
	// Si el campo no está en el JSON, será false. Si está y es false, será false.
	// Así que necesitamos una forma de saber si fue enviado explícitamente como false, o simplemente no enviado.
	// La forma más simple es que el cliente SIEMPRE envíe hybrid_mode: true o hybrid_mode: false.
	// Si asumimos que el cliente envía el flag, este log es útil.
	// Si no, podríamos tener un puntero o un wrapper, pero por ahora asumimos que el cliente lo envía.
	// Si no viene en el request (queda como false), aquí lo forzamos a true, a menos que el cliente
	// explícitamente haya enviado `hybrid_mode: false` (lo cual no podemos distinguir de "no enviado"
	// si el campo es `bool` y no `*bool`).
	// SOLUCIÓN SIMPLE: El frontend siempre enviará el flag. Si no lo hace, aquí decidimos el default.
	// Si el campo `HybridMode` no está presente en el JSON, `request.HybridMode` será `false`.
	// Si queremos que el default sea `true` a menos que el cliente diga `false`, hacemos:
	// hybridModePresent := c.Get("hybrid_mode_parsed_explicitly") // Esto no funciona así con ShouldBindJSON
	// Lo más simple es: si el cliente no envía nada, es false.
	// Si queremos default true, el cliente debe enviar true, o nosotros lo forzamos.
	// Por ahora, si el cliente no lo envía, será false. Para forzar default true:
	// if ! (valor explícito de false) { request.HybridMode = true } -- complejo
	// Asumimos que el cliente envía el booleano, o que el default false es aceptable
	// y se cambia la lógica de boosts si request.HybridMode es false.
	// Por ahora, lo dejaremos tal cual: si el cliente envía true, se usa; si envía false, se usa; si no envía, es false.
	// Para nuestra lógica de desarrollo, la forzaremos a true aquí si no se especifica otra cosa en la request
	// (esto es un poco un hack, idealmente el frontend es explícito o usamos un *bool).
	// ESTA LÓGICA DE DEFAULT ES PROBLEMÁTICA CON BOOL.
	// Lo más robusto es que el frontend SIEMPRE envíe el valor.
	// Si no, debemos usar un puntero *bool o un método diferente para detectar su ausencia.
	// Por ahora, si el cliente no manda `hybrid_mode`, será `false`.
	// Vamos a forzarlo a true a menos que explícitamente sea false.
	// Esta es una simplificación temporal.
	// hybridModeExplicitlySet := false
	// if rawHybrid, exists := c.Get("hybrid_mode"); exists { // Esto requeriría un middleware o parseo manual
	//     if val, ok := rawHybrid.(bool); ok {
	//          request.HybridMode = val
	//          hybridModeExplicitlySet = true
	//      }
	// }
	// if !hybridModeExplicitlySet {
	//     request.HybridMode = true // Default a true si no se envía explícitamente
	//     log.Println("HybridMode no enviado por el cliente, default a true.")
	// }
	// La forma más fácil es que el frontend envíe el flag. Si no lo hace, `request.HybridMode` será `false`.
	// Si queremos que el default sea `true`, y permitir al cliente desactivarlo:
	// No hay forma fácil de distinguir "no enviado" de "enviado como false" para un campo `bool`.
	// SOLUCIÓN: El frontend debe enviar el flag. Asumiremos true si no se especifica cómo deshabilitarlo.
	// Si `request.HybridMode` llega como `false` (porque el cliente lo envió o es el valor zero),
	// entonces la búsqueda híbrida se desactiva. Si queremos que sea `true` por defecto:
	// ¡Corregido! El cliente Vue ya envía `hybrid_mode: hybridModeEnabled.value`, así que esto funciona.

	log.Printf("Received query: '%s', HybridMode: %t, History: %d msgs, Intent: '%s', SelectedRef: '%s', IsOptionReply: %t\n",
		request.Query, request.HybridMode, len(request.History), request.Intent, request.SelectedRef, request.IsOptionReply)

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second) // Aumentado por múltiples llamadas LLM
	defer cancel()

	effectiveQuery := request.Query
	isFollowUpToOfferedOption := false

	if request.IsOptionReply { // Si el frontend indica que es una respuesta a opciones
		isFollowUpToOfferedOption = true
		if request.SelectedRef != "" {
			effectiveQuery = request.SelectedRef
			log.Printf("User selected an offered option (IsOptionReply=true, SelectedRef provided). Using SelectedRef as effectiveQuery: '%s'", effectiveQuery)
		} else {
			// Si IsOptionReply es true pero SelectedRef está vacío, la query actual DEBE ser la selección.
			log.Printf("User selected an offered option (IsOptionReply=true, SelectedRef empty). Using current query as effectiveQuery: '%s'", effectiveQuery)
		}
	} else if len(request.History) > 0 {
		rewrittenQuery, errRewrite := h.rewriteQueryWithHistory(request.Query, request.History, c)
		if errRewrite != nil {
			log.Printf("WARN: Could not rewrite query with history, using original: %v", errRewrite)
		} else {
			effectiveQuery = rewrittenQuery
		}
	}
	log.Printf(">>> FINAL EFFECTIVE QUERY FOR RAG: %s", effectiveQuery)

	queryVector, err := h.getEmbedding(effectiveQuery)
	if err != nil {
		log.Printf("Error getting query embedding: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process query embedding"})
		return
	}
	log.Printf("Successfully obtained query vector (size: %d)\n", len(queryVector))

	var textSearchBoost, knnSearchBoost float64
	var currentRetrievalSize, currentNumCandidates int
	isInitialOrExploratory := !isFollowUpToOfferedOption // Si no es un seguimiento a opción, es inicial/exploratoria

	if isInitialOrExploratory {
		log.Println("Query type: INITIAL or NEW EXPLORATORY. Using aggressive text boosts.")
		currentRetrievalSize = initialRetrievalResults
		currentNumCandidates = numCandidatesInitial
		textSearchBoost = textBoostInitialAggressive
		knnSearchBoost = knnBoostInitialAggressive
	} else {
		log.Println("Query type: FOLLOW-UP TO OFFERED OPTION. Using balanced boosts and fewer results.")
		currentRetrievalSize = retrievalSizeFollowUp
		currentNumCandidates = numCandidatesFollowUp
		textSearchBoost = textBoostFollowUpBalanced
		knnSearchBoost = knnBoostFollowUpBalanced
	}
	log.Printf("Search params: retrievalSize=%d, numCandidates=%d, textBoost=%.1f, knnBoost=%.1f",
		currentRetrievalSize, currentNumCandidates, textSearchBoost, knnSearchBoost)

	var esQuery gin.H
	if request.HybridMode {
		log.Println("Using HYBRID search mode.")
		esQuery = gin.H{
			"query": gin.H{
				"multi_match": gin.H{
					"query":     effectiveQuery,
					"fields":    []string{"OriginalTitle^5", "ChunkText^5"},
					"type":      "best_fields",
					"fuzziness": "AUTO",
					"boost":     textSearchBoost,
				},
			},
			"knn": gin.H{
				"field":          "EmbeddingVector",
				"query_vector":   queryVector,
				"k":              currentNumCandidates,
				"num_candidates": currentNumCandidates,
				"boost":          knnSearchBoost,
			},
			"_source": false,
			"fields":  []string{"MongoDocID", "OriginalTitle", "OriginalAuthors", "OriginalPublicationDate"},
			"size":    currentRetrievalSize,
		}
	} else {
		log.Println("Using VECTOR-ONLY search mode.")
		esQuery = gin.H{
			"knn": gin.H{
				"field":          "EmbeddingVector",
				"query_vector":   queryVector,
				"k":              currentNumCandidates,
				"num_candidates": currentNumCandidates,
			},
			"_source": false,
			"fields":  []string{"MongoDocID", "OriginalTitle"}, // Solo necesitamos MongoDocID y OriginalTitle si no hay texto
			"size":    currentRetrievalSize,
		}
	}
	// log.Printf("Elasticsearch Query: %s", func() string { b, _ := json.MarshalIndent(esQuery, "", "  "); return string(b) }())

	var esBuffer bytes.Buffer
	if err := json.NewEncoder(&esBuffer).Encode(esQuery); err != nil {
		log.Printf("Error encoding Elasticsearch query: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to build search query"})
		return
	}

	res, err := h.esClient.Search(
		h.esClient.Search.WithContext(ctx),
		h.esClient.Search.WithIndex(h.esIndexName),
		h.esClient.Search.WithBody(&esBuffer),
		h.esClient.Search.WithTrackTotalHits(true),
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error retrieving documents from ES"})
		return
	}

	var esResponse EsSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&esResponse); err != nil {
		log.Printf("Error decoding Elasticsearch response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed processing ES search results"})
		return
	}
	log.Printf("Elasticsearch found %d potential hits. Retrieved %d hits.", esResponse.Hits.Total.Value, len(esResponse.Hits.Hits))

	mongoIDs := []primitive.ObjectID{}
	retrievedTitlesFromES := []string{} // Para loguear los títulos directamente de ES si están
	for _, hit := range esResponse.Hits.Hits {
		var mongoDocIDStr string
		if hit.Fields != nil && len(hit.Fields.MongoDocID) > 0 {
			mongoDocIDStr = hit.Fields.MongoDocID[0]
		} else if hit.ID != "" {
			mongoDocIDStr = hit.ID
			log.Printf("WARN: MongoDocID not found in fields for ES hit %s, using ES _id as fallback.", hit.ID)
		} else {
			log.Println("WARN: Elasticsearch hit found without _id or MongoDocID in fields")
			continue
		}

		objID, err := primitive.ObjectIDFromHex(mongoDocIDStr)
		if err != nil {
			log.Printf("WARN: Could not convert ID '%s' to MongoDB ObjectID: %v\n", mongoDocIDStr, err)
			continue
		}
		mongoIDs = append(mongoIDs, objID)
		if hit.Fields != nil && len(hit.Fields.OriginalTitle) > 0 {
			retrievedTitlesFromES = append(retrievedTitlesFromES, fmt.Sprintf("ID: %s, Score: %.2f, Title: %s", mongoDocIDStr, hit.Score, hit.Fields.OriginalTitle[0]))
		} else {
			retrievedTitlesFromES = append(retrievedTitlesFromES, fmt.Sprintf("ID: %s, Score: %.2f, Title: (not in ES fields)", mongoDocIDStr, hit.Score))
		}
	}
	if len(retrievedTitlesFromES) > 0 {
		log.Printf("Top ES Hits (before Mongo lookup):\n  %s", strings.Join(retrievedTitlesFromES, "\n  "))
	}
	log.Printf("Extracted %d valid Mongo document IDs from ES results.", len(mongoIDs))

	var mongoResults []MongoResult
	if len(mongoIDs) > 0 {
		collection := h.mongoClient.Database(h.dbName).Collection(h.collectionName)
		filter := bson.M{"_id": bson.M{"$in": mongoIDs}}
		findOpts := options.Find().SetProjection(bson.M{
			"originaltitle":           1,
			"originalauthors":         1,
			"originalpublicationdate": 1,
			"chunktext":               1,
		})

		mongoCursor, err := collection.Find(ctx, filter, findOpts)
		if err != nil {
			// ... error handling ...
			log.Printf("Error finding documents in MongoDB: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed retrieving document content from Mongo"})
			return
		}
		defer mongoCursor.Close(ctx)

		if err = mongoCursor.All(ctx, &mongoResults); err != nil {
			// ... error handling ...
			log.Printf("Error decoding MongoDB results: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed processing document content from Mongo"})
			return
		}
		log.Printf("Successfully retrieved %d documents from MongoDB.", len(mongoResults))
	} else {
		log.Println("No relevant document IDs found in Elasticsearch to query MongoDB.")
	}

	orderedMongoResults := make([]MongoResult, 0, len(mongoIDs))
	mongoResultsMap := make(map[primitive.ObjectID]MongoResult)
	for _, res := range mongoResults {
		mongoResultsMap[res.ID] = res
	}
	for _, id := range mongoIDs {
		if doc, found := mongoResultsMap[id]; found {
			orderedMongoResults = append(orderedMongoResults, doc)
		}
	}
	mongoResults = orderedMongoResults

	var contextBuilder strings.Builder
	var retrievedDocsForClientResponse []string
	if len(mongoResults) > 0 {
		contextBuilder.WriteString("Contexto Proporcionado:\n---\n")
		for i, doc := range mongoResults {
			// Limitar el texto del chunk para evitar MAX_TOKENS
			chunkTextLimited := doc.ChunkText
			maxChunkLength := 800 // Caracteres por chunk
			if len(chunkTextLimited) > maxChunkLength {
				chunkTextLimited = chunkTextLimited[:maxChunkLength] + "..."
			}
			fragment := fmt.Sprintf("Fragmento de Investigación USACH %d:\nTítulo: \"%s\"\nAutores: %s\nFecha: %s\nContenido: %s\n---",
				i+1,
				doc.OriginalTitle,
				doc.OriginalAuthors,
				doc.OriginalPublicationDate,
				chunkTextLimited)
			contextBuilder.WriteString(fragment)
			if i < len(mongoResults)-1 {
				contextBuilder.WriteString("\n")
			}
			retrievedDocsForClientResponse = append(retrievedDocsForClientResponse, fmt.Sprintf("Título: %s", doc.OriginalTitle))
		}
	} else {
		contextBuilder.WriteString("No se encontró contexto relevante en la base de conocimiento disponible para responder a tu pregunta.")
	}
	contextString := contextBuilder.String()

	if len(mongoResults) > 0 {
		log.Printf("Retrieved Context for LLM contains %d documents. First title from Mongo: '%s'", len(mongoResults), mongoResults[0].OriginalTitle)
	} else {
		log.Println("No context retrieved for LLM based on MongoDB lookup.")
	}
	// Para depuración intensiva del contexto completo:
	// log.Printf("Full context string for LLM:\n%s", contextString)

	var finalResponse ChatResponse
	if isInitialOrExploratory && len(mongoResults) >= minResultsForClarification {
		log.Println("Attempting to synthesize topics for initial/exploratory query...")
		clarificationOptions, errSynth := h.synthesizeTopics(effectiveQuery, mongoResults) // Usa mongoResults ordenados
		if errSynth != nil {
			log.Printf("Error synthesizing topics: %v. Proceeding with direct answer.", errSynth)
		} else if len(clarificationOptions) > 0 {
			log.Printf("Successfully synthesized %d topic options.", len(clarificationOptions))
			count := len(clarificationOptions)
			var introMsg string
			if count == 1 {
				introMsg = fmt.Sprintf("Encontré la siguiente línea de investigación principal relacionada con tu pregunta sobre '%s'. ¿Te gustaría profundizar en ella?", request.Query)
			} else {
				introMsg = fmt.Sprintf("Encontré %s líneas de investigación relacionadas con tu pregunta sobre '%s'. ¿Sobre cuál te gustaría saber más información?", pluralize(count, "una", "varias"), request.Query)
			}

			finalResponse = ChatResponse{
				ResponseType:  "clarification_options",
				Response:      introMsg,
				Options:       clarificationOptions,
				RetrievedDocs: retrievedDocsForClientResponse,
			}
			c.JSON(http.StatusOK, finalResponse)
			return
		} else {
			log.Println("No clear topic options synthesized, or too few. Proceeding with direct answer.")
		}
	}

	log.Println("Proceeding with direct LLM call for answer generation (either follow-up or no clear topics).")

	session := LoadSession(c)
	if session.ID == "" {
		session = newSession()
	}

	recentHistoryStr := buildContextQuery(session.History, effectiveQuery)
	lastMessage := GetLastMessageContent(session)
	mainPrompt := fmt.Sprintf(`Eres "InvestigaUSACH", un asistente virtual especializado, entusiasta y muy didáctico de la Universidad de Santiago de Chile (USACH). Tu misión es proporcionar información precisa y atractiva sobre la investigación realizada en la USACH, basándote EXCLUSIVAMENTE en el "Contexto Proporcionado" (que puede incluir noticias, artículos científicos en español o inglés). Tu objetivo es que el usuario aprenda y se interese por la investigación de la USACH.

**Instrucciones CRÍTICAS para tu respuesta (SIEMPRE EN ESPAÑOL):**

1.  **Análisis de Pregunta y Contexto:**
    * Interpreta la "Pregunta del usuario" (en español).
    * Analiza CUIDADOSAMENTE el "Contexto Proporcionado". Asume que TODO el contexto es de investigación USACH. Si está en inglés, debes entenderlo y usarlo para tu respuesta en ESPAÑOL.

2.  **Respuesta Basada en Contexto:**
    * **Si encuentras información relevante y directa** para la "Pregunta del usuario" en el "Contexto Proporcionado":
        1.  Inicia con un saludo breve y entusiasta si es el comienzo de una nueva línea de consulta (ej. "¡Hola! Soy InvestigaUSACH...", "¡Excelente pregunta!"). Para seguimientos, sé más directo.
        2.  Resume la información MÁS DIRECTAMENTE RELEVANTE de forma concisa (1-3 frases clave) para responder a la pregunta.
        3.  Si el contexto lo permite, elabora con más detalles, explica conceptos si es necesario, y conecta información de diferentes fragmentos del contexto. Intenta citar de forma general la fuente si es un estudio particular (ej. "Según un estudio de la USACH sobre X...", "La publicación titulada 'Y' indica que...").
        4.  Finaliza con una pregunta abierta y específica que invite al usuario a profundizar en aspectos del tema tratado que SÍ estén cubiertos (o puedan inferirse plausiblemente) por el contexto. Ej: "¿Te gustaría que detallemos la metodología de [aspecto X] o los resultados principales de [aspecto Y]?"

3.  **Manejo de Contexto Insuficiente o Falta de Aspectos Específicos:**
    * **CASO A: La "Pregunta del usuario" es un seguimiento sobre un TEMA CENTRAL ya establecido en la conversación (visible en el "Historial Reciente"), pero el "Contexto Proporcionado" actual NO cubre el ASPECTO ESPECÍFICO solicitado sobre ese TEMA CENTRAL.**
        a.  **NO CAMBIES DE TEMA ABRUPTAMENTE.** NO hables de otros temas del contexto si no se relacionan con el TEMA CENTRAL.
        b.  TU PRIMERA ACCIÓN ES: Declara CLARAMENTE que no tienes información sobre ESE ASPECTO ESPECÍFICO del TEMA CENTRAL.
            Ejemplo: "Respecto a [TEMA CENTRAL, ej: el estudio sobre operadores no locales], en este momento no dispongo de información detallada sobre [ASPECTO ESPECÍFICO PREGUNTADO, ej: sus aplicaciones prácticas] en los documentos que tengo disponibles."
        c.  TU SEGUNDA ACCIÓN ES: Ofrece alternativas DENTRO DEL MISMO TEMA CENTRAL si el contexto (o tu conocimiento del historial reciente) lo permite.
            Ejemplo: "¿Te gustaría que revisemos otros detalles sobre [TEMA CENTRAL], como [otro aspecto del tema central, ej: su metodología o los factores de los que depende el caos], o prefieres que exploremos un área de investigación diferente?"
        d.  SOLO si no hay más que decir sobre el TEMA CENTRAL o el usuario indica querer cambiar, entonces puedes ofrecer explorar otras áreas generales.
    * **CASO B: La "Pregunta del usuario" es sobre un TEMA NUEVO (o el "Contexto Proporcionado" es completamente irrelevante para la pregunta), y el "Contexto Proporcionado" NO contiene información útil.**
        * Indica amablemente que no tienes detalles sobre ese tema. Ejemplo: "He revisado los documentos de investigación de la USACH que tengo disponibles, y no he encontrado información específica sobre [tu tema]. ¿Podría ayudarte con otro tema de investigación de la USACH del cual sí tenga información, por ejemplo, sobre [menciona un tema general del contexto si hay algo, sino omite]?"
    * **NO INVENTES información. NO uses conocimiento externo.**

4.  **Tono y Estilo:**
    * Periodístico, atractivo, entusiasta, didáctico. Evita jerga excesiva (o explícala brevemente).
    * Dirígete al usuario de forma amigable y profesional.

**--- INICIO DEL CONTEXTO PROPORCIONADO (Investigación USACH) ---**
%s
**--- FIN DEL CONTEXTO PROPORCIONADO ---**

**Historial Reciente (Últimos 2-5 intercambios, si aplica, para darte más contexto conversacional):**
%s
**--- FIN DEL HISTORIAL RECENTE ---**

**--- INICIO DE LA ULTIMA RESPUESTA ENTREGADA (en español), si esta vacia omite su utilizacion, (Recuerda que si existe alguna respuesta anterior, ya saludaste al usuario) ---**
%s 
**--- FIN DE LA RESPUESTA PROPORCIONADA ---**

**Pregunta del usuario (en español):**
%s

**InvestigaUSACH Responde (EN ESPAÑOL):**`, contextString, recentHistoryStr, lastMessage, effectiveQuery)

	finalAnswerConfig := &GeminiGenerationConfig{
		Temperature:     0.5,  // Un poco menos creativo para respuestas directas
		MaxOutputTokens: 2000, // Aumentado para evitar cortes
		TopP:            0.95,
	}

	llmResponseText, err := h.getLLMCompletion(mainPrompt, finalAnswerConfig)
	if err != nil {
		log.Printf("Error calling LLM API for final answer: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate response from LLM"})
		return
	}
	log.Println("Successfully received final answer from LLM.")

	session.History = append(session.History, Message{
		Role:    "user",
		Content: request.Query,
		SentAt:  time.Now(),
	})

	session.History = append(session.History, Message{
		Role:    "bot",
		Content: llmResponseText,
		SentAt:  time.Now(),
	})

	// Limitar el tamaño del historial a 10 mensajes (5 user + 5 bot)
	session.History = trimHistory(session.History)

	// 4. Guardar en cookies
	if err := SaveSession(c, session); err != nil {
		log.Printf("Error guardando sesión: %v", err)
	}

	// 5. Debug: imprimir conversación almacenada
	if session.ID != "" {
		fmt.Println("=== DEBUG: Contenido de cookies ===")
		printSessionFromCookie(session)
	}

	responseType := "direct_answer"
	if len(mongoResults) == 0 && !strings.Contains(llmResponseText, "No he encontrado información específica") { // Una heurística simple
		// Si no hay resultados de Mongo Y el LLM no dice explícitamente que no encontró,
		// podría ser una respuesta general o no basada en contexto.
		// Podríamos marcarlo diferente o simplemente confiar en el LLM.
		// Por ahora, lo dejamos como direct_answer, pero si no hay mongoResults, el prompt ya instruye al LLM.
		// Si el LLM responde algo a pesar de no tener contexto, es un fallo del LLM/prompt.
		// El prompt instruye que diga "No se encontró..." si no hay contexto.
		if !strings.Contains(contextString, "No se encontró contexto relevante") {
			// Esto es redundante ya que el prompt maneja el caso de no contexto.
		}
	}
	if strings.Contains(contextString, "No se encontró contexto relevante") && !strings.Contains(llmResponseText, "No he encontrado información específica") {
		log.Println("WARN: LLM generated a response even though context was empty and it didn't state no info found.")
		// Podríamos forzar un mensaje aquí o confiar en el prompt.
	}

	finalResponse = ChatResponse{
		ResponseType:  responseType, // Se podría refinar más si es necesario
		Response:      llmResponseText,
		RetrievedDocs: retrievedDocsForClientResponse,
	}
	c.JSON(http.StatusOK, finalResponse)
}

// --- Funciones Auxiliares ---

func buildRecentHistoryString(history []ChatMessage) string {
	if len(history) == 0 {
		return "(No hay historial previo en esta sesión)"
	}
	var recentHistory strings.Builder
	// Mostrar máximo los últimos 4 mensajes (2 intercambios)
	startIndex := 0
	if len(history) > 4 {
		startIndex = len(history) - 4
	}
	for i := startIndex; i < len(history); i++ {
		role := "Usuario"
		if history[i].Role == "model" || history[i].Role == "bot" || history[i].Role == "asistente" {
			role = "Asistente"
		}
		recentHistory.WriteString(fmt.Sprintf("%s: %s\n", role, history[i].Text))
	}
	return recentHistory.String()
}

func pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- Funciones de API de Google (Embedding y LLM) ---

func (h *ChatHandler) getEmbedding(text string) ([]float32, error) {
	apiURL := googleApiEndpoint + embeddingModel + ":embedContent?key=" + h.googleAPIKey
	if h.googleAPIKey == "" {
		return nil, fmt.Errorf("GOOGLE_API_KEY no está configurada")
	}
	reqBody := GoogleApiEmbeddingRequest{}
	reqBody.Content.Parts = append(reqBody.Content.Parts, struct {
		Text string `json:"text"`
	}{Text: text})

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("error marshalling embedding request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("error creating embedding http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("error making embedding POST request: %w", err)
	}
	defer resp.Body.Close()

	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading embedding response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Embedding API error: status %d, for text: %s", resp.StatusCode, text[:min(100, len(text))])
		return nil, fmt.Errorf("embedding API error: status %d, response: %s", resp.StatusCode, string(respBodyBytes))
	}

	var apiResp GoogleApiEmbeddingResponse
	if err := json.Unmarshal(respBodyBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("error unmarshalling embedding response: %w. Body: %s", err, string(respBodyBytes))
	}
	if len(apiResp.Embedding.Values) == 0 {
		return nil, fmt.Errorf("empty embedding vector in API response for text: %s", text[:min(100, len(text))])
	}
	return apiResp.Embedding.Values, nil
}

func (h *ChatHandler) getLLMCompletion(prompt string, customConfig *GeminiGenerationConfig) (string, error) {
	apiURL := googleApiEndpoint + llmModel + ":generateContent?key=" + h.googleAPIKey
	if h.googleAPIKey == "" {
		return "", fmt.Errorf("GOOGLE_API_KEY no está configurada")
	}

	var genConfig GeminiGenerationConfig
	if customConfig != nil {
		genConfig = *customConfig
		if genConfig.MaxOutputTokens == 0 { // Asegurar un default si no se especifica
			genConfig.MaxOutputTokens = 800
		}
		if genConfig.Temperature == 0 && customConfig.Temperature == 0 { // Evitar que Temperature sea 0.0 a menos que se pida explícitamente
			// Si Temperature es 0.0 en customConfig, se respeta. Si no, y es el valor zero de float32, default.
			// Esto es un poco complicado. Mejor asegurarse de que customConfig siempre tenga valores > 0 o que 0.0 sea válido.
			// Por simplicidad, si es 0.0 y no fue explícitamente seteado, usar un default.
			// Sin embargo, Gemini puede aceptar Temperature = 0.
			// Dejémoslo como está: si customConfig lo tiene, se usa.
		}

	} else {
		genConfig = GeminiGenerationConfig{
			Temperature:     0.7,
			MaxOutputTokens: 800,
		}
	}

	// Loguear la configuración de generación que se usará
	// log.Printf("LLM Generation Config: Temp=%.1f, MaxTokens=%d", genConfig.Temperature, genConfig.MaxOutputTokens)

	geminiReqBody := GeminiApiRequest{
		Contents:         []GeminiContent{{Parts: []GeminiPart{{Text: prompt}}}},
		GenerationConfig: genConfig,
		SafetySettings: []GeminiSafetySetting{
			{Category: "HARM_CATEGORY_HARASSMENT", Threshold: "BLOCK_ONLY_HIGH"}, // Menos restrictivo
			{Category: "HARM_CATEGORY_HATE_SPEECH", Threshold: "BLOCK_ONLY_HIGH"},
			{Category: "HARM_CATEGORY_SEXUALLY_EXPLICIT", Threshold: "BLOCK_ONLY_HIGH"},
			{Category: "HARM_CATEGORY_DANGEROUS_CONTENT", Threshold: "BLOCK_ONLY_HIGH"},
		},
	}

	jsonData, err := json.Marshal(geminiReqBody)
	if err != nil {
		return "", fmt.Errorf("error marshalling LLM request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) // Aumentado a 60s
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("error creating LLM http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("error making LLM POST request: %w", err)
	}
	defer resp.Body.Close()

	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading LLM response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("LLM API error: status %d, request_preview (first 200 chars): %s", resp.StatusCode, prompt[:min(200, len(prompt))])
		return "", fmt.Errorf("LLM API error: status %d, response: %s", resp.StatusCode, string(respBodyBytes))
	}

	var apiResp GeminiApiResponse
	if err := json.Unmarshal(respBodyBytes, &apiResp); err != nil {
		return "", fmt.Errorf("error unmarshalling LLM response: %w. Body: %s", err, string(respBodyBytes))
	}

	if len(apiResp.Candidates) > 0 {
		// Verificar si hay contenido en el candidato
		if len(apiResp.Candidates[0].Content.Parts) == 0 {
			// El candidato existe pero no tiene contenido
			if apiResp.Candidates[0].FinishReason == "MAX_TOKENS" {
				log.Printf("ERROR: LLM hit MAX_TOKENS with no content generated")
				return "", fmt.Errorf("el modelo alcanzó el límite de tokens antes de generar una respuesta")
			}
			log.Printf("WARN: Candidate exists but has no content parts")
		} else {
			responseText := apiResp.Candidates[0].Content.Parts[0].Text
			if apiResp.Candidates[0].FinishReason == "MAX_TOKENS" && responseText == "" {
				log.Printf("ERROR: LLM hit MAX_TOKENS but returned empty text. This suggests the prompt was too long.")
				return "", fmt.Errorf("el modelo alcanzó el límite de tokens sin generar respuesta")
			}
			if apiResp.Candidates[0].FinishReason != "STOP" && apiResp.Candidates[0].FinishReason != "" && apiResp.Candidates[0].FinishReason != "MAX_TOKENS" {
				log.Printf("WARN: Gemini response finishReason was '%s'.", apiResp.Candidates[0].FinishReason)
				if apiResp.Candidates[0].FinishReason == "SAFETY" {
					// Si hay texto parcial y fue por seguridad, devolverlo con una advertencia.
					if responseText != "" {
						return responseText + "\n\n(InvestigaUSACH: Mi respuesta pudo haber sido cortada o modificada debido a las políticas de contenido.)", nil
					}
					return "(InvestigaUSACH: No pude generar una respuesta completa debido a las políticas de contenido.)", nil
				}
			}
			return responseText, nil
		}
	}

	if apiResp.PromptFeedback != nil && len(apiResp.PromptFeedback.SafetyRatings) > 0 {
		isBlocked := false
		for _, rating := range apiResp.PromptFeedback.SafetyRatings {
			// BLOCK_ONLY_HIGH significa que solo se bloquea si es ALTO.
			// Si la probabilidad es MEDIUM o LOW para una categoría seteada en BLOCK_ONLY_HIGH, no debería bloquearse.
			// El problema es si el rating.Probability es ALTO para una categoría seteada en BLOCK_MEDIUM_AND_ABOVE o similar.
			// Sin embargo, el API devuelve un error HTTP 400 con reason "SAFETY" si el prompt es bloqueado.
			// Aquí, el PromptFeedback es más informativo si la respuesta SÍ se generó pero con advertencias.
			// Si la respuesta está vacía Y hay safety ratings en el prompt, es un problema.
			log.Printf("WARN: PromptFeedback SafetyRating: Category=%s, Probability=%s (Umbral: BLOCK_ONLY_HIGH)", rating.Category, rating.Threshold) // Threshold aquí es en realidad Probability en la respuesta
			// Si alguna categoría está bloqueada según los ratings del prompt (esto es más complejo de determinar solo con el feedback)
			// if rating.Blocked { isBlocked = true; break } // Si el API tuviera un campo "Blocked" directo en el rating
		}
		if isBlocked { // Esta flag 'isBlocked' no se setea bien con la estructura actual del API
			log.Printf("WARN: Prompt feedback indicates prompt was blocked due to safety ratings: %+v. Full response: %s", apiResp.PromptFeedback.SafetyRatings, string(respBodyBytes))
			return "(InvestigaUSACH: El tema de tu pregunta pudo haber activado filtros de contenido y no pude generar una respuesta.)", nil
		}
	}

	log.Printf("WARN: LLM response was empty or structure was unexpected. Full response: %s", string(respBodyBytes))
	return "(InvestigaUSACH: Hubo un inconveniente al generar la respuesta. Intenta reformular tu pregunta.)", nil // Mensaje genérico
}
