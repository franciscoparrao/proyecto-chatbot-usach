<template>
    <div class="chat-container">
      <h2>Chat de Investigación USACH (Prototipo)</h2>
      <div class="messages-area" ref="messagesAreaRef">
        <div v-for="message in messages" :key="message.id" class="message" :class="message.sender">
          <div class="message-bubble">
            <span v-if="message.sender === 'bot'" class="sender-label">Asistente:</span>
            <span v-if="message.sender === 'user'" class="sender-label">Tú:</span>
            <p class="message-text">{{ message.text }}</p>
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
  const messages = ref([]); // Array to hold chat messages { id, sender, text }
  const userInput = ref(''); // Input field model
  const isLoading = ref(false); // To show loading indicator and disable input/button
  const messagesAreaRef = ref(null); // To scroll down automatically
  
  // --- Backend API URL ---
  console.log("backend:", process.env.BACKEND_URL)
  const backendUrl = process.env.VUE_APP_BACKEND_URL; // URL from environment variables  
  
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
  
    // 3. Call backend API
    try {
      const response = await axios.post(backendUrl, { query: text });
      
      // Check if response and response.data exist
      if (response && response.data && response.data.response) {
          // 4. Add bot response to chat display
          messages.value.push({
              id: Date.now() + Math.random(),
              sender: 'bot',
              text: response.data.response, // Extract text from backend response
          });
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
  
  // Optional: Add a welcome message on component mount
  messages.value.push({
      id: Date.now(),
      sender: 'bot',
      text: '¡Hola! Soy un asistente de investigación prototipo. Pregúntame sobre las noticias cargadas.'
  });
  
  </script>
  
  <style scoped>
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
