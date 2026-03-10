<template>
  <AdminShell>
    <section class="space-y-6">
      <div class="panel p-6 sm:p-8">
        <div class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <p class="text-xs uppercase tracking-[0.35em] text-slate-400">Cleanup Governance</p>
            <h2 class="mt-3 text-3xl font-semibold text-ink">清理治理</h2>
            <p class="mt-2 text-sm text-slate-500">统一管理保护标签、生成待清理候选，并完成审核与执行。</p>
          </div>
          <div class="flex flex-wrap gap-3">
            <button class="ghost-btn" type="button" :disabled="runningCleanup" @click="refreshAll">刷新</button>
            <button class="primary-btn" type="button" :disabled="runningCleanup" @click="handleRunCleanup">
              {{ runningCleanup ? '生成中...' : '生成待审核清单' }}
            </button>
          </div>
        </div>
        <p v-if="pageMessage" class="mt-4 rounded-2xl bg-emerald-50 px-4 py-3 text-sm text-emerald-700">{{ pageMessage }}</p>
        <p v-if="pageError" class="mt-4 rounded-2xl bg-coral/10 px-4 py-3 text-sm text-coral">{{ pageError }}</p>
      </div>

      <section class="grid gap-6 xl:grid-cols-[420px_minmax(0,1fr)]">
        <div class="panel p-6 sm:p-8">
          <div class="flex items-start justify-between gap-4">
            <div>
              <p class="text-xs uppercase tracking-[0.35em] text-slate-400">Protected Tags</p>
              <h3 class="mt-3 text-2xl font-semibold text-ink">保护标签</h3>
              <p class="mt-2 text-sm text-slate-500">命中这些标签的记忆不会进入清理候选。</p>
            </div>
            <button class="ghost-btn" type="button" @click="resetProtectedTagForm">清空表单</button>
          </div>

          <form class="mt-6 space-y-4" @submit.prevent="handleSaveProtectedTag">
            <label class="block space-y-2">
              <span class="text-sm font-medium text-slate-700">标签名</span>
              <input v-model="tagForm.tag" class="field" placeholder="例如：核心故障" />
            </label>
            <label class="block space-y-2">
              <span class="text-sm font-medium text-slate-700">说明</span>
              <textarea v-model="tagForm.description" class="field min-h-24" placeholder="说明为什么需要长期保护" />
            </label>
            <label class="flex items-center gap-3 rounded-2xl border border-slate-200 bg-mist/60 px-4 py-3 text-sm text-slate-700">
              <input v-model="tagForm.enabled" class="h-4 w-4 rounded border-slate-300 text-ink" type="checkbox" />
              立即启用保护
            </label>
            <p v-if="tagError" class="rounded-2xl bg-coral/10 px-4 py-3 text-sm text-coral">{{ tagError }}</p>
            <div class="flex gap-3">
              <button class="ghost-btn flex-1" type="button" @click="resetProtectedTagForm">取消</button>
              <button class="primary-btn flex-1" type="submit" :disabled="tagSaving">
                {{ tagSaving ? '保存中...' : tagForm.id ? '保存修改' : '新增标签' }}
              </button>
            </div>
          </form>

          <div class="mt-6 space-y-3">
            <article v-for="item in protectedTags" :key="item.id" class="rounded-3xl border border-slate-200 bg-white/80 p-4">
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0 flex-1">
                  <div class="flex flex-wrap items-center gap-2">
                    <p class="font-semibold text-ink">{{ item.tag }}</p>
                    <span class="rounded-full px-3 py-1 text-xs" :class="item.enabled ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-500'">
                      {{ item.enabled ? '启用中' : '已停用' }}
                    </span>
                  </div>
                  <p class="mt-2 text-sm text-slate-500">{{ item.description || '暂无说明' }}</p>
                  <p class="mt-2 text-xs text-slate-400">来源：{{ item.source || '-' }}</p>
                </div>
                <div class="flex shrink-0 gap-2">
                  <button class="ghost-btn px-3 py-2 text-xs" type="button" @click="startEditProtectedTag(item)">编辑</button>
                  <button class="danger-btn px-3 py-2 text-xs" type="button" @click="handleDeleteProtectedTag(item)">删除</button>
                </div>
              </div>
            </article>
            <p v-if="!protectedTags.length" class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-400">暂无保护标签</p>
          </div>
        </div>

        <div class="space-y-6">
          <div class="panel p-6 sm:p-8">
            <div class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
              <div>
                <p class="text-xs uppercase tracking-[0.35em] text-slate-400">Cleanup Reviews</p>
                <h3 class="mt-3 text-2xl font-semibold text-ink">待清理清单</h3>
                <p class="mt-2 text-sm text-slate-500">先审核候选，再批量批准、拒绝或执行删除。</p>
              </div>
              <div class="flex flex-wrap gap-3">
                <button class="ghost-btn" type="button" :disabled="!selectedReviewIDs.length || reviewSubmitting" @click="handleRejectSelected">批量拒绝</button>
                <button class="ghost-btn" type="button" :disabled="!selectedReviewIDs.length || reviewSubmitting" @click="handleApproveSelected">批量批准</button>
                <button class="primary-btn" type="button" :disabled="!selectedApprovedReviewIDs.length || reviewSubmitting" @click="handleExecuteApproved">
                  {{ reviewSubmitting ? '处理中...' : '执行已批准候选' }}
                </button>
              </div>
            </div>

            <form class="mt-6 grid gap-4 lg:grid-cols-[180px_180px_minmax(0,1fr)_120px]" @submit.prevent="handleReviewSearch">
              <label class="block space-y-2">
                <span class="text-sm font-medium text-slate-700">状态</span>
                <select v-model="reviewFilters.status" class="field">
                  <option value="">全部状态</option>
                  <option value="pending">待审核</option>
                  <option value="approved">已批准</option>
                  <option value="rejected">已拒绝</option>
                  <option value="executed">已执行</option>
                </select>
              </label>
              <label class="block space-y-2">
                <span class="text-sm font-medium text-slate-700">类型</span>
                <select v-model="reviewFilters.type" class="field">
                  <option value="">全部类型</option>
                  <option value="summary">总结记忆</option>
                  <option value="error">错误记忆</option>
                </select>
              </label>
              <label class="block space-y-2">
                <span class="text-sm font-medium text-slate-700">项目</span>
                <input v-model="reviewFilters.project_name" class="field" placeholder="按 project_name 过滤" />
              </label>
              <div class="flex items-end gap-3">
                <button class="primary-btn flex-1" type="submit">筛选</button>
              </div>
            </form>
            <p class="mt-4 text-sm text-slate-500">执行操作只会处理当前勾选且状态为“已批准”的候选。</p>
          </div>

          <section class="panel overflow-hidden">
            <div class="overflow-x-auto">
              <table class="min-w-full divide-y divide-slate-200 text-sm">
                <thead class="bg-slate-50/80 text-left text-slate-500">
                  <tr>
                    <th class="px-5 py-4 font-medium">
                      <input :checked="allVisibleSelected" class="h-4 w-4 rounded border-slate-300" type="checkbox" @change="toggleSelectVisible($event)" />
                    </th>
                    <th class="px-5 py-4 font-medium">标题</th>
                    <th class="px-5 py-4 font-medium">项目</th>
                    <th class="px-5 py-4 font-medium">类型</th>
                    <th class="px-5 py-4 font-medium">状态</th>
                    <th class="px-5 py-4 font-medium">评分</th>
                    <th class="px-5 py-4 font-medium">使用次数</th>
                    <th class="px-5 py-4 font-medium">最近使用</th>
                    <th class="px-5 py-4 font-medium">原因</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-slate-100 bg-white/90">
                  <tr v-if="reviewsLoading">
                    <td class="px-5 py-10 text-center text-slate-400" colspan="9">正在加载待清理清单...</td>
                  </tr>
                  <tr v-else-if="!reviews.length">
                    <td class="px-5 py-10 text-center text-slate-400" colspan="9">暂无候选记录</td>
                  </tr>
                  <tr v-for="item in reviews" :key="item.id" class="align-top">
                    <td class="px-5 py-4">
                      <input v-model="selectedReviewIDs" class="h-4 w-4 rounded border-slate-300" type="checkbox" :value="item.id" />
                    </td>
                    <td class="px-5 py-4 text-ink">
                      <p class="max-w-[16rem] break-words font-medium">{{ item.snapshot?.title || `记忆 #${item.memory_id}` }}</p>
                      <p class="mt-1 text-xs text-slate-400">ID: {{ item.memory_id }}</p>
                    </td>
                    <td class="px-5 py-4 text-slate-600">{{ item.project_name || '-' }}</td>
                    <td class="px-5 py-4 text-slate-600">{{ item.type === 'error' ? '错误记忆' : '总结记忆' }}</td>
                    <td class="px-5 py-4 text-slate-600">{{ reviewStatusLabel(item.status) }}</td>
                    <td class="px-5 py-4 text-slate-600">{{ formatScore(item.score) }}</td>
                    <td class="px-5 py-4 text-slate-600">{{ item.reason?.use_count ?? '-' }}</td>
                    <td class="px-5 py-4 text-slate-600">{{ formatDate(item.snapshot?.last_used_at) }}</td>
                    <td class="px-5 py-4 text-slate-500">
                      <div class="max-w-[20rem] space-y-1 text-xs leading-6">
                        <p>年龄: {{ item.reason?.age_days ?? '-' }} 天</p>
                        <p>最近使用间隔: {{ item.reason?.last_used_days ?? '-' }} 天</p>
                        <p>项目库存: {{ item.reason?.project_type_count ?? '-' }}</p>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>

            <div class="flex flex-col gap-4 border-t border-slate-200 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
              <p class="text-sm text-slate-500">共 {{ reviewTotal }} 条，当前第 {{ reviewPage }} / {{ reviewTotalPage || 1 }} 页</p>
              <div class="flex flex-wrap gap-3">
                <button class="ghost-btn" type="button" :disabled="reviewPage <= 1 || reviewsLoading" @click="changeReviewPage(reviewPage - 1)">上一页</button>
                <button class="ghost-btn" type="button" :disabled="reviewPage >= reviewTotalPage || reviewsLoading || reviewTotalPage === 0" @click="changeReviewPage(reviewPage + 1)">下一页</button>
              </div>
            </div>
          </section>
        </div>
      </section>
    </section>
  </AdminShell>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import AdminShell from '../components/AdminShell.vue'
