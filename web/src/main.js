import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import './styles.css'

// createApp 统一挂载根实例，确保路由守卫和样式只初始化一次。
createApp(App).use(router).mount('#app')
