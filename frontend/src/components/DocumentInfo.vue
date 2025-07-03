<template>
  <div class="document-info mt-4 p-4 bg-gray-50 rounded-lg border border-gray-200">
    <h4 class="text-sm font-semibold text-gray-700 mb-3">📚 Documentos consultados:</h4>
    <div v-for="(doc, index) in documents" :key="index" class="mb-3 p-3 bg-white rounded-md border border-gray-100">
      <h5 class="text-sm font-medium text-gray-800 mb-1">{{ doc.title }}</h5>
      
      <div v-if="doc.authors" class="text-xs text-gray-600 mb-1">
        <span class="font-medium">Autores:</span> {{ doc.authors }}
      </div>
      
      <div v-if="doc.journal" class="text-xs text-gray-600 mb-1">
        <span class="font-medium">Publicado en:</span> {{ doc.journal }}
      </div>
      
      <div class="flex flex-wrap gap-2 mt-2">
        <!-- Enlace DOI -->
        <a v-if="doc.doi_link" 
           :href="doc.doi_link" 
           target="_blank" 
           rel="noopener noreferrer"
           class="inline-flex items-center gap-1 px-3 py-1 bg-blue-50 text-blue-700 rounded-full text-xs hover:bg-blue-100 transition-colors">
          <svg class="w-3 h-3" fill="currentColor" viewBox="0 0 20 20">
            <path d="M11 3a1 1 0 100 2h2.586l-6.293 6.293a1 1 0 101.414 1.414L15 6.414V9a1 1 0 102 0V4a1 1 0 00-1-1h-5z"/>
            <path d="M5 5a2 2 0 00-2 2v8a2 2 0 002 2h8a2 2 0 002-2v-3a1 1 0 10-2 0v3H5V7h3a1 1 0 000-2H5z"/>
          </svg>
          Ver artículo completo
        </a>
        
        <!-- Email de contacto -->
        <a v-if="doc.email" 
           :href="'mailto:' + doc.email" 
           class="inline-flex items-center gap-1 px-3 py-1 bg-green-50 text-green-700 rounded-full text-xs hover:bg-green-100 transition-colors">
          <svg class="w-3 h-3" fill="currentColor" viewBox="0 0 20 20">
            <path d="M2.003 5.884L10 9.882l7.997-3.998A2 2 0 0016 4H4a2 2 0 00-1.997 1.884z"/>
            <path d="M18 8.118l-8 4-8-4V14a2 2 0 002 2h12a2 2 0 002-2V8.118z"/>
          </svg>
          Contactar autor
        </a>
        
        <!-- Detalles de autores USACH -->
        <div v-if="doc.author_details && doc.author_details.some(a => a.is_usach)" class="w-full mt-2">
          <div class="text-xs text-gray-600 mb-1 font-medium">Investigadores USACH:</div>
          <div v-for="author in doc.author_details.filter(a => a.is_usach)" :key="author.name" class="ml-2 text-xs">
            <span class="text-gray-700">• {{ author.name }}</span>
            <a v-if="author.email" 
               :href="'mailto:' + author.email"
               class="ml-2 text-blue-600 hover:underline">
              {{ author.email }}
            </a>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { defineProps } from 'vue';

defineProps({
  documents: {
    type: Array,
    default: () => []
  }
});
</script>

<style scoped>
.document-info {
  animation: fadeIn 0.3s ease-in;
}

@keyframes fadeIn {
  from {
    opacity: 0;
    transform: translateY(5px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
</style>