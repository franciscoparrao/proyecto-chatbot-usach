package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Estructura para artículos con afiliación USACH detectada
type USACHArticle struct {
	ID                      primitive.ObjectID `bson:"_id,omitempty"`
	MongoArticleID          primitive.ObjectID `bson:"mongo_article_id"`
	OriginalTitle           string             `bson:"original_title"`
	OriginalAuthors         string             `bson:"original_authors"`
	OriginalPublicationDate string             `bson:"original_publication_date"`
	USACHMentions           []string           `bson:"usach_mentions"`
	ConfidenceScore         float32            `bson:"confidence_score"`
	LastAuthor              string             `bson:"last_author"`
	DetectedAt              time.Time          `bson:"detected_at"`
}

// Patrones para detectar afiliación USACH
var usachPatterns = []string{
	`(?i)universidad\s+de\s+santiago\s+de\s+chile`,
	`(?i)university\s+of\s+santiago\s+de\s+chile`,
	`(?i)\bUSACH\b`,
	`(?i)univ\.\s+santiago`,
	`(?i)u\.\s+de\s+santiago`,
	`(?i)santiago\s+de\s+chile.*university`,
	`(?i)departamento.*universidad.*santiago`,
	`(?i)facultad.*universidad.*santiago`,
}

// Compilar patrones regex
var compiledPatterns []*regexp.Regexp

func init() {
	for _, pattern := range usachPatterns {
		compiledPatterns = append(compiledPatterns, regexp.MustCompile(pattern))
	}
}

func main() {
	// Cargar variables de entorno
	err := godotenv.Load()
	if err != nil {
		log.Printf("Warning: Error loading .env file: %v\n", err)
	}

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27019"
	}

	// Conectar a MongoDB
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(context.Background())

	// Verificar conexión
	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatalf("Failed to ping MongoDB: %v", err)
	}
	log.Println("Connected to MongoDB!")

	// Colecciones
	db := client.Database("investigacion_usach_db")
	articlesCollection := db.Collection("articulos_wos")
	usachCollection := db.Collection("articulos_usach_afiliacion")

	// Crear índice para búsquedas eficientes
	_, err = usachCollection.Indexes().CreateOne(
		context.Background(),
		mongo.IndexModel{
			Keys: bson.D{{"mongo_article_id", 1}},
			Options: options.Index().SetUnique(true),
		},
	)
	if err != nil {
		log.Printf("Warning: Could not create index: %v", err)
	}

	// Obtener todos los artículos únicos
	pipeline := mongo.Pipeline{
		{{"$group", bson.D{
			{"_id", bson.D{
				{"title", "$originaltitle"},
				{"authors", "$originalauthors"},
				{"date", "$originalpublicationdate"},
			}},
			{"article_id", bson.D{{"$first", "$_id"}}},
			{"chunk_text", bson.D{{"$push", "$chunktext"}}},
		}}},
	}

	cursor, err := articlesCollection.Aggregate(context.Background(), pipeline)
	if err != nil {
		log.Fatalf("Failed to aggregate articles: %v", err)
	}
	defer cursor.Close(context.Background())

	totalArticles := 0
	usachArticles := 0
	highConfidenceArticles := 0

	// Analizar cada artículo
	for cursor.Next(context.Background()) {
		var result struct {
			ID struct {
				Title   string `bson:"title"`
				Authors string `bson:"authors"`
				Date    string `bson:"date"`
			} `bson:"_id"`
			ArticleID primitive.ObjectID `bson:"article_id"`
			ChunkText []string           `bson:"chunk_text"`
		}

		if err := cursor.Decode(&result); err != nil {
			log.Printf("Error decoding document: %v", err)
			continue
		}

		totalArticles++

		// Combinar todos los chunks
		fullText := strings.Join(result.ChunkText, " ")
		
		// Buscar menciones de USACH
		mentions := findUSACHMentions(fullText)
		
		if len(mentions) > 0 {
			// Calcular score de confianza
			confidenceScore := calculateConfidenceScore(mentions, result.ID.Authors, fullText)
			
			// Obtener último autor
			lastAuthor := getLastAuthor(result.ID.Authors)
			
			// Crear documento USACH
			usachArticle := USACHArticle{
				MongoArticleID:          result.ArticleID,
				OriginalTitle:           result.ID.Title,
				OriginalAuthors:         result.ID.Authors,
				OriginalPublicationDate: result.ID.Date,
				USACHMentions:           mentions,
				ConfidenceScore:         confidenceScore,
				LastAuthor:              lastAuthor,
				DetectedAt:              time.Now(),
			}
			
			// Guardar en colección de artículos USACH
			opts := options.Update().SetUpsert(true)
			_, err := usachCollection.UpdateOne(
				context.Background(),
				bson.M{"mongo_article_id": result.ArticleID},
				bson.M{"$set": usachArticle},
				opts,
			)
			
			if err != nil {
				log.Printf("Error saving USACH article: %v", err)
			} else {
				usachArticles++
				if confidenceScore > 0.8 {
					highConfidenceArticles++
				}
				
				// Log de artículos encontrados
				fmt.Printf("\n[USACH Article Found]\n")
				fmt.Printf("Title: %s\n", result.ID.Title)
				fmt.Printf("Authors: %s\n", result.ID.Authors)
				fmt.Printf("Last Author: %s\n", lastAuthor)
				fmt.Printf("Mentions: %v\n", mentions)
				fmt.Printf("Confidence: %.2f\n", confidenceScore)
			}
		}
		
		// Progreso
		if totalArticles%100 == 0 {
			log.Printf("Processed %d articles, found %d with USACH affiliation", totalArticles, usachArticles)
		}
	}

	// Resumen final
	fmt.Printf("\n=== Analysis Complete ===\n")
	fmt.Printf("Total articles analyzed: %d\n", totalArticles)
	fmt.Printf("Articles with USACH affiliation: %d (%.1f%%)\n", 
		usachArticles, float64(usachArticles)/float64(totalArticles)*100)
	fmt.Printf("High confidence articles: %d\n", highConfidenceArticles)

	// Guardar resumen en archivo
	summary := map[string]interface{}{
		"analysis_date":           time.Now(),
		"total_articles":          totalArticles,
		"usach_articles":          usachArticles,
		"high_confidence_articles": highConfidenceArticles,
		"percentage":              float64(usachArticles) / float64(totalArticles) * 100,
	}

	summaryJSON, _ := json.MarshalIndent(summary, "", "  ")
	err = os.WriteFile("usach_affiliation_summary.json", summaryJSON, 0644)
	if err != nil {
		log.Printf("Error writing summary file: %v", err)
	}
}

