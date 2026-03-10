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

      <div class="grid gap-6 xl:grid-cols-[1.25fr_0.75fr]">
        <section class="space-y-6">
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

          <div class="grid gap-6 lg:grid-cols-2">
            <article class="panel p-6">
              <p class="text-xs uppercase tracking-[0.35em] text-slate-400">Overview Stats</p>
              <div class="mt-5 grid grid-cols-2 gap-4">
                <div class="rounded-2xl border border-slate-200 bg-mist/60 p-4">
                  <p class="text-xs text-slate-500">用户总数</p>
                  <p class="mt-2 text-2xl font-semibold text-ink">{{ stats.base.user_total }}</p>
                </div>
                <div class="rounded-2xl border border-slate-200 bg-mist/60 p-4">
                  <p class="text-xs text-slate-500">记忆总数</p>
                  <p class="mt-2 text-2xl font-semibold text-ink">{{ stats.base.memory_total }}</p>
                </div>
              </div>
            </article>

            <article class="panel p-6">
              <p class="text-xs uppercase tracking-[0.35em] text-slate-400">Memory Type</p>
              <div class="mt-5 flex items-center gap-5">
                <div class="relative h-28 w-28 shrink-0 rounded-full" :style="memoryTypeChartStyle">
                  <div class="absolute inset-4 flex items-center justify-center rounded-full bg-white text-sm font-semibold text-ink">
                    {{ stats.base.memory_total }}
                  </div>
                </div>
                <div class="space-y-3 text-sm text-slate-600">
                  <p class="flex items-center gap-2"><span class="h-2.5 w-2.5 rounded-full bg-pine"></span>总结 {{ stats.base.memory_type.summary }} ({{ formatPercent(summaryRate) }})</p>
                  <p class="flex items-center gap-2"><span class="h-2.5 w-2.5 rounded-full bg-brass"></span>错误 {{ stats.base.memory_type.error }} ({{ formatPercent(errorRate) }})</p>
                </div>
              </div>
            </article>
          </div>

          <article class="panel p-6">
            <div class="flex items-center justify-between gap-3">
              <p class="text-xs uppercase tracking-[0.35em] text-slate-400">Hot Tags Top {{ stats.base.hot_tag_top_limit || 10 }}</p>
              <button class="ghost-btn px-3 py-2 text-xs" type="button" @click="loadDashboardStats">刷新统计</button>
            </div>
            <div class="mt-5 flex flex-wrap gap-2.5">
              <span
                v-for="item in stats.base.hot_tags"
                :key="`hot-tag-${item.tag}`"
                class="rounded-full border border-slate-200 bg-white px-3 py-1.5 text-xs font-medium text-slate-600"
              >
                {{ item.tag }} · {{ item.count }}
              </span>
              <span v-if="!stats.base.hot_tags?.length" class="text-sm text-slate-400">暂无标签数据</span>
            </div>
          </article>
        </section>

        <aside class="space-y-6">
          <article class="panel p-6">
            <div class="flex items-center justify-between gap-3">
              <p class="text-xs uppercase tracking-[0.35em] text-slate-400">Cache Runtime</p>
              <span class="rounded-full px-2.5 py-1 text-xs font-medium" :class="stats.cache.enabled ? 'bg-pine/10 text-pine' : 'bg-slate-200 text-slate-500'">
                {{ stats.cache.enabled ? '已启用' : '未启用' }}
              </span>
            </div>

            <div v-if="stats.cache.enabled" class="mt-5 space-y-3">
              <div class="rounded-2xl border border-slate-200 bg-mist/60 p-4">
                <p class="text-xs text-slate-500">查询向量缓存条目数</p>
                <p class="mt-2 text-xl font-semibold text-ink">{{ stats.cache.query_embedding_entry_count }}</p>
              </div>
              <div class="rounded-2xl border border-slate-200 bg-mist/60 p-4">
                <p class="text-xs text-slate-500">语义命中缓存条目数</p>
                <p class="mt-2 text-xl font-semibold text-ink">{{ stats.cache.semantic_hits_entry_count }}</p>
              </div>
              <div class="rounded-2xl border border-slate-200 bg-mist/60 p-4">
                <p class="text-xs text-slate-500">缓存命中率</p>
                <p class="mt-2 text-xl font-semibold text-ink">{{ formatPercent(stats.cache.hit_rate) }}</p>
              </div>
              <div class="rounded-2xl border border-slate-200 bg-mist/60 p-4">
                <p class="text-xs text-slate-500">缓存总内存占用估算</p>
                <p class="mt-2 text-xl font-semibold text-ink">{{ stats.cache.estimated_memory_human || '-' }}</p>
              </div>
              <p class="text-xs text-slate-400">缓存统计刷新频率：{{ stats.cache_refresh_interval_seconds }} 秒</p>
            </div>

            <p v-else class="mt-5 rounded-2xl border border-dashed border-slate-200 bg-mist/50 px-4 py-3 text-sm text-slate-500">
              当前未启用查询缓存，可在 `search.cache.enabled` 打开后查看缓存运行统计。
            </p>
          </article>
        </aside>
      </div>

      <p v-if="errorMessage" class="rounded-2xl bg-coral/10 px-4 py-3 text-sm text-coral">{{ errorMessage }}</p>
    </section>
  </AdminShell>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import AdminShell from '../components/AdminShell.vue'
