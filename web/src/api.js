import { auth } from './store'

// 统一请求封装：自动带令牌，401 时回登录页
export async function request(path, { method = 'GET', body } = {}) {
  const res = await fetch(path, {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...(auth.token ? { Authorization: `Bearer ${auth.token}` } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (res.status === 401) {
    auth.logout()
    location.hash = '#/login'
    throw new Error('登录已失效')
  }
  let data = null
  const text = await res.text()
  if (text) {
    try { data = JSON.parse(text) } catch { data = null }
  }
  if (!res.ok) {
    throw new Error((data && data.error) || `请求失败（${res.status}）`)
  }
  return data
}
