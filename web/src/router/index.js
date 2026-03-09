import { createRouter, createWebHistory } from 'vue-router'
import DashboardView from '../views/DashboardView.vue'
import LoginView from '../views/LoginView.vue'
import MemoriesView from '../views/MemoriesView.vue'
import UsersView from '../views/UsersView.vue'
import { getToken, getStoredUser } from '../lib/auth'

const routes = [
  { path: '/login', name: 'login', component: LoginView },
  { path: '/', redirect: '/admin' },
  { path: '/admin', name: 'dashboard', component: DashboardView, meta: { requiresAuth: true } },
  { path: '/admin/memories', name: 'memories', component: MemoriesView, meta: { requiresAuth: true } },
  { path: '/admin/users', name: 'users', component: UsersView, meta: { requiresAuth: true, requiresAdmin: true } },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// beforeEach 统一管理未登录跳转，避免每个页面单独实现重复的鉴权重定向逻辑。
router.beforeEach((to) => {
  const hasToken = Boolean(getToken())
  if (to.meta.requiresAuth && !hasToken) {
    return '/login'
  }
  if (to.path === '/login' && hasToken) {
    return '/admin'
  }
  if (to.meta.requiresAdmin) {
    const user = getStoredUser()
    if (!user.is_admin) {
      return '/admin'
    }
  }
  return true
})

export default router
