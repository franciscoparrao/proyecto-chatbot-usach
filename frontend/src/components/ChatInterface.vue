<template>
    <div class="chat-container">
      <h2>Chat de Investigación USACH (Prototipo)</h2>
      <div class="search-config">
        <div class="toggle-container">
          <label class="switch">
            <input type="checkbox" v-model="hybridModeEnabled">
            <span class="slider round"></span>
          </label>
          <span class="toggle-label">Modo búsqueda híbrida: {{ hybridModeEnabled ? 'Activado' : 'Desactivado' }}</span>
        </div>
      </div>
      <div class="messages-area" ref="messagesAreaRef">
        <div v-for="message in messages" :key="message.id" class="message" :class="message.sender">
          <div class="message-bubble">
            <span v-if="message.sender === 'bot'" class="sender-label">Asistente:</span>
            <span v-if="message.sender === 'user'" class="sender-label">Tú:</span>
            <p class="message-text">{{ message.text }}</p>
            <!-- Opciones de temas (si existen) -->
            <div v-if="message.options && message.options.length > 0" class="topic-options">
              <div v-for="option in message.options" :key="option.label" 
                   class="topic-option" @click="selectOption(option)">
                {{ option.label }}
              </div>
            </div>
          </div>
        </div>
        <div v-if="isLoading" class="message bot">
           <div class="message-bubble loading-indicator">
              <span>Escribiendo...</span>
           </div>
        </div>
      </div>
      <div class="input-area">
        <input
          type="text"
          v-model="userInput"
          placeholder="Escribe tu pregunta aquí..."
          @keyup.enter="sendMessage"
          :disabled="isLoading"
        />
        <button @click="sendMessage" :disabled="isLoading">Enviar</button>
      </div>
    </div>
  </template>
  
  <script setup>
  import { ref, nextTick } from 'vue';
  import axios from 'axios';
  
  // --- Reactive State ---
  const messages = ref([]); // Array to hold chat messages { id, sender, text, options }
  const userInput = ref(''); // Input field model
  const isLoading = ref(false); // To show loading indicator and disable input/button
  const messagesAreaRef = ref(null); // To scroll down automatically
  const hybridModeEnabled = ref(true); // Activar búsqueda híbrida por defecto
  
  // --- Backend API URL ---
  console.log("backend:", process.env.VUE_APP_BACKEND_URL)
  // Usar una URL fija si la variable de entorno no está definida
  const backendUrl = process.env.VUE_APP_BACKEND_URL || 'http://localhost:8011/api/chat'; // URL from environment variables
  
