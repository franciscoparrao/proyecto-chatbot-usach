package main

import (
	"encoding/json"

	"bufio"

	"fmt"

	"strings"

	"github.com/xuri/excelize/v2"

	"os"

	"log"
)

type Article struct {
	Title           string `json:"title"`
	Authors         string `json:"authors"`
	PublicationDate string `json:"publication_date"`
	RawText         string `json:"raw_text"`
}

func main() {

	file, err := os.Open("files.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	var articles []Article

	for scanner.Scan() {
		readTxt := scanner.Text()
		readTxt = "files/" + readTxt

		file, err := excelize.OpenFile(readTxt)

		defer file.Close()

		if err != nil {
			fmt.Println(err)
			return
		}

		file.RemoveRow("savedrecs", 1)

		rows, err := file.GetRows("savedrecs")
		if err != nil {
			fmt.Println(err)
			return
		}

		for _, row := range rows {
			title := row[8]
			authors := row[5]
			rawText := row[21]
			publicationDate := strings.Replace(row[46], ",", "", 1) + " " + row[45]

			articleJson := Article{
				Title:           title,
				Authors:         authors,
				RawText:         rawText,
				PublicationDate: publicationDate,
			}
			articles = append(articles, articleJson)
		}
	}

	outputFileName := "../processor/files/scraped_articles_wos.json"
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
}
