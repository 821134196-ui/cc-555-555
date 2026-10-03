<template>
  <div v-if="!user" class="login-wrap">
    <LoginView @logged="onLogged" />
  </div>
  <div v-else class="app">
    <header class="topbar">
      <div class="brand">🪑 工位搬迁协同系统</div>
      <nav class="tabs">
        <button :class="{ active: tab === 'map' }" @click="tab = 'map'">楼层图</button>
        <button :class="{ active: tab === 'orders' }" @click="switchOrders">
          {{ user.role === 'employee' ? '我的搬迁单' : '搬迁单' }}
        </button>
      </nav>
      <div class="user-box">
        <span class="user-name">{{ user.name }}</span>
        <span class="role-badge" :data-role="user.role">{{ roleText(user.role) }}</span>
        <button class="btn btn-sm" @click="logout">退出</button>
      </div>
    </header>

    <main class="content">
      <FloorMap
        v-if="tab === 'map'"
        :user="user"
        :refresh-key="mapRefresh"
        @toast="toast"
        @changed="bump"
        @open-order="openOrder"
      />
      <OrderList
        v-else
        :user="user"
        :refresh-key="listRefresh"
        @toast="toast"
        @changed="bump"
        @open="openOrder"
        @create="tab = 'map'"
      />
    </main>

    <OrderDetail
      :id="activeOrderId"
      :user="user"
      @close="activeOrderId = null"
      @toast="toast"
      @changed="bump"
    />

    <transition name="fade">
      <div v-if="toastMsg" class="toast" :class="toastKind === 'ok' ? 'toast-ok' : 'toast-error'">
        {{ toastMsg }}
      </div>
    </transition>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import LoginView from './components/LoginView.vue'
import FloorMap from './components/FloorMap.vue'
import OrderList from './components/OrderList.vue'
import OrderDetail from './components/OrderDetail.vue'
import { api, setToken } from './api.js'

const user = ref(null)
const tab = ref('map')
const activeOrderId = ref(null)
const mapRefresh = ref(0)
const listRefresh = ref(0)
const toastMsg = ref('')
const toastKind = ref('error')
let toastTimer = null

onMounted(async () => {
  window.addEventListener('sm-unauth', onUnauth)
  try {
    user.value = await api.me()
  } catch (_) { /* 未登录 */ }
})
onUnmounted(() => window.removeEventListener('sm-unauth', onUnauth))

function onUnauth() { user.value = null }

function onLogged(u) {
  user.value = u
  tab.value = 'map'
  bump()
}
function logout() {
  setToken('')
  user.value = null
  activeOrderId.value = null
}
function switchOrders() {
  tab.value = 'orders'
  listRefresh.value++
}
function openOrder(id) {
  activeOrderId.value = id
}
function bump() {
  mapRefresh.value++
  listRefresh.value++
}
function toast(msg, kind = 'error') {
  toastMsg.value = msg
  toastKind.value = kind
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => (toastMsg.value = ''), 3200)
}

function roleText(r) {
  return { admin: '行政', it: 'IT', employee: '员工' }[r] || r
}
</script>

<style scoped>
.login-wrap { min-height: 100vh; display: flex; align-items: center; justify-content: center; }
.app { min-height: 100vh; }
.topbar {
  height: 56px; background: #fff; border-bottom: 1px solid var(--line);
  display: flex; align-items: center; padding: 0 22px; gap: 28px;
  position: sticky; top: 0; z-index: 100;
}
.brand { font-weight: 700; font-size: 16px; }
.tabs { display: flex; gap: 6px; flex: 1; }
.tabs button {
  border: none; background: none; padding: 8px 16px; border-radius: 8px;
  cursor: pointer; font-size: 14px; color: var(--muted);
}
.tabs button.active { background: #eaf1fe; color: var(--brand); font-weight: 600; }
.user-box { display: flex; align-items: center; gap: 10px; }
.user-name { font-size: 13px; font-weight: 600; }
.role-badge {
  font-size: 11px; padding: 2px 8px; border-radius: 999px; font-weight: 600;
  background: #eef1f5; color: #5b6675;
}
.role-badge[data-role="admin"] { background: #fdecec; color: #c0392b; }
.role-badge[data-role="it"] { background: #e7f0ff; color: #2f6fed; }
.role-badge[data-role="employee"] { background: var(--ok-bg); color: #178a5b; }
.content { padding: 20px 22px; max-width: 1280px; margin: 0 auto; }
.fade-enter-active, .fade-leave-active { transition: opacity .3s; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
