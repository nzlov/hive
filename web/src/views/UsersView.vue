<template>
  <AdminShell>
    <section class="space-y-6">
      <div class="panel p-6 sm:p-8">
        <div class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <p class="text-xs uppercase tracking-[0.35em] text-slate-400">User Directory</p>
            <h2 class="mt-3 text-3xl font-semibold text-ink">用户列表</h2>
            <p class="mt-2 text-sm text-slate-500">支持按用户名与真实名搜索，并统一使用分页浏览与侧边栏编辑。</p>
          </div>
          <div class="flex flex-wrap gap-3">
            <button class="ghost-btn" type="button" @click="loadUsers">刷新列表</button>
            <button class="primary-btn" type="button" @click="openCreateDrawer">新增用户</button>
          </div>
        </div>

        <form class="mt-6 grid gap-4 lg:grid-cols-[minmax(0,1fr)_180px_120px]" @submit.prevent="handleSearch">
          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">关键字搜索</span>
            <input v-model="keywordInput" class="field" placeholder="搜索用户名或真实名" />
          </label>
          <label class="block space-y-2">
            <span class="text-sm font-medium text-slate-700">每页条数</span>
            <select v-model.number="pageSize" class="field" @change="handlePageSizeChange">
              <option v-for="size in pageSizeOptions" :key="size" :value="size">{{ size }} 条/页</option>
            </select>
          </label>
          <div class="flex items-end gap-3">
            <button class="primary-btn flex-1" type="submit">搜索</button>
          </div>
        </form>

        <p v-if="listError" class="mt-4 rounded-2xl bg-coral/10 px-4 py-3 text-sm text-coral">{{ listError }}</p>
      </div>

      <section class="panel overflow-hidden">
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-slate-200 text-sm">
            <thead class="bg-slate-50/80 text-left text-slate-500">
              <tr>
                <th class="px-5 py-4 font-medium">真实名称</th>
                <th class="px-5 py-4 font-medium">用户名</th>
                <th class="px-5 py-4 font-medium">UUID</th>
                <th class="px-5 py-4 font-medium">角色</th>
                <th class="px-5 py-4 font-medium">API Token</th>
                <th class="px-5 py-4 font-medium">操作</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-slate-100 bg-white/90">
              <tr v-if="loading">
                <td class="px-5 py-10 text-center text-slate-400" colspan="6">正在加载用户列表...</td>
              </tr>
              <tr v-else-if="!users.length">
                <td class="px-5 py-10 text-center text-slate-400" colspan="6">暂无匹配的用户</td>
              </tr>
              <tr v-for="item in users" :key="item.id" class="align-top">
                <td class="px-5 py-4 font-medium text-ink">{{ item.real_name || item.username || '-' }}</td>
                <td class="px-5 py-4 text-slate-600">{{ item.username || '-' }}</td>
                <td class="px-5 py-4 text-slate-500">
                  <p class="max-w-xs break-all">{{ item.userid || '-' }}</p>
                </td>
                <td class="px-5 py-4">
                  <span class="rounded-full bg-mist px-3 py-1 text-xs text-slate-600">{{ item.is_admin ? '管理员' : '普通用户' }}</span>
                </td>
                <td class="px-5 py-4 text-slate-500">
                  <p class="max-w-xs break-all">{{ item.apitoken || '-' }}</p>
                </td>
                <td class="px-5 py-4">
                  <div class="flex flex-wrap gap-3">
                    <button class="ghost-btn" type="button" @click="startEdit(item)">编辑</button>
                    <button class="danger-btn" type="button" @click="handleDelete(item)">删除</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="flex flex-col gap-4 border-t border-slate-200 px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
          <p class="text-sm text-slate-500">共 {{ total }} 条，当前第 {{ page }} / {{ totalPage || 1 }} 页</p>
          <div class="flex flex-wrap gap-3">
            <button class="ghost-btn" type="button" :disabled="page <= 1 || loading" @click="changePage(page - 1)">上一页</button>
            <button class="ghost-btn" type="button" :disabled="page >= totalPage || loading || totalPage === 0" @click="changePage(page + 1)">下一页</button>
          </div>
        </div>
      </section>
    </section>

    <div v-if="drawerVisible" class="fixed inset-0 z-40 flex justify-end bg-slate-950/35">
      <button class="absolute inset-0 cursor-default" type="button" aria-label="关闭编辑抽屉" @click="closeDrawer" />
      <aside class="relative z-10 flex h-full w-full max-w-2xl flex-col bg-white shadow-2xl">
        <div class="border-b border-slate-200 px-6 py-5">
          <div class="flex items-start justify-between gap-4">
            <div>
              <p class="text-xs uppercase tracking-[0.35em] text-slate-400">User Editor</p>
              <h3 class="mt-3 text-2xl font-semibold text-ink">{{ editingId ? '编辑用户' : '新增用户' }}</h3>
            </div>
            <button class="ghost-btn" type="button" @click="closeDrawer">关闭</button>
          </div>
        </div>

        <div class="flex-1 overflow-y-auto px-6 py-6">
          <form class="space-y-4" @submit.prevent="handleSubmit">
            <label class="block space-y-2">
              <span class="text-sm font-medium text-slate-700">用户名</span>
              <input v-model="form.username" class="field" placeholder="请输入用户名" />
            </label>
            <label class="block space-y-2">
              <span class="text-sm font-medium text-slate-700">真实名称</span>
              <input v-model="form.real_name" class="field" placeholder="请输入真实名称" />
            </label>
            <label class="block space-y-2">
              <span class="text-sm font-medium text-slate-700">密码{{ editingId ? '（留空则不修改）' : '' }}</span>
              <input v-model="form.password" class="field" type="password" placeholder="请输入密码" />
            </label>
            <label class="flex items-center gap-3 rounded-2xl border border-slate-200 bg-mist/60 px-4 py-3 text-sm text-slate-700">
              <input v-model="form.is_admin" class="h-4 w-4 rounded border-slate-300 text-ink" type="checkbox" />
              管理员用户
            </label>
            <label v-if="editingId" class="flex items-center gap-3 rounded-2xl border border-slate-200 bg-mist/60 px-4 py-3 text-sm text-slate-700">
              <input v-model="form.regenerate_token" class="h-4 w-4 rounded border-slate-300 text-ink" type="checkbox" />
              重新生成 API Token
            </label>
            <p v-if="formError" class="rounded-2xl bg-coral/10 px-4 py-3 text-sm text-coral">{{ formError }}</p>
            <div class="flex gap-3 pt-2">
              <button class="ghost-btn flex-1" type="button" @click="closeDrawer">取消</button>
              <button class="primary-btn flex-1" type="submit" :disabled="submitting">
                {{ submitting ? '提交中...' : editingId ? '保存修改' : '创建用户' }}
              </button>
            </div>
          </form>
        </div>
      </aside>
    </div>
  </AdminShell>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import AdminShell from '../components/AdminShell.vue'
