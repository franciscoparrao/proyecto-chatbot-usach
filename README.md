# Chatbot de Investigación USACH

Un sistema de IA conversacional que permite a los usuarios consultar artículos e investigaciones de la Universidad de Santiago de Chile.

## Descripción General

Este proyecto consta de:
- Un backend en Go que se integra con Google AI y MongoDB
- Un frontend en Vue.js que proporciona una interfaz de chat
- Scripts de procesamiento de datos para extraer y generar embeddings del contenido de investigación

## Características

- Interfaz de chat interactiva
- Consultas en lenguaje natural sobre investigaciones
- Datos extraídos de fuentes de la USACH
- Embeddings vectoriales para búsqueda semántica

## Estructura del Proyecto

```
├── backend/          # Servidor API en Go
├── frontend/         # Interfaz de usuario en Vue.js
└── scripts/          # Utilidades de procesamiento de datos
    ├── scrapper/            # Herramientas de web scraping
    ├── chunker/             # Generación de chunks del contenido scrappeado
    ├── chunk-embeddings/    # Generación de embeddings de los chunks
    └── processor/           # Subida de los vectores a Mongo Atlas
```

## Requisitos Previos

- Go 1.23+
- Node.js y npm
- Cuenta de MongoDB
- Clave API de Google AI

## Variables de Entorno

```
GOOGLE_API_KEY=tu_clave_de_api_google_ai
MONGO_URI=tu_cadena_de_conexion_mongodb
```

Si prefieres usar .env para trabajar tus variables de entorno puedes usar "github.com/joho/godotenv" que permite este trabajo.
Si no, en consola escribir export GOOGLE_API_KEY=tu_clave_de_api_google_ai, export=MONGO_URI=tu_cadena_de_conexion_mongodb sirve.

## Instalación

1. Clonar el repositorio
   ```
   git clone https://github.com/tuusuario/proyecto-chatbot-usach.git
   cd proyecto-chatbot-usach
   ```

2. Configurar el frontend
   ```
   cd frontend
   npm install
   ```

3. Configurar el backend (deben establecerse/exportar las variables de entorno y más si es necesario)
   3.1 Si necesitas trabajar scrappear/chunkear/hacer embeddings/subir vectores, debes primero trabajar con los scripts realizados.
   3.2 Debes crear una base de datos en Mongo Atlas y su colección.
   3.3 Para asegurar la rápida búsqueda semántica, debes crear un vector de búsqueda (index search) en Mongo Atlas. 

## Ejecutar la Aplicación

1. Iniciar el servidor backend
   ```
   go run ./backend/main.go
   ```

2. Iniciar el servidor de desarrollo frontend
   ```
   cd frontend
   npm run serve
   ```

3. Visitar `http://localhost:8080` en tu navegador


# Cómo correr la aplicación

1.- Levantar los contenedores usando docker compose up -d en la carpeta deployment.

# Cómo subir archivos a Elasticsearch

// ESTO SE DEBE CAMBIAR SI O SI PARA PODER AJUSTAR EL NOMBRE DE LOS CAMPOS, OSEA AÑADIR TITULO, AUTORES, FECHA DE PUBLICACION,
// EL CHUNK Y EL EMBEDDING VECTOR

1.- correr el siguiente comando: curl -X PUT "http://localhost:9200/usach_chatbot_vectors" -H 'Content-Type: application/json' -d'
{
  "mappings": {
    "properties": {
      "EmbeddingVector": {
        "type": "dense_vector",
        "dims": 768,             
        "index": true,           
        "similarity": "cosine"   
      },
      "MongoDocID": {         
        "type": "keyword"        
      },
      "OriginalTitle": {        
         "type": "text"         
      },
      "OriginalAuthors": {        
         "type": "text"         
      },
      "OriginalPublicationDate": {        
         "type": "text"         
      }
    }
  }
}
'

2.- Cabe destacar que para conectarse a mongo en una terminal que no conoce al contenedor, se debe usar el primer mapeo de puertos del docker-compose.yml, en este caso 27018.