import {
  approveCleanupReviews,
  createProtectedTag,
  deleteProtectedTag,
  executeCleanupReviews,
  fetchCleanupReviews,
  fetchProtectedTags,
  rejectCleanupReviews,
  runCleanupReviews,
  updateProtectedTag,
} from '../lib/api'

const pageError = ref('')
const pageMessage = ref('')
const runningCleanup = ref(false)
const tagSaving = ref(false)
const tagError = ref('')
const protectedTags = ref([])
const reviews = ref([])
const reviewsLoading = ref(false)
const reviewSubmitting = ref(false)
const reviewPage = ref(1)
const reviewPageSize = ref(10)
const reviewTotal = ref(0)
const reviewTotalPage = ref(0)
const selectedReviewIDs = ref([])
const tagForm = reactive({ id: 0, tag: '', description: '', enabled: true })
const reviewFilters = reactive({ status: '', type: '', project_name: '' })

const allVisibleSelected = computed(() => reviews.value.length > 0 && reviews.value.every((item) => selectedReviewIDs.value.includes(item.id)))
const selectedApprovedReviewIDs = computed(() => {
  const selected = new Set(selectedReviewIDs.value)
  return reviews.value.filter((item) => selected.has(item.id) && item.status === 'approved').map((item) => item.id)
})

