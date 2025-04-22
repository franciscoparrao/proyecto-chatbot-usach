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
    ├── processor/    # Generación de embeddings de contenido
    └── scraper/      # Herramientas de web scraping
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

3. Configurar el backend (deben establecerse las variables de entorno)

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

## Licencia

[Añade tu licencia aquí]

## Contacto

[Tu información de contacto]