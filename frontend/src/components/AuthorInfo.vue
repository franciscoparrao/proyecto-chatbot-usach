<template>
  <div v-if="authors && authors.length > 0" class="authors-info-container">
    <div class="authors-header">
      <h4>
        <i class="fas fa-users"></i>
        Investigadores de este estudio
      </h4>
    </div>
    
    <div class="authors-grid">
      <div 
        v-for="(author, index) in authors" 
        :key="`${author.name}-${index}`"
        class="author-card"
        :class="{ 'usach-author': author.is_usach }"
      >
        <div class="author-header">
          <span class="author-name">{{ author.name }}</span>
          <span v-if="author.is_usach" class="usach-badge">
            <i class="fas fa-university"></i>
            USACH
          </span>
        </div>
        
        <div v-if="author.orcid_id" class="author-details">
          <a 
            :href="`https://orcid.org/${author.orcid_id}`"
            target="_blank"
            rel="noopener noreferrer"
            class="orcid-link"
          >
            <img 
              src="https://orcid.org/sites/default/files/images/orcid_16x16.png" 
              alt="ORCID"
              class="orcid-icon"
            >
            {{ author.orcid_id }}
          </a>
        </div>
        
        <div v-if="author.affiliations && author.affiliations.length > 0" class="affiliations">
          <small>
            <i class="fas fa-building"></i>
            {{ author.affiliations[0].organization_name }}
            <span v-if="author.affiliations[0].current" class="current-badge">Actual</span>
          </small>
        </div>
      </div>
    </div>
    
    <div v-if="showLoadMore" class="load-more-container">
      <button 
        @click="loadFullAuthorInfo" 
        class="load-more-button"
        :disabled="loading"
      >
        <i v-if="loading" class="fas fa-spinner fa-spin"></i>
        <span v-else>Cargar información completa de autores</span>
      </button>
    </div>
  </div>
</template>

<script>
export default {
  name: 'AuthorInfo',
  props: {
    authors: {
      type: Array,
      default: () => []
    },
    articleId: {
      type: String,
      default: null
    }
  },
  data() {
    return {
      loading: false,
      fullAuthorsInfo: null,
      showLoadMore: false
    }
  },
  computed: {
    hasUSACHAuthors() {
      return this.authors.some(author => author.is_usach)
    },
    displayAuthors() {
      return this.fullAuthorsInfo || this.authors
    }
  },
  mounted() {
    // Si tenemos article_id pero no información completa de autores, mostrar botón
    if (this.articleId && !this.authors.some(a => a.orcid_id)) {
      this.showLoadMore = true
    }
  },
  methods: {
    async loadFullAuthorInfo() {
      if (!this.articleId || this.loading) return
      
      this.loading = true
      try {
        const response = await fetch(`${process.env.VUE_APP_API_URL}/api/article/${this.articleId}/authors-info`)
        if (response.ok) {
          const data = await response.json()
          this.fullAuthorsInfo = data.authors
          this.showLoadMore = false
          this.$emit('authors-loaded', data.authors)
        }
      } catch (error) {
        console.error('Error loading author info:', error)
        this.$emit('error', 'No se pudo cargar la información de los autores')
      } finally {
        this.loading = false
      }
    }
  }
}
</script>

<style scoped>
.authors-info-container {
  margin: 20px 0;
  padding: 15px;
  background-color: #f8f9fa;
  border-radius: 8px;
  border: 1px solid #e9ecef;
}

.authors-header {
  margin-bottom: 15px;
}

.authors-header h4 {
  margin: 0;
  color: #333;
  font-size: 1.1rem;
}

.authors-header i {
  margin-right: 8px;
  color: #6c757d;
}

.authors-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 12px;
  margin-bottom: 15px;
}

.author-card {
  padding: 12px;
  background: white;
  border-radius: 6px;
  border: 1px solid #dee2e6;
  transition: all 0.2s ease;
}

.author-card:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  transform: translateY(-1px);
}

.author-card.usach-author {
  border-color: #003366;
  background: linear-gradient(to right, #f8f9fa 0%, #e6f0ff 100%);
}

.author-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.author-name {
  font-weight: 600;
  color: #212529;
  font-size: 0.95rem;
}

.usach-badge {
  background-color: #003366;
  color: white;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 0.75rem;
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 4px;
}

.usach-badge i {
  font-size: 0.7rem;
}

.author-details {
  margin: 8px 0;
}

.orcid-link {
  color: #a6ce39;
  text-decoration: none;
  font-size: 0.85rem;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  transition: color 0.2s;
}

.orcid-link:hover {
  color: #89a82c;
  text-decoration: underline;
}

.orcid-icon {
  width: 16px;
  height: 16px;
}

.affiliations {
  margin-top: 8px;
  color: #6c757d;
  font-size: 0.8rem;
}

.affiliations i {
  margin-right: 4px;
}

.current-badge {
  background-color: #28a745;
  color: white;
  padding: 1px 4px;
  border-radius: 3px;
  font-size: 0.7rem;
  margin-left: 4px;
}

.load-more-container {
  text-align: center;
  margin-top: 15px;
}

.load-more-button {
  background-color: #007bff;
  color: white;
  border: none;
  padding: 8px 20px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 0.9rem;
  transition: background-color 0.2s;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.load-more-button:hover:not(:disabled) {
  background-color: #0056b3;
}

.load-more-button:disabled {
  background-color: #6c757d;
  cursor: not-allowed;
}

.fa-spinner {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* Modo oscuro */
@media (prefers-color-scheme: dark) {
  .authors-info-container {
    background-color: #2d3748;
    border-color: #4a5568;
  }
  
  .author-card {
    background-color: #1a202c;
    border-color: #4a5568;
  }
  
  .author-card.usach-author {
    background: linear-gradient(to right, #1a202c 0%, #2d3e5f 100%);
  }
  
  .author-name {
    color: #e2e8f0;
  }
  
  .authors-header h4 {
    color: #e2e8f0;
  }
}
</style>