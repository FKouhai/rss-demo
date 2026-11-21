/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./views/**/*.{templ,go}", "./handlers/**/*.go"],
  theme: {
    extend: {
      fontFamily: {
        serif: ["Georgia", "Charter", "'Times New Roman'", "Times", "serif"],
        sans: ["system-ui", "-apple-system", "'Segoe UI'", "sans-serif"],
      },
    },
  },
  plugins: [],
};
