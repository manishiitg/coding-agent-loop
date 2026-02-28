/// <reference types="vite/client" />

// Global functions for keyboard shortcuts
declare global {
  interface Window {
    toggleAutoScroll?: () => void;
  }
}

export {};
