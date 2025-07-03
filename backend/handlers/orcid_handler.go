package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Constantes ORCID
const (
	orcidAPIURL        = "https://pub.orcid.org/v3.0"
	orcidSandboxAPIURL = "https://pub.sandbox.orcid.org/v3.0"
	cacheExpiration    = 30 * 24 * time.Hour // 30 días
)

// Estructuras de datos
type ORCIDHandler struct {
	httpClient     *http.Client
	accessToken    string
	mongoClient    *mongo.Client
	cacheDB        *mongo.Collection
	useSandbox     bool
}

type AuthorORCIDInfo struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	AuthorName   string             `bson:"author_name" json:"author_name"`
	ORCIDID      string             `bson:"orcid_id,omitempty" json:"orcid_id,omitempty"`
	Affiliations []Affiliation      `bson:"affiliations" json:"affiliations"`
	IsUSACH      bool               `bson:"is_usach" json:"is_usach"`
	LastUpdated  time.Time          `bson:"last_updated" json:"last_updated"`
	TTL          time.Time          `bson:"ttl" json:"-"` // Para TTL index de MongoDB
}

type Affiliation struct {
	OrganizationName string `bson:"organization_name" json:"organization_name"`
	StartDate        string `bson:"start_date,omitempty" json:"start_date,omitempty"`
	EndDate          string `bson:"end_date,omitempty" json:"end_date,omitempty"`
	RoleTitle        string `bson:"role_title,omitempty" json:"role_title,omitempty"`
	Current          bool   `bson:"current" json:"current"`
}

// Estructuras de respuesta ORCID API
type ORCIDSearchResponse struct {
	NumFound    int                    `json:"num-found"`
	Start       int                    `json:"start"`
	TotalPages  int                    `json:"num-pages"`
	Result      []ORCIDSearchResult    `json:"result"`
}

type ORCIDSearchResult struct {
	ORCIDIdentifier struct {
		URI  string `json:"uri"`
		Path string `json:"path"`
		Host string `json:"host"`
	} `json:"orcid-identifier"`
}

type ORCIDExpandedSearchResponse struct {
	NumFound    int                          `json:"num-found"`
	Results     []ORCIDExpandedSearchResult  `json:"expanded-result"`
}

type ORCIDExpandedSearchResult struct {
	ORCIDID     string   `json:"orcid-id"`
	GivenNames  string   `json:"given-names"`
	FamilyName  string   `json:"family-names"`
	Institution []string `json:"institution-name"`
}

// Constructor
func NewORCIDHandler(mongoClient *mongo.Client, accessToken string, useSandbox bool) *ORCIDHandler {
	cacheCollection := mongoClient.Database("investigacion_usach_db").Collection("orcid_cache")
	
	// Crear índice TTL para expiración automática
	indexModel := mongo.IndexModel{
		Keys:    bson.D{{"ttl", 1}},
		Options: options.Index().SetExpireAfterSeconds(0),
	}
	cacheCollection.Indexes().CreateOne(context.Background(), indexModel)
	
	return &ORCIDHandler{
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		accessToken: accessToken,
		mongoClient: mongoClient,
		cacheDB:     cacheCollection,
		useSandbox:  useSandbox,
	}
}

// Métodos principales

// SearchAuthor busca un autor por nombre y opcionalmente por afiliación
func (h *ORCIDHandler) SearchAuthor(c *gin.Context) {
	name := c.Query("name")
	affiliation := c.Query("affiliation")
	
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name parameter is required"})
		return
	}
	
	// Verificar caché primero
	cached, err := h.getCachedAuthorInfo(name)
	if err == nil && cached != nil {
		c.JSON(http.StatusOK, cached)
		return
	}
	
	// Buscar en ORCID API
	results, err := h.searchORCID(name, affiliation)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("ORCID search failed: %v", err)})
		return
	}
	
	// Si encontramos resultados, obtener detalles y guardar en caché
	if len(results) > 0 {
		authorInfo := h.processSearchResults(name, results, affiliation)
		h.cacheAuthorInfo(authorInfo)
		c.JSON(http.StatusOK, authorInfo)
		return
	}
	
	// No se encontraron resultados
	c.JSON(http.StatusOK, gin.H{
		"author_name": name,
		"message": "No ORCID found for this author",
		"is_usach": false,
	})
}

