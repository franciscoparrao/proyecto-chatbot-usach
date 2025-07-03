# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Research Chatbot for USACH (Universidad de Santiago de Chile) that uses AI to answer questions about university research articles. The system implements hybrid search (semantic + lexical) to retrieve relevant documents and uses Google AI to generate responses.

## Key Commands

### Development

```bash
# Backend (from /backend)
go run main.go

# Frontend (from /frontend)
npm install
npm run serve  # Development server on localhost:8080
npm run build  # Production build
npm run lint   # Lint and fix files

# Docker deployment (from /deployment)
docker-compose up -d  # Start all services with containers
```

### Data Processing Pipeline

```bash
# Complete reset and reprocess (from /scripts)
./reset_and_reprocess.sh  # Cleans MongoDB, recreates ES index, reprocesses all data

# Individual processing steps
cd scripts
go run scraper/main.go     # Scrape articles from USACH and WoS
go run processor/main.go   # Process chunks and upload to databases
```

### Environment Variables

Required for backend:
- `GOOGLE_API_KEY` - Google AI API key for embeddings and chat
- `MONGO_URI` - MongoDB connection string (default: mongodb://localhost:27019)
- `ES_URL` - Elasticsearch URL (default: http://localhost:9201)

## Architecture

### Service Architecture
- **MongoDB** (port 27019): Stores full documents and metadata
- **Elasticsearch** (port 9201): Stores embeddings and text for hybrid search
- **Backend** (port 8000): Go/Gin API that orchestrates search and AI responses
- **Frontend** (port 3011 in Docker, 8080 in dev): Vue.js chat interface

### Data Flow
1. Scripts scrape research articles → JSON files in `/scripts/processor/files/`
2. Processor chunks content and generates 768-dim embeddings via Google AI
3. Documents stored in MongoDB collection `articulos_wos` in database `investigacion_usach_db`
4. Vectors and text indexed in Elasticsearch index `usach_chatbot_vectors_wos`
5. Chat queries trigger hybrid search in ES, results provide context to Google AI

### Key Implementation Details

**Hybrid Search Configuration:**
- Elasticsearch mapping defined in `/scripts/es_mapping.json`
- Spanish language analyzer for text search
- Cosine similarity for vector search
- Adjustable boost weights in `chat_handler.go` (text: 0.1, vector: 1.0)

**Frontend-Backend Communication:**
- CORS enabled for cross-origin requests
- Chat endpoint: POST `/api/chat`
- Request includes: `message`, `hybrid_mode` (boolean)
- Response includes: `response` (AI-generated text)

**Data Schema:**
- MongoDB documents include: title, authors, publication date, chunks, embeddings
- Elasticsearch documents include: MongoDocID, ChunkText, EmbeddingVector, metadata fields