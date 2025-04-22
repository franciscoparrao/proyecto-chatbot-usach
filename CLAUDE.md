# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Run Commands
- Backend: `go run ./backend/main.go` (requires env vars set)
- Frontend: `cd frontend && npm run serve`
- Frontend Lint: `cd frontend && npm run lint`
- Frontend Build: `cd frontend && npm run build`

## Code Style Guidelines
### Go (Backend)
- Imports: Organize imports in standard groups (stdlib, external, internal)
- Error handling: Check errors immediately
- Naming: Use camelCase for variables, PascalCase for exported functions
- Structure: Use handlers/, models/, services/ directories

### Vue.js (Frontend)
- Component organization: Use script setup pattern and composition API
- Styling: Use scoped CSS
- API calls: Use axios for HTTP requests
- Error handling: Catch errors and display user-friendly messages
- Component naming: Use PascalCase for components

### General
- Keep related functionality logically grouped
- Ensure clean error messages reach the user interface
- Document environment variables