// GetAuthorByORCID obtiene información detallada de un autor por su ORCID ID
func (h *ORCIDHandler) GetAuthorByORCID(c *gin.Context) {
	orcidID := c.Param("orcid_id")
	
	if orcidID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "orcid_id is required"})
		return
	}
	
	// TODO: Implementar obtención de perfil completo desde ORCID
	c.JSON(http.StatusNotImplemented, gin.H{"message": "Full profile retrieval not yet implemented"})
}

// GetArticleAuthorsInfo obtiene información de todos los autores de un artículo
func (h *ORCIDHandler) GetArticleAuthorsInfo(c *gin.Context) {
	articleID := c.Param("article_id")
	
	if articleID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "article_id is required"})
		return
	}
	
	// Obtener el artículo de MongoDB
	objID, err := primitive.ObjectIDFromHex(articleID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid article ID"})
		return
	}
	
	collection := h.mongoClient.Database("investigacion_usach_db").Collection("articulos_wos")
	var article struct {
		OriginalAuthors string `bson:"originalauthors"`
		OriginalTitle   string `bson:"originaltitle"`
	}
	
	err = collection.FindOne(context.Background(), bson.M{"_id": objID}).Decode(&article)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "Article not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		}
		return
	}
	
	// Procesar lista de autores
	authors := strings.Split(article.OriginalAuthors, ";")
	var authorsInfo []AuthorORCIDInfo
	
	for _, author := range authors {
		author = strings.TrimSpace(author)
		if author == "" {
			continue
		}
		
		// Buscar información del autor (primero en caché, luego en ORCID)
		info, _ := h.getCachedAuthorInfo(author)
		if info == nil {
			// Buscar en ORCID con afiliación USACH
			results, err := h.searchORCID(author, "Universidad de Santiago de Chile")
			if err == nil && len(results) > 0 {
				info = h.processSearchResults(author, results, "Universidad de Santiago de Chile")
				h.cacheAuthorInfo(info)
			} else {
				// Crear entrada vacía si no se encuentra
				info = &AuthorORCIDInfo{
					AuthorName:  author,
					IsUSACH:     false,
					LastUpdated: time.Now(),
				}
			}
		}
		
		authorsInfo = append(authorsInfo, *info)
	}
	
	c.JSON(http.StatusOK, gin.H{
		"article_title": article.OriginalTitle,
		"authors":       authorsInfo,
	})
}

// Métodos auxiliares

func (h *ORCIDHandler) getAPIBaseURL() string {
	if h.useSandbox {
		return orcidSandboxAPIURL
	}
	return orcidAPIURL
}

func (h *ORCIDHandler) searchORCID(name, affiliation string) ([]ORCIDSearchResult, error) {
	// Construir query
	nameParts := strings.Split(name, " ")
	var queryParts []string
	
	// Buscar por apellido y nombre
	if len(nameParts) >= 2 {
		// Asumir formato "Apellido, Nombre" o "Nombre Apellido"
		if strings.Contains(name, ",") {
			parts := strings.Split(name, ",")
			queryParts = append(queryParts, fmt.Sprintf("family-name:%s", strings.TrimSpace(parts[0])))
			if len(parts) > 1 {
				queryParts = append(queryParts, fmt.Sprintf("given-names:%s", strings.TrimSpace(parts[1])))
			}
		} else {
			// Intentar ambas combinaciones
			queryParts = append(queryParts, fmt.Sprintf("(family-name:%s OR given-names:%s)", nameParts[0], nameParts[0]))
			queryParts = append(queryParts, fmt.Sprintf("(family-name:%s OR given-names:%s)", nameParts[len(nameParts)-1], nameParts[len(nameParts)-1]))
		}
	} else {
		queryParts = append(queryParts, name)
	}
	
	query := strings.Join(queryParts, " AND ")
	
	if affiliation != "" {
		query += fmt.Sprintf(" AND affiliation-org-name:\"%s\"", affiliation)
	}
	
	// Construir URL
	searchURL := fmt.Sprintf("%s/search/?q=%s", h.getAPIBaseURL(), url.QueryEscape(query))
	
	// Crear request
	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", h.accessToken))
	
	// Ejecutar request
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ORCID API returned status %d", resp.StatusCode)
	}
	
	// Decodificar respuesta
	var searchResponse ORCIDSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResponse); err != nil {
		return nil, err
	}
	
	return searchResponse.Result, nil
}

