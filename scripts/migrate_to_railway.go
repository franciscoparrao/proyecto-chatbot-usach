package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	// Get Railway MongoDB URI from environment or arguments
	railwayURI := os.Getenv("RAILWAY_MONGO_URI")
	if railwayURI == "" && len(os.Args) > 1 {
		railwayURI = os.Args[1]
	}
	if railwayURI == "" {
		log.Fatal("Please provide RAILWAY_MONGO_URI as environment variable or first argument")
	}

	// Local MongoDB URI
	localURI := "mongodb://localhost:27019"

	fmt.Println("🚀 Starting MongoDB migration from local to Railway...")
	fmt.Printf("Source: %s\n", localURI)
	fmt.Printf("Destination: %s\n", railwayURI)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Connect to local MongoDB
	localClient, err := mongo.Connect(ctx, options.Client().ApplyURI(localURI))
	if err != nil {
		log.Fatal("Failed to connect to local MongoDB:", err)
	}
	defer localClient.Disconnect(ctx)

	// Connect to Railway MongoDB
	railwayClient, err := mongo.Connect(ctx, options.Client().ApplyURI(railwayURI))
	if err != nil {
		log.Fatal("Failed to connect to Railway MongoDB:", err)
	}
	defer railwayClient.Disconnect(ctx)

	// Test connections
	if err := localClient.Ping(ctx, nil); err != nil {
		log.Fatal("Failed to ping local MongoDB:", err)
	}
	fmt.Println("✓ Connected to local MongoDB")

	if err := railwayClient.Ping(ctx, nil); err != nil {
		log.Fatal("Failed to ping Railway MongoDB:", err)
	}
	fmt.Println("✓ Connected to Railway MongoDB")

	// Database and collection names
	dbName := "investigacion_usach_db"
	collectionName := "articulos_wos"

	// Get source and destination collections
	sourceCollection := localClient.Database(dbName).Collection(collectionName)
	destCollection := railwayClient.Database(dbName).Collection(collectionName)

	// Count documents in source
	sourceCount, err := sourceCollection.CountDocuments(ctx, bson.M{})
	if err != nil {
		log.Fatal("Failed to count source documents:", err)
	}
	fmt.Printf("\n📊 Found %d documents in local MongoDB\n", sourceCount)

	// Count existing documents in destination
	destCount, err := destCollection.CountDocuments(ctx, bson.M{})
	if err != nil {
		log.Fatal("Failed to count destination documents:", err)
	}
	if destCount > 0 {
		fmt.Printf("⚠️  Warning: Destination already has %d documents\n", destCount)
		fmt.Println("Do you want to continue? The migration will add documents (not replace). Press Enter to continue or Ctrl+C to cancel...")
		fmt.Scanln()
	}

	// Find all documents from source
	cursor, err := sourceCollection.Find(ctx, bson.M{})
	if err != nil {
		log.Fatal("Failed to query source documents:", err)
	}
	defer cursor.Close(ctx)

	// Prepare documents for batch insert
	var documents []interface{}
	batchSize := 100
	totalMigrated := 0

	fmt.Println("\n🔄 Starting migration...")
	startTime := time.Now()

	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			log.Printf("Failed to decode document: %v", err)
			continue
		}

		// Remove _id to let Railway MongoDB generate new ones (optional)
		// delete(doc, "_id")

		documents = append(documents, doc)

		// Insert in batches
		if len(documents) >= batchSize {
			result, err := destCollection.InsertMany(ctx, documents)
			if err != nil {
				log.Printf("Failed to insert batch: %v", err)
			} else {
				totalMigrated += len(result.InsertedIDs)
				fmt.Printf("  Migrated %d/%d documents...\r", totalMigrated, sourceCount)
			}
			documents = documents[:0] // Clear slice
		}
	}

	// Insert remaining documents
	if len(documents) > 0 {
		result, err := destCollection.InsertMany(ctx, documents)
		if err != nil {
			log.Printf("Failed to insert final batch: %v", err)
		} else {
			totalMigrated += len(result.InsertedIDs)
		}
	}

	// Check for cursor errors
	if err := cursor.Err(); err != nil {
		log.Printf("Cursor error: %v", err)
	}

	// Final count in destination
	finalCount, _ := destCollection.CountDocuments(ctx, bson.M{})
	
	duration := time.Since(startTime)
	fmt.Printf("\n\n✅ Migration completed in %v\n", duration)
	fmt.Printf("📈 Summary:\n")
	fmt.Printf("  - Documents in source: %d\n", sourceCount)
	fmt.Printf("  - Documents migrated: %d\n", totalMigrated)
	fmt.Printf("  - Total documents in destination: %d\n", finalCount)

	// Also migrate Elasticsearch data if needed
	fmt.Println("\n💡 Note: This script only migrates MongoDB data.")
	fmt.Println("   You'll also need to reindex Elasticsearch data using the reindex script.")
}