// refreshAll 统一刷新保护标签和审核列表，避免页面各区域更新口径不一致。
async function refreshAll() {
  await Promise.all([loadProtectedTags(), loadReviews()])
}

// loadProtectedTags 拉取当前保护标签列表，确保管理员看到的就是数据库生效规则。
async function loadProtectedTags() {
  try {
    const response = await fetchProtectedTags()
    protectedTags.value = response.items || []
  } catch (error) {
    pageError.value = error.message
  }
}

// loadReviews 拉取待清理候选分页数据，方便管理员统一审核和执行。
async function loadReviews() {
  reviewsLoading.value = true
  pageError.value = ''
  try {
    const response = await fetchCleanupReviews(reviewPage.value, reviewPageSize.value, reviewFilters)
    reviews.value = response.items || []
    reviewTotal.value = response.total || 0
    reviewTotalPage.value = response.total_page || 0
    reviewPage.value = response.page || 1
    reviewPageSize.value = response.page_size || reviewPageSize.value
    selectedReviewIDs.value = selectedReviewIDs.value.filter((id) => reviews.value.some((item) => item.id === id))
  } catch (error) {
    pageError.value = error.message
  } finally {
    reviewsLoading.value = false
  }
}

// resetProtectedTagForm 清理标签表单状态，避免新增和编辑之间残留旧值。
function resetProtectedTagForm() {
  tagForm.id = 0
  tagForm.tag = ''
  tagForm.description = ''
  tagForm.enabled = true
  tagError.value = ''
}

// startEditProtectedTag 把目标标签灌入表单，避免列表区域承担复杂编辑状态。
function startEditProtectedTag(item) {
  tagForm.id = item.id
  tagForm.tag = item.tag
  tagForm.description = item.description || ''
  tagForm.enabled = Boolean(item.enabled)
  tagError.value = ''
}

