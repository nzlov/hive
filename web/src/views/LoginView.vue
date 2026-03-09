<template>
  <div class="flex min-h-screen items-center justify-center px-4 py-10 sm:px-6">
    <div class="grid w-full max-w-6xl gap-6 lg:grid-cols-[1.1fr_0.9fr]">
      <section class="panel hidden overflow-hidden lg:block">
        <div class="h-full bg-[linear-gradient(135deg,#132238_0%,#1d5f54_48%,#c37b2c_100%)] px-10 py-12 text-white">
          <p class="text-xs uppercase tracking-[0.45em] text-white/60">Memory Manager</p>
          <h1 class="mt-6 max-w-xl text-5xl font-semibold leading-tight">让记忆权限、来源与管理界面回到同一条控制面。</h1>
          <div class="mt-10 grid gap-4 md:grid-cols-2">
            <div class="rounded-3xl border border-white/20 bg-white/10 p-5">
              <p class="text-sm font-semibold">JWT 管理端</p>
              <p class="mt-2 text-sm text-white/70">用户管理接口统一走 `/api/v1/users`，便于后台管理与页面守卫。</p>
            </div>
            <div class="rounded-3xl border border-white/20 bg-white/10 p-5">
              <p class="text-sm font-semibold">API Token 记忆接口</p>
              <p class="mt-2 text-sm text-white/70">写入与查询统一走 `/tokenapi/v1/memories/*`，并记录创建用户 UUID。</p>
            </div>
          </div>
        </div>
      </section>
      <section class="panel p-6 sm:p-8 lg:p-10">
        <p class="text-xs uppercase tracking-[0.4em] text-slate-400">Admin Login</p>
        <h2 class="mt-4 text-3xl font-semibold text-ink">登录管理后台</h2>
        <p class="mt-3 text-sm text-slate-500">请输入你的账号与密码登录管理后台。</p>
        <form class="mt-8 space-y-4" @submit.prevent="handleSubmit">
          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">用户名</span>
            <input v-model="form.username" class="field" autocomplete="username" placeholder="请输入用户名" />
          </label>
          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">密码</span>
            <input v-model="form.password" class="field" type="password" autocomplete="current-password" placeholder="请输入密码" />
          </label>
          <p v-if="errorMessage" class="rounded-2xl bg-coral/10 px-4 py-3 text-sm text-coral">{{ errorMessage }}</p>
          <button class="primary-btn w-full" type="submit" :disabled="loading">
            {{ loading ? '登录中...' : '进入后台' }}
          </button>
        </form>
      </section>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { login } from '../lib/api'

const router = useRouter()
const loading = ref(false)
const errorMessage = ref('')
const form = reactive({
  username: '',
  password: '',
})

// handleSubmit 统一处理登录状态与错误提示，避免表单重复提交造成界面抖动。
async function handleSubmit() {
  const username = form.username.trim()
  const password = form.password.trim()
  if (!username) {
    errorMessage.value = '请输入用户名'
    return
  }
  if (!password) {
    errorMessage.value = '请输入密码'
    return
  }
  loading.value = true
  errorMessage.value = ''
  try {
    await login(username, password)
    router.push('/admin')
  } catch (error) {
    errorMessage.value = error.message
  } finally {
    loading.value = false
  }
}
</script>