import { fetchCurrentUser, fetchDashboardStats, fetchDashboardCacheStats } from '../lib/api'
import { getStoredUser, saveSession, getToken } from '../lib/auth'

const profile = ref(getStoredUser())
const errorMessage = ref('')
const pollTimer = ref(null)
const stats = ref({
  base: {
    user_total: 0,
    memory_total: 0,
    memory_type: { summary: 0, error: 0 },
    hot_tags: [],
    hot_tag_top_limit: 10,
  },
  cache: {
    enabled: false,
    query_embedding_entry_count: 0,
    semantic_hits_entry_count: 0,
    hit_rate: 0,
    estimated_memory_human: '-',
  },
  cache_refresh_interval_seconds: 0,
})

// summaryRate 计算总结记忆占比，避免模板层重复处理除零分支。
const summaryRate = computed(() => {
  if (!stats.value.base.memory_total) {
    return 0
  }
  return stats.value.base.memory_type.summary / stats.value.base.memory_total
})

// errorRate 计算错误记忆占比，保证图表和图例使用统一口径。
const errorRate = computed(() => {
  if (!stats.value.base.memory_total) {
    return 0
  }
  return stats.value.base.memory_type.error / stats.value.base.memory_total
})

// memoryTypeChartStyle 用环形渐变展示记忆类型分布，避免引入额外图表依赖。
const memoryTypeChartStyle = computed(() => {
  if (!stats.value.base.memory_total) {
    return { background: 'conic-gradient(#dbe3ea 0 100%)' }
  }
  const summaryPercent = Math.round(summaryRate.value * 100)
  return { background: `conic-gradient(#1d5f54 0 ${summaryPercent}%, #c37b2c ${summaryPercent}% 100%)` }
})

// loadProfile 在进入总览页时刷新一次当前用户，避免本地缓存与后端状态长期漂移。
async function loadProfile() {
  const response = await fetchCurrentUser()
  profile.value = response.item
  saveSession(getToken(), response.item)
}

// startCachePolling 按配置启动缓存统计轮询，避免在缓存关闭或频率为 0 时产生无效请求。
function startCachePolling() {
  if (pollTimer.value) {
    clearInterval(pollTimer.value)
    pollTimer.value = null
  }
  const intervalSeconds = Number(stats.value.cache_refresh_interval_seconds || 0)
  if (!stats.value.cache.enabled || intervalSeconds <= 0) {
    return
  }
  pollTimer.value = setInterval(() => {
    loadCacheStats().catch(() => {})
  }, intervalSeconds * 1000)
}

// loadDashboardStats 拉取基础统计并同步缓存配置，确保总览页初始化后即可显示完整指标。
async function loadDashboardStats() {
  const response = await fetchDashboardStats()
  stats.value = response
  startCachePolling()
}

// loadCacheStats 刷新缓存快照并保留基础统计，避免轮询时覆盖已有卡片数据。
async function loadCacheStats() {
  if (!stats.value.cache.enabled) {
    return
  }
  const response = await fetchDashboardCacheStats()
  stats.value = { ...stats.value, cache: response.cache }
}

// formatPercent 统一格式化百分比展示，避免不同卡片精度不一致。
function formatPercent(value) {
  return `${(Number(value || 0) * 100).toFixed(1)}%`
}

onMounted(() => {
  Promise.all([loadProfile(), loadDashboardStats()]).catch((error) => {
    errorMessage.value = error.message || '加载总览统计失败'
  })
})

onUnmounted(() => {
  if (!pollTimer.value) {
    return
  }
  clearInterval(pollTimer.value)
  pollTimer.value = null
})
</script>
