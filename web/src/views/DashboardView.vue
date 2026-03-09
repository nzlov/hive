<template>
  <AdminShell>
    <section class="space-y-6">
      <div class="panel p-6 sm:p-8">
        <p class="text-xs uppercase tracking-[0.35em] text-slate-400">Control Center</p>
        <div class="mt-4 flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <h2 class="text-3xl font-semibold text-ink">欢迎回来，{{ profile.real_name || profile.username }}</h2>
            <p class="mt-2 text-sm text-slate-500">当前界面用于集中维护后台 JWT 用户与面向记忆接口的 API Token。</p>
          </div>
          <RouterLink class="primary-btn" to="/admin/users">前往用户管理</RouterLink>
        </div>
      </div>
      <div class="grid gap-6 md:grid-cols-3">
        <article class="panel p-6">
          <p class="text-sm text-slate-500">用户 UUID</p>
          <p class="mt-4 break-all text-lg font-semibold text-ink">{{ profile.userid || '-' }}</p>
        </article>
        <article class="panel p-6">
          <p class="text-sm text-slate-500">管理员身份</p>
          <p class="mt-4 text-lg font-semibold text-ink">{{ profile.is_admin ? '管理员' : '普通用户' }}</p>
        </article>
        <article class="panel p-6">
          <p class="text-sm text-slate-500">API Token</p>
          <p class="mt-4 break-all text-xs font-medium text-pine">{{ profile.apitoken || '-' }}</p>
        </article>
      </div>
    </section>
  </AdminShell>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import AdminShell from '../components/AdminShell.vue'
import { fetchCurrentUser } from '../lib/api'
import { getStoredUser, saveSession, getToken } from '../lib/auth'

const profile = ref(getStoredUser())

// loadProfile 在进入总览页时刷新一次当前用户，避免本地缓存与后端状态长期漂移。
async function loadProfile() {
  const response = await fetchCurrentUser()
  profile.value = response.item
  saveSession(getToken(), response.item)
}

onMounted(() => {
  loadProfile().catch(() => {})
})
</script>