func (h *ORCIDHandler) processSearchResults(authorName string, results []ORCIDSearchResult, affiliation string) *AuthorORCIDInfo {
	info := &AuthorORCIDInfo{
		AuthorName:   authorName,
		LastUpdated:  time.Now(),
		TTL:          time.Now().Add(cacheExpiration),
		Affiliations: []Affiliation{},
	}
	
	// Por ahora, tomar el primer resultado
	if len(results) > 0 {
		info.ORCIDID = results[0].ORCIDIdentifier.Path
		
		// Si buscamos con afiliación USACH y encontramos resultados, marcar como USACH
		if strings.Contains(strings.ToLower(affiliation), "santiago") {
			info.IsUSACH = true
			info.Affiliations = append(info.Affiliations, Affiliation{
				OrganizationName: "Universidad de Santiago de Chile",
				Current:          true, // Asumimos que es actual si aparece en la búsqueda
			})
		}
	}
	
	return info
}

func (h *ORCIDHandler) getCachedAuthorInfo(authorName string) (*AuthorORCIDInfo, error) {
	var cached AuthorORCIDInfo
	err := h.cacheDB.FindOne(
		context.Background(),
		bson.M{"author_name": authorName},
	).Decode(&cached)
	
	if err != nil {
		return nil, err
	}
	
	return &cached, nil
}

func (h *ORCIDHandler) cacheAuthorInfo(info *AuthorORCIDInfo) error {
	_, err := h.cacheDB.UpdateOne(
		context.Background(),
		bson.M{"author_name": info.AuthorName},
		bson.M{"$set": info},
		options.Update().SetUpsert(true),
	)
	return err
}

// EnrichArticlesWithORCID es una función para ejecutar en batch y enriquecer artículos con información ORCID
func (h *ORCIDHandler) EnrichArticlesWithORCID(limit int) error {
	collection := h.mongoClient.Database("investigacion_usach_db").Collection("articulos_wos")
	
	// Obtener artículos únicos
	pipeline := mongo.Pipeline{
		{{"$group", bson.D{
			{"_id", "$originalauthors"},
			{"count", bson.D{{"$sum", 1}}},
		}}},
		{{"$limit", limit}},
	}
	
	cursor, err := collection.Aggregate(context.Background(), pipeline)
	if err != nil {
		return err
	}
	defer cursor.Close(context.Background())
	
	processedCount := 0
	usachCount := 0
	
	for cursor.Next(context.Background()) {
		var result struct {
			Authors string `bson:"_id"`
			Count   int    `bson:"count"`
		}
		
		if err := cursor.Decode(&result); err != nil {
			continue
		}
		
		// Procesar cada autor
		authors := strings.Split(result.Authors, ";")
		for _, author := range authors {
			author = strings.TrimSpace(author)
			if author == "" {
				continue
			}
			
			// Verificar si ya está en caché
			cached, _ := h.getCachedAuthorInfo(author)
			if cached != nil {
				if cached.IsUSACH {
					usachCount++
				}
				continue
			}
			
			// Buscar en ORCID con afiliación USACH
			results, err := h.searchORCID(author, "Universidad de Santiago de Chile")
			if err != nil {
				fmt.Printf("Error searching ORCID for %s: %v\n", author, err)
				continue
			}
			
			if len(results) > 0 {
				info := h.processSearchResults(author, results, "Universidad de Santiago de Chile")
				h.cacheAuthorInfo(info)
				if info.IsUSACH {
					usachCount++
				}
				processedCount++
			}
			
			// Rate limiting
			time.Sleep(50 * time.Millisecond) // 20 requests per second max
		}
	}
	
	fmt.Printf("Enrichment complete: processed %d authors, found %d USACH affiliations\n", processedCount, usachCount)
	return nil
}