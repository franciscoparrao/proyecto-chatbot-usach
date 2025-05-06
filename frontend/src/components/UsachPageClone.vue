<template>
  <div class="relative w-screen h-screen">
    <iframe 
      src="/usach.cl/index.html" 
      class="w-full h-full border-none absolute top-0 left-0 show-chatbot:hidden"
    ></iframe>
    <img 
      src="/Group 1.svg" 
      alt="logo chatbot" 
      class="absolute bottom-32 right-7 z-10 w-24 h-24 cursor-pointer"
      @click="showChat = true"
      v-if="!showChat"
    >
    <div 
      v-if="showChat"
      class="fixed inset-0 flex items-center justify-center z-20"
    >
      <div class="absolute inset-0 bg-black bg-opacity-40" @click="showChat = false"></div>
      <Chatbot class="relative z-30" @close="showChat = false" />
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import Chatbot from './ChatInterface.vue'

const showChat = ref(false)
</script>

  