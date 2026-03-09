/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js}'],
  theme: {
    extend: {
      colors: {
        ink: '#132238',
        mist: '#eef4f8',
        brass: '#c37b2c',
        pine: '#1d5f54',
        coral: '#d46b55',
      },
      fontFamily: {
        sans: ['Noto Sans SC', 'Source Han Sans SC', 'PingFang SC', 'Microsoft YaHei', 'sans-serif'],
      },
      boxShadow: {
        panel: '0 20px 60px rgba(19, 34, 56, 0.12)',
      },
    },
  },
  plugins: [],
}