// handleSaveProtectedTag 统一处理保护标签新增与编辑流程，确保后台维护入口一致。
async function handleSaveProtectedTag() {
  tagSaving.value = true
  tagError.value = ''
  pageMessage.value = ''
  try {
    if (tagForm.id) {
      await updateProtectedTag(tagForm.id, { tag: tagForm.tag, description: tagForm.description, enabled: tagForm.enabled })
      pageMessage.value = '保护标签已更新。'
    } else {
      await createProtectedTag({ tag: tagForm.tag, description: tagForm.description, enabled: tagForm.enabled })
      pageMessage.value = '保护标签已创建。'
    }
    resetProtectedTagForm()
    await loadProtectedTags()
  } catch (error) {
    tagError.value = error.message
  } finally {
    tagSaving.value = false
  }
}

// handleDeleteProtectedTag 删除前二次确认，避免误删长期保留规则。
async function handleDeleteProtectedTag(item) {
  if (!window.confirm(`确定删除保护标签“${item.tag}”吗？`)) {
    return
  }
  pageError.value = ''
  pageMessage.value = ''
  try {
    await deleteProtectedTag(item.id)
    pageMessage.value = '保护标签已删除。'
    if (tagForm.id === item.id) {
      resetProtectedTagForm()
    }
    await loadProtectedTags()
  } catch (error) {
    pageError.value = error.message
  }
}

// handleRunCleanup 手动生成一轮待审核候选，便于管理员即时观察评分与白名单效果。
async function handleRunCleanup() {
  runningCleanup.value = true
  pageError.value = ''
  pageMessage.value = ''
  try {
    const response = await runCleanupReviews()
    pageMessage.value = response.message || '待审核清单已刷新。'
    reviewPage.value = 1
    await loadReviews()
  } catch (error) {
    pageError.value = error.message
  } finally {
    runningCleanup.value = false
  }
}

// handleReviewSearch 在管理员筛选后回到第一页，避免旧页码导致结果看起来为空。
async function handleReviewSearch() {
  reviewPage.value = 1
  await loadReviews()
}

// changeReviewPage 统一处理审核分页切换，避免模板里散落边界判断。
async function changeReviewPage(nextPage) {
  if (nextPage < 1 || (reviewTotalPage.value > 0 && nextPage > reviewTotalPage.value)) {
    return
  }
  reviewPage.value = nextPage
  await loadReviews()
}

// toggleSelectVisible 支持当前页全选或取消全选，减少管理员批量审核的点击成本。
function toggleSelectVisible(event) {
  if (event.target.checked) {
    const merged = new Set([...selectedReviewIDs.value, ...reviews.value.map((item) => item.id)])
    selectedReviewIDs.value = Array.from(merged)
    return
  }
  selectedReviewIDs.value = selectedReviewIDs.value.filter((id) => !reviews.value.some((item) => item.id === id))
}

// handleApproveSelected 批量批准当前选中的待审核候选。
async function handleApproveSelected() {
  await handleReviewAction(() => approveCleanupReviews(selectedReviewIDs.value), '已批准所选候选。')
}

// handleRejectSelected 批量拒绝当前选中的待审核候选。
async function handleRejectSelected() {
  await handleReviewAction(() => rejectCleanupReviews(selectedReviewIDs.value), '已拒绝所选候选。')
}

// handleExecuteApproved 执行当前已批准候选，统一复用同一提交流程避免状态不同步。
async function handleExecuteApproved() {
  await handleReviewAction(() => executeCleanupReviews(selectedApprovedReviewIDs.value), '已执行清理任务。')
}

// handleReviewAction 收敛审核和执行动作，确保消息提示与列表刷新口径一致。
async function handleReviewAction(action, successMessage) {
  reviewSubmitting.value = true
  pageError.value = ''
  pageMessage.value = ''
  try {
    const response = await action()
    pageMessage.value = response.message || successMessage
    selectedReviewIDs.value = []
    await loadReviews()
  } catch (error) {
    pageError.value = error.message
  } finally {
    reviewSubmitting.value = false
  }
}

// reviewStatusLabel 统一状态文案，避免模板里散落状态码判断。
function reviewStatusLabel(status) {
  if (status === 'approved') return '已批准'
  if (status === 'rejected') return '已拒绝'
  if (status === 'executed') return '已执行'
  return '待审核'
}

// formatScore 格式化候选评分，便于列表快速比较优先级高低。
function formatScore(value) {
  const score = Number(value || 0)
  if (!Number.isFinite(score)) {
    return '-'
  }
  return score.toFixed(3)
}

// formatDate 统一展示日期时间，避免空值或非法时间直接渲染为杂乱字符串。
function formatDate(value) {
  if (!value) {
    return '-'
  }
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return value
  }
  return parsed.toLocaleString('zh-CN', { hour12: false })
}

onMounted(() => {
  refreshAll()
})
</script>
