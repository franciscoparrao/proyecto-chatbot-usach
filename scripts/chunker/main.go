package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"
)

// --- Constantes ---
const (
	inputFile      = "../scraper/scraped_articles.json"   // Ruta RELATIVA al archivo del scraper
	outputFile     = "../chunk-embeddings/processed_chunks.json" // Archivo de salida en esta carpeta
	minChunkLength = 50                                   // Longitud mínima (en caracteres) para considerar un chunk válido
)

// --- Estructuras ---

// ScrapedArticle debe coincidir con la estructura guardada por el scraper
type ScrapedArticle struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	RawText string `json:"raw_text"`
}

// ProcessedChunk representa un fragmento de texto listo para embedding (añadiremos el vector luego)
type ProcessedChunk struct {
	OriginalURL   string `json:"original_url"`
	OriginalTitle string `json:"original_title"`
	ChunkText     string `json:"chunk_text"`
	// EmbeddingVector []float32 \`json:"embedding_vector"\` // Lo añadiremos en el siguiente paso
}

func main() {
	log.Println("Starting processing script...")

	// --- 1. Leer el archivo JSON del scraper ---
	log.Printf("Reading input file: %s\n", inputFile)
	jsonData, err := os.ReadFile(inputFile)
	if err != nil {
		log.Fatalf("FATAL: Error reading file %s: %v\n", inputFile, err)
	}

	// --- 2. Decodificar (Unmarshal) JSON en structs ---
	var scrapedArticles []ScrapedArticle
	err = json.Unmarshal(jsonData, &scrapedArticles)
	if err != nil {
		log.Fatalf("FATAL: Error unmarshalling JSON from %s: %v\n", inputFile, err)
	}
	log.Printf("Successfully read %d articles from JSON.\n", len(scrapedArticles))

	// --- 3. Procesar cada artículo: Chunking y Limpieza Básica ---
	var allChunks []ProcessedChunk
	totalChunks := 0

	for i, article := range scrapedArticles {
		log.Printf("Processing article %d: '%s' (%s)\n", i+1, article.Title, article.URL)

		cleanedText := strings.TrimSpace(article.RawText)

		if cleanedText == "" {
			log.Printf("  Skipping article %d because RawText is empty.\n", i+1)
			continue
		}

		paragraphs := strings.Split(cleanedText, "\n\n")
		articleChunks := 0

		// *** CORRECCIÓN AQUÍ: Reemplazar 'j' con '_' ***
		for _, para := range paragraphs { // <--- Cambiado j por _
			chunkText := strings.TrimSpace(para)

			if len(chunkText) >= minChunkLength {
				processedChunk := ProcessedChunk{
					OriginalURL:   article.URL,
					OriginalTitle: article.Title,
					ChunkText:     chunkText,
				}
				allChunks = append(allChunks, processedChunk)
				articleChunks++
			}
			// Las líneas comentadas que usaban 'j' pueden quedarse comentadas o eliminarse.
		}
		log.Printf("  Generated %d valid chunks for article %d.\n", articleChunks, i+1)
		totalChunks += articleChunks
	}

	log.Printf("Processing complete. Total valid chunks generated: %d\n", totalChunks)

	// --- 4. Guardar los chunks procesados en un nuevo archivo JSON ---
	if len(allChunks) > 0 {
		log.Printf("Writing %d processed chunks to %s...\n", len(allChunks), outputFile)
		chunkData, err := json.MarshalIndent(allChunks, "", "  ")
		if err != nil {
			log.Fatalf("FATAL: Failed to marshal chunks to JSON: %v\n", err)
		}

		err = os.WriteFile(outputFile, chunkData, 0644)
		if err != nil {
			log.Fatalf("FATAL: Failed to write JSON to file %s: %v\n", outputFile, err)
		}
		log.Printf("Successfully wrote processed chunks to %s\n", outputFile)
	} else {
		log.Println("No valid chunks were generated. No output file created.")
	}

	log.Println("Processing script finished.")
}
