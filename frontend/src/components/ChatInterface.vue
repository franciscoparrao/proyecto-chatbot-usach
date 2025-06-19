<template>
  <div class="flex flex-col h-[80vh] w-full max-w-[700px] mx-auto my-5 border border-gray-200 rounded-2xl overflow-hidden shadow-2xl font-sans bg-white">
    <!-- Título -->
    <h2 class="text-center py-3 m-0 bg-gray-50 border-b border-gray-200 text-xl font-bold tracking-tight text-gray-800">
      Chat de Investigación USACH (Prototipo)
    </h2>

    <!-- Configuración de búsqueda híbrida -->
    <div class="flex justify-center items-center gap-2 py-2 bg-gray-50 border-b border-gray-200">
      <label class="relative inline-block w-12 h-6">
        <input type="checkbox" v-model="hybridModeEnabled" class="peer opacity-0 w-0 h-0" />
        <span
          class="absolute cursor-pointer top-0 left-0 right-0 bottom-0 bg-gray-300 rounded-full transition peer-checked:bg-orange-500
            before:content-[''] before:absolute before:h-4 before:w-4 before:left-1 before:bottom-1 before:bg-white before:rounded-full before:transition
            peer-checked:before:translate-x-6"
        ></span>
      </label>
      <span class="text-sm text-gray-600">Modo búsqueda híbrida: <span :class="hybridModeEnabled ? 'text-orange-500 font-semibold' : 'text-gray-400'">{{ hybridModeEnabled ? 'Activado' : 'Desactivado' }}</span></span>
    </div>

    <!-- Área de mensajes -->
    <div ref="messagesAreaRef" class="flex-1 overflow-y-auto p-4 bg-white flex flex-col text-sm">
      <TransitionGroup name="msg" tag="div" class="flex flex-col gap-2">
        <div
          v-for="message in messages"
          :key="message.id"
          class="flex items-end"
          :class="message.sender === 'user' ? 'justify-end' : 'justify-start'"
        >
          <!-- Avatar solo para el bot -->
          <template v-if="message.sender === 'bot'">
            <img
              src="../assets/bot-avatar.png"
              alt="Agente"
              class="w-10 h-10 rounded-full mr-2 self-end shadow border border-gray-200 bg-gray-100 object-cover"
            />
          </template>
          <div
            class="max-w-[75%] px-4 py-2 rounded-2xl shadow-sm break-words"
            :class="message.sender === 'user'
              ? 'bg-gradient-to-br from-orange-400 to-orange-500 text-white rounded-br-md ml-auto'
              : 'bg-gray-100 text-gray-800 rounded-bl-md'"
          >
            <span class="block text-xs font-bold mb-1 opacity-70 select-none">
              {{ message.sender === 'bot' ? 'Asistente:' : 'Tú:' }}
            </span>
            <!-- Máquina de escribir solo para el mensaje actual del bot -->
            <template v-if="message.sender === 'bot' && message.id === botMessageId">
              <p class="m-0 leading-relaxed whitespace-pre-line">
                <span v-html="botVisibleText"></span>
                <span v-if="botTyping" class="invisible">
                  {{ botFullText.slice(botVisibleText.length) }}
                </span>
              </p>
              <!-- Opciones si existen -->
              <div v-if="message.options && message.options.length > 0" class="flex flex-wrap gap-2 mt-3">
                <div
                  v-for="option in message.options"
                  :key="option.label"
                  @click="selectOption(option)"
                  class="bg-gray-50 border border-gray-300 rounded-full px-3 py-1 text-sm cursor-pointer transition
                    text-gray-700 hover:bg-orange-500 hover:text-white hover:border-orange-500"
                >
                  {{ option.label }}
                </div>
              </div>
            </template>
            <!-- Mensaje normal (usuario o bot ya terminado) -->
            <template v-else>
              <p class="m-0 leading-relaxed whitespace-pre-line">{{ message.text }}</p>
              <div v-if="message.options && message.options.length > 0" class="flex flex-wrap gap-2 mt-3">
                <div
                  v-for="option in message.options"
                  :key="option.label"
                  @click="selectOption(option)"
                  class="bg-gray-50 border border-gray-300 rounded-full px-3 py-1 text-sm cursor-pointer transition
                    text-gray-700 hover:bg-orange-500 hover:text-white hover:border-orange-500"
                >
                  {{ option.label }}
                </div>
              </div>
            </template>
          </div>
        </div>
      </TransitionGroup>
      <!-- Indicador de carga animado -->
      <div v-if="isLoading && !botTyping" class="flex items-end justify-start mb-4">
        <img
          src="../assets/bot-avatar.png"
          alt="Agente"
          class="w-10 h-10 rounded-full mr-2 self-end shadow border border-gray-200 bg-gray-100 object-cover"
        />
        <div class="max-w-[75%] px-4 py-2 rounded-2xl bg-gray-100 text-gray-800 rounded-bl-md italic flex items-center shadow-sm">
          <span class="jumping-dots">
            <span class="dot dot-1"></span>
            <span class="dot dot-2"></span>
            <span class="dot dot-3"></span>
          </span>
        </div>
      </div>
    </div>

    <!-- Área de input -->
    <div class="flex p-3 border-t border-gray-200 bg-gray-50">
      <input
        type="text"
        v-model="userInput"
        placeholder="Escribe tu pregunta aquí…"
        @keyup.enter="sendMessage"
        :disabled="isLoading"
        class="flex-1 px-4 py-2 border border-gray-200 rounded-full mr-3 text-base focus:outline-none focus:ring-2 focus:ring-orange-400 disabled:bg-gray-100 shadow"
      />
      <button
        @click="sendMessage"
        :disabled="isLoading"
        class="px-6 py-2 bg-gradient-to-br from-orange-400 to-orange-500 text-white rounded-full text-base font-bold shadow transition hover:scale-105 hover:from-orange-500 hover:to-orange-600 disabled:bg-gray-300 disabled:cursor-not-allowed"
      >
        Enviar
      </button>
    </div>
  </div>
