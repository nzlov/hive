import { clearSession, getToken, saveSession } from './auth'

// request 统一封装管理端请求，避免每个页面重复拼接 JWT 请求头和错误处理。
async function request(url, options = {}) {
  const headers = new Headers(options.headers || {})
  if (!headers.has('Content-Type') && options.body) {
    headers.set('Content-Type', 'application/json')
  }
  const token = getToken()
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  const response = await fetch(url, { ...options, headers })
  const payload = await response.json().catch(() => ({}))
  if (!response.ok) {
    if (response.status === 401) {
      clearSession()
    }
    throw new Error(payload.error || '请求失败')
  }
  return payload
}

// login 统一处理登录响应，确保令牌与当前用户资料同步写入本地状态。
export async function login(username, password) {
  const payload = await request('/api/v1/users/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
  saveSession(payload.token, payload.user)
  return payload
}

// fetchCurrentUser 用于刷新当前会话身份，避免页面直读本地缓存导致信息过期。
export function fetchCurrentUser() {
  return request('/api/v1/users/me')
}

// fetchDashboardStats 拉取总览页基础统计与缓存配置，避免页面拆分多个初始化请求。
export function fetchDashboardStats() {
  return request('/api/v1/users/stats')
}

// fetchDashboardCacheStats 拉取缓存统计快照，供总览页按配置频率轮询刷新。
export function fetchDashboardCacheStats() {
  return request('/api/v1/users/stats/cache')
}

// fetchUsers 拉取用户分页列表，确保搜索和翻页共享同一查询协议。
export function fetchUsers(page = 1, pageSize = 10, keyword = '') {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  if (String(keyword || '').trim()) {
    params.set('keyword', String(keyword).trim())
  }
  return request(`/api/v1/admin/users?${params.toString()}`)
}

// createUser 创建新用户并返回完整对象，便于页面直接更新列表而不是再次推断生成字段。
export function createUser(payload) {
  return request('/api/v1/admin/users', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

// updateUser 更新用户资料、密码或 token，集中复用同一接口约定。
export function updateUser(id, payload) {
  return request(`/api/v1/admin/users/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

// deleteUser 删除指定用户，保持页面层无需关心删除接口的空响应细节。
export function deleteUser(id) {
  return request(`/api/v1/admin/users/${id}`, { method: 'DELETE' })
}

// fetchMemories 拉取后台记忆分页列表，确保管理页与服务端共享同一筛选协议。
export function fetchMemories(page = 1, pageSize = 10, queries = []) {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  queries.forEach((query) => {
    if (String(query || '').trim()) {
      params.append('queries', String(query).trim())
    }
  })
  return request(`/api/v1/memories?${params.toString()}`)
}

// fetchMemoryDetail 读取单条记忆详情，避免列表页为了抽屉展示预取整段正文。
export function fetchMemoryDetail(id) {
  return request(`/api/v1/memories/${id}`)
}

// updateMemory 更新单条记忆，并由后端同步刷新向量数据保持检索一致。
export function updateMemory(id, payload) {
  return request(`/api/v1/admin/memories/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

// deleteMemory 删除记忆并触发后端同步清理向量，保持索引与主记录一致。
export function deleteMemory(id) {
  return request(`/api/v1/admin/memories/${id}`, { method: 'DELETE' })
}

// fetchMemoryProjects 拉取已有项目名列表，便于管理员通过下拉选择主副项目。
export function fetchMemoryProjects() {
  return request('/api/v1/admin/memories/projects')
}

// mergeMemoryProject 把副项目记忆并入主项目，并由后端同步重建相关向量。
export function mergeMemoryProject(payload) {
  return request('/api/v1/admin/memories/merge-project', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

// fetchProtectedTags 拉取保护标签列表，便于管理员维护清理白名单。
export function fetchProtectedTags() {
  return request('/api/v1/admin/memories/protected-tags')
}

// createProtectedTag 创建新的保护标签规则，避免只能通过改配置文件维护白名单。
export function createProtectedTag(payload) {
  return request('/api/v1/admin/memories/protected-tags', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

// updateProtectedTag 更新保护标签内容或启用状态，确保后台改动可立即生效。
export function updateProtectedTag(id, payload) {
  return request(`/api/v1/admin/memories/protected-tags/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

// deleteProtectedTag 删除不再需要的保护标签规则。
export function deleteProtectedTag(id) {
  return request(`/api/v1/admin/memories/protected-tags/${id}`, { method: 'DELETE' })
}

// fetchCleanupReviews 拉取待清理候选列表，支持按状态、类型和项目筛选。
export function fetchCleanupReviews(page = 1, pageSize = 10, filters = {}) {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  if (String(filters.status || '').trim()) {
    params.set('status', String(filters.status).trim())
  }
  if (String(filters.type || '').trim()) {
    params.set('type', String(filters.type).trim())
  }
  if (String(filters.project_name || '').trim()) {
    params.set('project_name', String(filters.project_name).trim())
  }
  return request(`/api/v1/admin/memories/cleanup-reviews?${params.toString()}`)
}

// runCleanupReviews 手动生成一轮待审核候选，方便管理员在页面里即时预览规则效果。
export function runCleanupReviews() {
  return request('/api/v1/admin/memories/cleanup-reviews/run', { method: 'POST' })
}

// approveCleanupReviews 批量批准候选进入执行队列。
export function approveCleanupReviews(ids) {
  return request('/api/v1/admin/memories/cleanup-reviews/approve', {
    method: 'POST',
    body: JSON.stringify({ ids }),
  })
}

// rejectCleanupReviews 批量拒绝候选，避免误删重要记忆。
export function rejectCleanupReviews(ids) {
  return request('/api/v1/admin/memories/cleanup-reviews/reject', {
    method: 'POST',
    body: JSON.stringify({ ids }),
  })
}

// executeCleanupReviews 执行已批准的候选删除。
export function executeCleanupReviews(ids = []) {
  return request('/api/v1/admin/memories/cleanup-reviews/execute', {
    method: 'POST',
    body: JSON.stringify({ ids }),
  })
}