// findUSACHMentions busca menciones de USACH en el texto
func findUSACHMentions(text string) []string {
	var mentions []string
	mentionMap := make(map[string]bool)
	
	for _, pattern := range compiledPatterns {
		matches := pattern.FindAllString(text, -1)
		for _, match := range matches {
			// Normalizar y evitar duplicados
			normalized := strings.TrimSpace(match)
			if !mentionMap[normalized] {
				mentionMap[normalized] = true
				mentions = append(mentions, normalized)
			}
		}
	}
	
	return mentions
}

// calculateConfidenceScore calcula un score de confianza basado en varios factores
func calculateConfidenceScore(mentions []string, authors, fullText string) float32 {
	score := float32(0.0)
	
	// Más menciones = mayor confianza
	mentionScore := float32(len(mentions)) * 0.2
	if mentionScore > 0.5 {
		mentionScore = 0.5
	}
	score += mentionScore
	
	// Si aparece "Universidad de Santiago de Chile" completo, alta confianza
	for _, mention := range mentions {
		if strings.Contains(strings.ToLower(mention), "universidad de santiago de chile") {
			score += 0.3
			break
		}
	}
	
	// Si aparece en contexto de afiliación
	affiliationContext := []string{"affiliation", "afiliación", "institution", "institución", "department", "departamento", "faculty", "facultad"}
	for _, context := range affiliationContext {
		if strings.Contains(strings.ToLower(fullText), context) {
			score += 0.1
			break
		}
	}
	
	// Si el último autor tiene nombre chileno común (heurística simple)
	lastAuthor := getLastAuthor(authors)
	chileanSurnames := []string{"González", "Rodríguez", "Muñoz", "Rojas", "Díaz", "Pérez", "Soto", "Contreras", "Silva", "Martínez"}
	for _, surname := range chileanSurnames {
		if strings.Contains(lastAuthor, surname) {
			score += 0.1
			break
		}
	}
	
	// Normalizar score
	if score > 1.0 {
		score = 1.0
	}
	
	return score
}

// getLastAuthor extrae el último autor de la lista
func getLastAuthor(authors string) string {
	// Manejar diferentes formatos de separación
	var authorList []string
	
	if strings.Contains(authors, ";") {
		authorList = strings.Split(authors, ";")
	} else if strings.Contains(authors, ",") && !strings.Contains(authors, ", ") {
		// Probablemente separado por comas sin espacios
		authorList = strings.Split(authors, ",")
	} else {
		// Asumir un solo autor o formato desconocido
		return strings.TrimSpace(authors)
	}
	
	if len(authorList) > 0 {
		return strings.TrimSpace(authorList[len(authorList)-1])
	}
	
	return ""
}