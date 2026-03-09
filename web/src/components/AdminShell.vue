<template>
  <div class="min-h-screen px-4 py-6 sm:px-6 lg:px-8">
    <div class="mx-auto flex w-full max-w-[92vw] flex-col gap-6 lg:flex-row">
      <aside class="panel overflow-hidden lg:w-80">
        <div class="bg-ink px-6 py-8 text-white">
          <p class="text-xs uppercase tracking-[0.4em] text-white/60">Hive Admin</p>
          <h1 class="mt-3 text-3xl font-semibold">记忆治理台</h1>
          <p class="mt-3 text-sm text-white/70">统一管理登录用户、API Token 与记忆服务访问权限。</p>
        </div>
        <div class="space-y-5 px-6 py-6">
          <div>
            <p class="text-xs uppercase tracking-[0.3em] text-slate-400">当前用户</p>
            <p class="mt-2 text-lg font-semibold text-ink">{{ user.real_name || user.username }}</p>
            <p class="text-sm text-slate-500">{{ user.username }}</p>
            <p class="mt-1 break-all text-xs text-slate-400">{{ user.userid }}</p>
          </div>
          <nav class="space-y-2">
            <RouterLink class="block rounded-2xl px-4 py-3 text-sm font-medium transition hover:bg-mist" to="/admin">总览</RouterLink>
            <RouterLink class="block rounded-2xl px-4 py-3 text-sm font-medium transition hover:bg-mist" to="/admin/memories">记忆管理</RouterLink>
            <RouterLink class="block rounded-2xl px-4 py-3 text-sm font-medium transition hover:bg-mist" to="/admin/users">用户管理</RouterLink>
          </nav>
          <button class="ghost-btn w-full" type="button" @click="handleLogout">退出登录</button>
        </div>
      </aside>
      <main class="min-w-0 flex-1">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup>
import { RouterLink, useRouter } from 'vue-router'
import { clearSession, getStoredUser } from '../lib/auth'

const router = useRouter()
const user = getStoredUser()

// handleLogout 统一清理本地会话并跳回登录页，避免不同页面退出动作出现状态残留。
function handleLogout() {
  clearSession()
  router.push('/login')
}
</script>