import { createUser, deleteUser, fetchUsers, updateUser } from '../lib/api'

const users = ref([])
const loading = ref(false)
const submitting = ref(false)
const listError = ref('')
const formError = ref('')
const keywordInput = ref('')
const page = ref(1)
const pageSize = ref(10)
const total = ref(0)
const totalPage = ref(0)
const drawerVisible = ref(false)
const editingId = ref(0)
const pageSizeOptions = [10, 15, 20, 30, 50]
const form = reactive({
  username: '',
  real_name: '',
  password: '',
  is_admin: false,
  regenerate_token: false,
})

// resetForm 统一清空表单与编辑态，避免侧边栏在新增和编辑之间残留上一次输入。
function resetForm() {
  editingId.value = 0
  formError.value = ''
  form.username = ''
  form.real_name = ''
  form.password = ''
  form.is_admin = false
  form.regenerate_token = false
}

// loadUsers 统一拉取分页用户列表，确保搜索、翻页、增删改后的刷新口径一致。
async function loadUsers() {
  loading.value = true
  listError.value = ''
  try {
    const response = await fetchUsers(page.value, pageSize.value, keywordInput.value)
    users.value = response.items || []
    total.value = response.total || 0
    totalPage.value = response.total_page || 0
    page.value = response.page || 1
    pageSize.value = response.page_size || pageSize.value
  } catch (error) {
    listError.value = error.message
  } finally {
    loading.value = false
  }
}

// openCreateDrawer 先重置表单再打开抽屉，避免误把上一次编辑内容带入新增流程。
function openCreateDrawer() {
  resetForm()
  drawerVisible.value = true
}

// startEdit 把目标用户灌入侧边栏表单，避免列表区域承担复杂编辑状态。
function startEdit(item) {
  drawerVisible.value = true
  editingId.value = item.id
  formError.value = ''
  form.username = item.username
  form.real_name = item.real_name
  form.password = ''
  form.is_admin = item.is_admin
  form.regenerate_token = false
}

// closeDrawer 统一回收抽屉状态，避免关闭后再次打开仍显示旧错误和旧值。
function closeDrawer() {
  drawerVisible.value = false
  resetForm()
}

// handleSearch 在用户显式搜索后回到第一页，避免旧页码导致结果看起来为空。
async function handleSearch() {
  page.value = 1
  await loadUsers()
}

// handlePageSizeChange 修改每页条数后重置页码，避免当前页超出新分页范围。
async function handlePageSizeChange() {
  page.value = 1
  await loadUsers()
}

// changePage 统一处理翻页边界，避免模板层散落页码保护逻辑。
async function changePage(nextPage) {
  if (nextPage < 1 || (totalPage.value > 0 && nextPage > totalPage.value)) {
    return
  }
  page.value = nextPage
  await loadUsers()
}

// handleSubmit 统一新增与编辑提交流程，并在新增后跳回第一页方便立即看到最新记录。
async function handleSubmit() {
  submitting.value = true
  formError.value = ''
  try {
    const isEditing = Boolean(editingId.value)
    if (isEditing) {
      await updateUser(editingId.value, { ...form })
    } else {
      await createUser({ ...form })
      page.value = 1
    }
    await loadUsers()
    closeDrawer()
  } catch (error) {
    formError.value = error.message
  } finally {
    submitting.value = false
  }
}

// handleDelete 删除前先确认，并在最后一条被删掉时自动回退页码避免出现空白页。
async function handleDelete(item) {
  if (!window.confirm(`确认删除用户 ${item.username} 吗？`)) {
    return
  }
  listError.value = ''
  try {
    await deleteUser(item.id)
    if (editingId.value === item.id) {
      closeDrawer()
    }
    if (users.value.length === 1 && page.value > 1) {
      page.value -= 1
    }
    await loadUsers()
  } catch (error) {
    listError.value = error.message
  }
}

onMounted(() => {
  loadUsers()
})
</script>