// --- Functions ---
  const scrollToBottom = async () => {
    // Wait for DOM update after adding message
    await nextTick(); 
    const area = messagesAreaRef.value;
    if (area) {
      area.scrollTop = area.scrollHeight;
    }
  };
  
  const sendMessage = async () => {
    const text = userInput.value.trim();
    if (!text || isLoading.value) {
      return; // Don't send empty messages or while loading
    }
  
    // 1. Add user message to chat display
    messages.value.push({
      id: Date.now() + Math.random(), // Simple unique ID
      sender: 'user',
      text: text,
    });
    userInput.value = ''; // Clear input
    await scrollToBottom(); // Scroll down
  
    // 2. Set loading state
    isLoading.value = true;
    
    // Obtener historial de conversación (últimos 3 intercambios = 6 mensajes)
    const MAX_HISTORY_TURNS = 3;
    const historyToSend = messages.value
      .slice(-MAX_HISTORY_TURNS * 2) // Obtener los últimos N*2 mensajes
      .map(msg => ({ 
        role: msg.sender === 'bot' ? 'model' : 'user', 
        text: msg.text 
      }));
    
    console.log("Sending history:", historyToSend);
  
    // 3. Call backend API with history
    try {
      const response = await axios.post(backendUrl, { 
        query: text,
        history: historyToSend,
        hybrid_mode: hybridModeEnabled.value // Enviar el estado del modo híbrido
      });
      
      // Check if response and response.data exist
      if (response && response.data && response.data.response) {
          // 4. Add bot response to chat display
          const botMessage = {
              id: Date.now() + Math.random(),
              sender: 'bot',
              text: response.data.response, // Extract text from backend response
          };
          
          // Add options if available (for clarification_options type)
          if (response.data.response_type === 'clarification_options' && 
              response.data.options && response.data.options.length > 0) {
              botMessage.options = response.data.options;
              console.log('Received topic options:', response.data.options);
          }
          
          messages.value.push(botMessage);
      } else {
           // Handle unexpected response structure
           console.error('Unexpected response structure:', response);
           messages.value.push({
              id: Date.now() + Math.random(),
              sender: 'bot',
              text: 'Recibí una respuesta inesperada del servidor.',
          });
      }
  
    } catch (error) {
      console.error('Error sending message:', error);
      let errorMessage = 'Lo siento, ocurrió un error al contactar al servidor.';
      if (error.response && error.response.data && error.response.data.error) {
          // Try to get specific error from backend if available
          errorMessage = `Error del servidor: ${error.response.data.error}`;
      } else if (error.message) {
          errorMessage = `Error de red o conexión: ${error.message}`;
      }
       // 5. Add error message to chat display
       messages.value.push({
          id: Date.now() + Math.random(),
          sender: 'bot',
          text: errorMessage,
      });
    } finally {
      // 6. Reset loading state
      isLoading.value = false;
      await scrollToBottom(); // Scroll down again after bot response/error
    }
  };
  
  // Function to handle option selection
  const selectOption = (option) => {
    if (!option || !option.query_ref) return;
    
    // Add user selection as a message
    const selectionText = `${option.label}`;
    messages.value.push({
      id: Date.now() + Math.random(),
      sender: 'user',
      text: selectionText,
    });
    
    // Send the selected option as a query
    isLoading.value = true;
    
    // Get conversation history for context
    const MAX_HISTORY_TURNS = 3;
    const historyToSend = messages.value
      .slice(-MAX_HISTORY_TURNS * 2)
      .map(msg => ({ 
        role: msg.sender === 'bot' ? 'model' : 'user', 
        text: msg.text 
      }));
    
    // Send the option's query_ref to the backend WITH explicit option selection indicators
    axios.post(backendUrl, { 
      query: option.label,            // Send the label as the query (what user sees)
      selected_ref: option.query_ref, // Send the reference separately (might be different from label)
      is_option_reply: true,          // Explicit flag that this is a selection of an offered option
      intent: "select_option",        // For backward compatibility
      hybrid_mode: hybridModeEnabled.value, // Enviar el estado del modo híbrido
      history: historyToSend
    })
    .then(response => {
      if (response && response.data && response.data.response) {
        const botResponse = {
          id: Date.now() + Math.random(),
          sender: 'bot',
          text: response.data.response,
        };
        
        // Add any options if they exist
        if (response.data.response_type === 'clarification_options' && 
            response.data.options && response.data.options.length > 0) {
            botResponse.options = response.data.options;
        }
        
        messages.value.push(botResponse);
      }
    })
    .catch(error => {
      console.error('Error sending option selection:', error);
      const errorMessage = `Error al procesar tu selección: ${error.message}`;
      messages.value.push({
        id: Date.now() + Math.random(),
        sender: 'bot',
        text: errorMessage,
      });
    })
    .finally(() => {
      isLoading.value = false;
      scrollToBottom();
    });
  };
  
  // Optional: Add a welcome message on component mount
  messages.value.push({
      id: Date.now(),
      sender: 'bot',
      text: '¡Hola! Soy un asistente de investigación prototipo. Pregúntame sobre las noticias cargadas.'
  });
  
  </script>
  
  <style scoped>
  /* Estilos para el interruptor del modo híbrido */
  .search-config {
    display: flex;
    justify-content: center;
    margin: 5px 0;
    padding: 5px 0;
    background-color: #f4f4f4;
    border-bottom: 1px solid #ccc;
  }

  .toggle-container {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .toggle-label {
    font-size: 0.85em;
    color: #555;
  }

  /* The switch - the box around the slider */
  .switch {
    position: relative;
    display: inline-block;
    width: 50px;
    height: 24px;
  }

  /* Hide default HTML checkbox */
  .switch input {
    opacity: 0;
    width: 0;
    height: 0;
  }

  /* The slider */
  .slider {
    position: absolute;
    cursor: pointer;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background-color: #ccc;
    transition: .4s;
  }

  .slider:before {
    position: absolute;
    content: "";
    height: 16px;
    width: 16px;
    left: 4px;
    bottom: 4px;
    background-color: white;
    transition: .4s;
  }

  input:checked + .slider {
    background-color: #EF7D00;
  }

  input:focus + .slider {
    box-shadow: 0 0 1px #EF7D00;
  }

  input:checked + .slider:before {
    transform: translateX(26px);
  }

  /* Rounded sliders */
  .slider.round {
    border-radius: 24px;
  }

  .slider.round:before {
    border-radius: 50%;
  }
  
  /* Estilos para opciones de temas */
  .topic-options {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 12px;
  }
  
  .topic-option {
    background-color: #f8f9fa;
    border: 1px solid #ddd;
    border-radius: 16px;
    padding: 6px 12px;
    font-size: 0.9em;
    cursor: pointer;
    transition: all 0.2s ease;
    color: #333;
  }
  
  .topic-option:hover {
    background-color: #EF7D00;
    color: white;
    border-color: #EF7D00;
  }
  .chat-container {
    display: flex;
    flex-direction: column;
    height: 80vh; /* Adjust height as needed */
    max-width: 700px;
    margin: 20px auto;
    border: 1px solid #ccc;
    border-radius: 8px;
    overflow: hidden;
    box-shadow: 0 2px 10px rgba(0, 0, 0, 0.1);
    font-family: 'Nunito Sans', sans-serif; /* Using similar font */
  }
  
  h2 {
      text-align: center;
      padding: 10px;
      margin: 0;
      background-color: #f4f4f4;
      border-bottom: 1px solid #ccc;
      font-size: 1.2em;
  }
  
  .messages-area {
    flex-grow: 1;
    overflow-y: auto;
    padding: 15px;
    background-color: #fff;
    display: flex;
    flex-direction: column;
  }
  
  .message {
    margin-bottom: 15px;
    display: flex;
    max-width: 80%; /* Max width of message bubble */
  }
  
  .message-bubble {
    padding: 10px 15px;
    border-radius: 18px;
    word-wrap: break-word; /* Wrap long words */
  }
  
  .message.user {
    align-self: flex-end; /* User messages on the right */
  }
  .message.user .message-bubble {
    background-color: #EF7D00; /* Bootstrap primary blue */
    color: white;
    border-bottom-right-radius: 4px; /* Flat corner */
  }
  
  .message.bot {
    align-self: flex-start; /* Bot messages on the left */
  }
  .message.bot .message-bubble {
    background-color: #e9ecef; /* Light grey */
    color: #333;
    border-bottom-left-radius: 4px; /* Flat corner */
  }
  
  .sender-label {
      display: block;
      font-size: 0.8em;
      font-weight: bold;
      margin-bottom: 4px;
      opacity: 0.8;
  }
  
  .message-text {
      margin: 0;
      line-height: 1.4;
  }
  
  .loading-indicator span {
      font-style: italic;
      color: #555;
  }
  
  
  .input-area {
    display: flex;
    padding: 10px;
    border-top: 1px solid #ccc;
    background-color: #f8f9fa;
  }
  
  .input-area input {
    flex-grow: 1;
    padding: 10px;
    border: 1px solid #ccc;
    border-radius: 20px;
    margin-right: 10px;
    font-size: 1em;
  }
  .input-area input:disabled {
      background-color: #e9ecef;
  }
  
  .input-area button {
    padding: 10px 20px;
    background-color: #EF7D00;
    color: white;
    border: none;
    border-radius: 20px;
    cursor: pointer;
    font-size: 1em;
    transition: background-color 0.2s;
  }
  
  .input-area button:hover {
    background-color: #c86801;
  }
  .input-area button:disabled {
      background-color: #a0c7e4;
      cursor: not-allowed;
  }
  </style>
