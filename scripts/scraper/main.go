package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
)

type Article struct {
	Title           string `json:"title"`
	Authors         string `json:"authors"`
	PublicationDate string `json:"publication_date"`
	RawText         string `json:"raw_text"`
}

func main() {
	var articles []Article

	c := colly.NewCollector(
		colly.AllowedDomains("usach.cl", "www.usach.cl"),
		colly.UserAgent("USachResearchChatbotScraper/1.0 (+http://your-project-info-or-contact)"),
	)

	err := c.Limit(&colly.LimitRule{
		DomainGlob:  "*.usach.cl",
		Parallelism: 1,
		Delay:       2 * time.Second,
		RandomDelay: 1 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to set limit rule: %v", err)
	}

	c.OnError(func(r *colly.Response, err error) {
		// Solo loguear errores HTTP reales, no necesariamente errores de scraping como 404
		if err != nil || r.StatusCode >= 400 {
			log.Printf("Error fetching %s: Status %d, Error %v\n", r.Request.URL, r.StatusCode, err)
		}
	})

	// --- Extracción en la página del artículo ---
	// *** CAMBIO CLAVE: Usamos el BODY específico de páginas de artículo como contexto ***
	c.OnHTML("body.page-node-type-article", func(e *colly.HTMLElement) {
		log.Printf("DEBUG: Processing article page %s using body context\n", e.Request.URL.String())

		// fecha
		articleDate := strings.TrimSpace(e.ChildText("time.datetime"))
		log.Printf("DEBUG: Date found: '%s'\n", articleDate)

		// autor
		articleAuthor := strings.TrimSpace(e.ChildText(".field--name-field-autor a"))
		log.Printf("DEBUG: Author found: '%s'\n", articleAuthor)

		// --- TÍTULO ---
		// Usamos el selector más preciso confirmado por el debug, relativo al body
		articleTitle := strings.TrimSpace(e.ChildText("#block-usach-page-title h1 span.field--name-title"))
		if articleTitle == "" { // Fallback por si el ID cambia o no está
			articleTitle = strings.TrimSpace(e.ChildText("h1 span.field--name-title"))
		}
		log.Printf("DEBUG: Title found: '%s'\n", articleTitle)

		// --- CUERPO ---
		// Usamos el selector que funcionó consistentemente, relativo al body
		var bodyTextBuilder strings.Builder
		foundBodyParagraphs := false
		// Buscamos dentro de div.node__content que está dentro del body
		e.ForEach("div.node__content p", func(_ int, p *colly.HTMLElement) {
			foundBodyParagraphs = true
			paragraph := strings.TrimSpace(p.Text)
			if paragraph != "" {
				isLikelyExternalLink := p.ChildAttr("a[href]", "href") != "" && len(p.ChildAttrs("a[href]", "href")) == 1 && strings.TrimSpace(p.ChildText("a")) == paragraph
				containsNature := strings.Contains(p.ChildAttr("a[href]", "href"), "nature.com")
				if !isLikelyExternalLink && !containsNature {
					bodyTextBuilder.WriteString(paragraph)
					bodyTextBuilder.WriteString("\n\n")
				}
			}
		})

		if !foundBodyParagraphs {
			log.Printf("WARNING: No body paragraphs found using 'div.node__content p' for %s\n", e.Request.URL.String())
		}
		articleText := strings.TrimSpace(bodyTextBuilder.String())

		// --- Guardado ---
		if articleTitle != "" && articleText != "" {
			article := Article{
				Title:           articleTitle,
				Authors:         articleAuthor,
				PublicationDate: articleDate,
				RawText:         articleText,
			}
			log.Printf("SUCCESS: Scraped: '%s' from %s\n", article.Title)
			// Evitar duplicados
			found := false
			if !found {
				articles = append(articles, article)
			}
		} else {
			missing := []string{}
			if articleTitle == "" {
				missing = append(missing, "Title")
			}
			if articleText == "" {
				missing = append(missing, "Body Text")
			}
			log.Printf("Warning: Missing %s at %s\n", strings.Join(missing, " and "), e.Request.URL.String())
		}
	})

	// --- Navegación en páginas índice/listado ---
	// Selector confirmado para enlaces a artículos
	c.OnHTML("div.view-content li div.caja_t a[href^=\"/news/\"]", func(e *colly.HTMLElement) {
		link := e.Request.AbsoluteURL(e.Attr("href"))
		if strings.Contains(link, "/news/") && !strings.Contains(link, "?page=") && strings.Contains(link, "usach.cl") {
			log.Printf("Found article link: %s\n", link)
			err := c.Visit(link)
			if err != nil {
				log.Printf("Non-fatal error visiting article link %s: %v\n", link, err)
			}
		}
	})

	// --- PAGINATION LINK HANDLER (No confirmado, probablemente innecesario o incorrecto) ---
	c.OnHTML("li.pager__item--next a[href]", func(e *colly.HTMLElement) { // GUESS
		nextPageLink := e.Request.AbsoluteURL(e.Attr("href"))
		log.Printf("Found potential next page link (selector needs verification): %s\n", nextPageLink)
		err := c.Visit(nextPageLink)
		if err != nil {
			log.Printf("Non-fatal error visiting next page link %s: %v\n", nextPageLink, err)
		}
	})

	// --- Inicio del Scraping ---
	startURL := "https://usach.cl/lista-noticias"

	log.Println("Starting scrape at:", startURL)
	err = c.Visit(startURL)
	if err != nil {
		log.Fatalf("FATAL: Failed to visit start URL %s: %v\n", startURL, err)
	}

	c.Wait()

	log.Printf("Scraping finished. Found %d unique articles.\n", len(articles))

	// --- Guardar Resultados en JSON ---
	if len(articles) > 0 {
		outputFileName := "../processor/files/scraped_articles_usach.json"
		log.Printf("Writing results to %s...\n", outputFileName)
		jsonData, err := json.MarshalIndent(articles, "", "  ")
		if err != nil {
			log.Fatalf("Failed to marshal articles to JSON: %v\n", err)
		}
		err = os.WriteFile(outputFileName, jsonData, 0644)
		if err != nil {
			log.Fatalf("Failed to write JSON to file %s: %v\n", outputFileName, err)
		}
		log.Printf("Successfully wrote %d articles to %s\n", len(articles), outputFileName)
	} else {
		log.Println("No articles were scraped successfully. No output file generated.")
	}
}
