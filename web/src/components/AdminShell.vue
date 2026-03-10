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
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0 flex-1">
              <p class="text-xs uppercase tracking-[0.3em] text-slate-400">当前用户</p>
              <p class="mt-2 text-lg font-semibold text-ink truncate">{{ user.real_name || user.username }}</p>
              <p class="text-sm text-slate-500 truncate">{{ user.username }}</p>
              <p class="mt-1 break-all text-xs text-slate-400">{{ user.userid }}</p>
            </div>
            <button class="ghost-btn shrink-0 px-3 py-2 text-xs" type="button" @click="showPasswordModal = true">编辑密码</button>
          </div>
          <nav class="space-y-2">
            <RouterLink class="block rounded-2xl px-4 py-3 text-sm font-medium transition hover:bg-mist" to="/admin">总览</RouterLink>
            <RouterLink class="block rounded-2xl px-4 py-3 text-sm font-medium transition hover:bg-mist" to="/admin/memories">记忆管理</RouterLink>
            <RouterLink v-if="user.is_admin" class="block rounded-2xl px-4 py-3 text-sm font-medium transition hover:bg-mist" to="/admin/memory-cleanup">清理治理</RouterLink>
            <RouterLink v-if="user.is_admin" class="block rounded-2xl px-4 py-3 text-sm font-medium transition hover:bg-mist" to="/admin/users">用户管理</RouterLink>
          </nav>
          <button class="ghost-btn w-full" type="button" @click="handleLogout">退出登录</button>
        </div>
      </aside>
      <main class="min-w-0 flex-1">
        <slot />
      </main>
    </div>

    <div v-if="showPasswordModal" class="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/50" @click.self="closePasswordModal">
      <div class="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl">
        <h3 class="text-lg font-semibold text-ink">修改密码</h3>
        <form class="mt-4 space-y-4" @submit.prevent="handleChangePassword">
          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">新密码</span>
            <input v-model="newPassword" class="field w-full" placeholder="请输入新密码" type="password" />
          </label>
          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">确认密码</span>
            <input v-model="confirmPassword" class="field w-full" placeholder="请再次输入新密码" type="password" />
          </label>
          <p v-if="passwordError" class="rounded-xl bg-coral/10 px-3 py-2 text-sm text-coral">{{ passwordError }}</p>
          <div class="flex gap-3 pt-2">
            <button class="ghost-btn flex-1" type="button" @click="closePasswordModal">取消</button>
            <button class="primary-btn flex-1" type="submit" :disabled="passwordLoading">{{ passwordLoading ? '保存中...' : '保存' }}</button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup>
import { RouterLink, useRouter } from 'vue-router'
import { clearSession, getStoredUser } from '../lib/auth'
import { ref } from 'vue'

const router = useRouter()
const user = getStoredUser()
const showPasswordModal = ref(false)
const newPassword = ref('')
const confirmPassword = ref('')
const passwordError = ref('')
const passwordLoading = ref(false)

// handleLogout 统一清理本地会话并跳回登录页，避免不同页面退出动作出现状态残留。
function handleLogout() {
  clearSession()
  router.push('/login')
}

// closePasswordModal 关闭修改密码弹窗并清理表单状态，避免下次打开时残留旧数据。
function closePasswordModal() {
  showPasswordModal.value = false
  newPassword.value = ''
  confirmPassword.value = ''
  passwordError.value = ''
}

// handleChangePassword 处理密码修改逻辑，校验两次输入一致后调用后端接口。
async function handleChangePassword() {
  passwordError.value = ''
  if (!newPassword.value.trim()) {
    passwordError.value = '请输入新密码'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    passwordError.value = '两次输入的密码不一致'
    return
  }
  passwordLoading.value = true
  try {
    const token = localStorage.getItem('hive_admin_token')
    const response = await fetch('/api/v1/users/me/password', {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`,
      },
      body: JSON.stringify({ new_password: newPassword.value }),
    })
    const data = await response.json()
    if (data.error) {
      passwordError.value = data.error
    } else {
      closePasswordModal()
      alert('密码修改成功')
    }
  } catch (err) {
    passwordError.value = err.message || '修改失败，请重试'
  } finally {
    passwordLoading.value = false
  }
}
</script>
