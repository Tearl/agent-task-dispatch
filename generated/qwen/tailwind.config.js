/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        cyber: {
          dark: '#050714',
          panel: '#0a0f29',
          blue: '#00f0ff',
          pink: '#ff00ff',
          purple: '#8a2be2'
        }
      },
      boxShadow: {
        'neon-blue': '0 0 10px #00f0ff, 0 0 20px #00f0ff inset',
        'neon-pink': '0 0 10px #ff00ff, 0 0 20px #ff00ff inset',
        'neon-purple': '0 0 15px #8a2be2, 0 0 30px #8a2be2 inset'
      }
    },
  },
  plugins: [],
}