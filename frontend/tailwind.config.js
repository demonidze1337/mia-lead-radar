/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        ink: '#070d14',
        panel: '#0b1520',
        edge: '#1b2c3d',
        mint: '#34d399',
        cyan: '#22d3ee',
      },
      boxShadow: {
        glow: '0 0 40px rgba(34, 211, 238, 0.12)',
      },
      fontFamily: {
        sans: ['Inter', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'ui-monospace', 'SFMono-Regular', 'monospace'],
      },
    },
  },
  plugins: [],
};