</template>



<script setup>
import { ref, nextTick } from 'vue';
import axios from 'axios';

// --- Reactive State ---
const messages = ref([]);
const userInput = ref('');
const isLoading = ref(false);
const messagesAreaRef = ref(null);
const hybridModeEnabled = ref(true);
const backendUrl = process.env.VUE_APP_BACKEND_URL || 'http://localhost:8000/api/chat'; // URL from environment variables

// --- Máquina de escribir ---
const botTyping = ref(false);
const botFullText = ref('');
const botVisibleText = ref('');
const botMessageId = ref(null);

async function showBotMessageProgressively(text, options = null) {
  botFullText.value = text;
  botVisibleText.value = '';
  botTyping.value = true;

  // Agrega el mensaje "vacío" al chat
  const id = Date.now() + Math.random();
  botMessageId.value = id;
  messages.value.push({
    id,
    sender: 'bot',
    text: '',
    ...(options ? { options } : {})
  });

  await nextTick();

  for (let i = 0; i <= text.length; i++) {
    botVisibleText.value = text.slice(0, i);
    const idx = messages.value.findIndex(m => m.id === id);
    if (idx !== -1) messages.value[idx].text = botVisibleText.value;
    await new Promise(res => setTimeout(res, 4)); // velocidad
  }

  botTyping.value = false;
  botMessageId.value = null;
  await scrollToBottom();
}

// --- Functions ---
const scrollToBottom = async () => {
  await nextTick();
  const area = messagesAreaRef.value;
  if (area) {
    area.scrollTop = area.scrollHeight;
  }
};

const sendMessage = async () => {
  const text = userInput.value.trim();
  if (!text || isLoading.value || botTyping.value) return;

  messages.value.push({
    id: Date.now() + Math.random(),
    sender: 'user',
    text: text,
  });
  userInput.value = '';
  await scrollToBottom();

  isLoading.value = true;
  
  try {
    const response = await axios.post(backendUrl, { 
      query: text,
      history: [],
      hybrid_mode: hybridModeEnabled.value
    }, {
      withCredentials: true
    });

    if (response?.data?.response) {
      // Si hay opciones, pásalas también
      const options = (response.data.response_type === 'clarification_options' && response.data.options?.length > 0)
        ? response.data.options
        : null;
      await showBotMessageProgressively(response.data.response, options);
    }
  } catch (error) {
    console.error('Error:', error);
    await showBotMessageProgressively(error.response?.data?.error || 'Error de conexión');
  } finally {
    isLoading.value = false;
    await scrollToBottom();
  }
};

const selectOption = async (option) => {
  if (!option?.query_ref || isLoading.value || botTyping.value) return;

  messages.value.push({
    id: Date.now() + Math.random(),
    sender: 'user',
    text: option.label,
  });
  
  isLoading.value = true;

  try {
    const response = await axios.post(backendUrl, { 
      query: option.label,
      selected_ref: option.query_ref,
      is_option_reply: true,
      intent: "select_option",
      hybrid_mode: hybridModeEnabled.value,
      history: []
    }, {
      withCredentials: true
    });

    if (response?.data?.response) {
      const options = (response.data.response_type === 'clarification_options' && response.data.options?.length > 0)
        ? response.data.options
        : null;
      await showBotMessageProgressively(response.data.response, options);
    }
  } catch (error) {
    console.error('Error:', error);
    await showBotMessageProgressively(error.response?.data?.error || 'Error al procesar');
  } finally {
    isLoading.value = false;
    await scrollToBottom();
  }
};

// Mensaje de bienvenida
messages.value.push({
  id: Date.now(),
  sender: 'bot',
  text: '¡Hola! Soy Usachin!, Tu asistente de investigación. Pregúntame sobre las noticias cargadas.'
});
</script>

<style scoped>
.jumping-dots {
  display: inline-block;
  height: 16px;
}

.jumping-dots .dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  margin-right: 3px;
  background-color: #6c6c6c; /* O el color que prefieras */
  border-radius: 50%;
  position: relative;
  animation: jump 1s infinite;
}

.jumping-dots .dot-1 { animation-delay: 0.1s; }
.jumping-dots .dot-2 { animation-delay: 0.3s; }
.jumping-dots .dot-3 { animation-delay: 0.5s; }

@keyframes jump {
  0%   { bottom: 0px; opacity: 0.6; }
  20%  { bottom: 6px; opacity: 1; }
  40%  { bottom: 0px; opacity: 0.6; }
  100% { bottom: 0px; opacity: 0.6; }
}

/* Animación para mensajes nuevos */
.msg-enter-from {
  opacity: 0;
  transform: translateY(20px);
}
.msg-enter-active {
  transition: opacity 0.3s ease-out, transform 0.3s cubic-bezier(0.42,0,1,1); /* ease-out para entrada */
}
.msg-enter-to {
  opacity: 1;
  transform: translateY(0);
}

.msg-leave-from {
  opacity: 1;
  transform: translateY(0);
}
.msg-leave-active {
  transition: opacity 0.2s ease-in, transform 0.2s cubic-bezier(0.42,0,1,1); /* ease-in para salida */
}
.msg-leave-to {
  opacity: 0;
  transform: translateY(-20px);
}
</style>