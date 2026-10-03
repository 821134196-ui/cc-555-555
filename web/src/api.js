// 轻量 API 封装：JWT 存 localStorage，401 时回到登录页
let token = localStorage.getItem('sm_token') || ''

export function setToken(t) {
  token = t || ''
  if (t) localStorage.setItem('sm_token', t)
  else localStorage.removeItem('sm_token')
}
export function getToken() { return token }

export class ApiError extends Error {
  constructor(status, msg) { super(msg); this.status = status }
}

async function request(method, path, body) {
  const headers = { 'Content-Type': 'application/json' }
  if (token) headers.Authorization = 'Bearer ' + token
  const resp = await fetch('/api' + path, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined
  })
  let data = {}
  try { data = await resp.json() } catch (_) { /* 无 body */ }
  if (!resp.ok) {
    if (resp.status === 401) {
      setToken('')
      window.dispatchEvent(new CustomEvent('sm-unauth'))
    }
    throw new ApiError(resp.status, data.error || `请求失败 (${resp.status})`)
  }
  return data
}

export const api = {
  login: (username) => request('POST', '/login', { username }),
  me: () => request('GET', '/me'),
  floors: () => request('GET', '/floors'),
  seats: (floor) => request('GET', '/seats' + (floor ? `?floor=${encodeURIComponent(floor)}` : '')),
  activeOrderBySeat: (seatId) => request('GET', `/seats/${seatId}/active-order`),
  employees: () => request('GET', '/employees'),
  orders: (status) => request('GET', '/orders' + (status ? `?status=${encodeURIComponent(status)}` : '')),
  order: (id) => request('GET', `/orders/${id}`),
  createOrder: (payload) => request('POST', '/orders', payload),
  reschedule: (id, payload) => request('POST', `/orders/${id}/reschedule`, payload),
  cancel: (id, reason) => request('POST', `/orders/${id}/cancel`, { reason }),
  itCheck: (id, payload) => request('POST', `/orders/${id}/it-check`, payload),
  setNetState: (seatId, enabled, reason) =>
    request('POST', `/seats/${seatId}/net-state`, { enabled, reason }),
  confirm: (id, payload) => request('POST', `/orders/${id}/confirm`, payload)
}
