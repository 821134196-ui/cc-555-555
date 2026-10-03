<template>
  <div class="page">
    <div class="toolbar">
      <div class="filters">
        <span class="muted">状态筛选</span>
        <select v-model="statusFilter" @change="load">
          <option value="">全部</option>
          <option value="pending">待 IT 核对</option>
          <option value="ready">待员工确认</option>
          <option value="blocked">受阻</option>
          <option value="completed">已完成</option>
          <option value="cancelled">已取消</option>
        </select>
      </div>
      <router-link v-if="auth.user?.role === 'admin'" to="/tickets/new" class="btn primary">
        + 新建搬迁单
      </router-link>
    </div>

    <div v-if="error" class="alert error">{{ error }}</div>

    <div class="card">
      <table class="list" v-if="tickets.length">
        <thead>
          <tr>
            <th>单号</th><th>员工</th><th>原座位</th><th>目标座位</th>
            <th>搬迁日期</th><th>状态</th><th>网口</th><th>分机</th><th>显示器</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="t in tickets" :key="t.id" @click="$router.push(`/tickets/${t.id}`)">
            <td>{{ t.ticket_no }}</td>
            <td>{{ t.employee?.name }}</td>
            <td>{{ t.from_seat?.code }}</td>
            <td>{{ t.to_seat?.code }}</td>
            <td>{{ t.move_date }}</td>
            <td><StatusBadge :status="t.status" /></td>
            <td v-html="icon(t.network_ok)"></td>
            <td v-html="icon(t.phone_ok)"></td>
            <td v-html="icon(t.monitor_ok)"></td>
          </tr>
        </tbody>
      </table>
      <p v-else class="muted">暂无搬迁单</p>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { request } from '../api'
import { auth } from '../store'
import StatusBadge from '../components/StatusBadge.vue'

const tickets = ref([])
const statusFilter = ref('')
const error = ref('')

function icon(ok) {
  return ok ? '<span style="color:#16a34a">✓</span>' : '<span style="color:#d97706">—</span>'
}

async function load() {
  error.value = ''
  try {
    const q = statusFilter.value ? `?status=${statusFilter.value}` : ''
    tickets.value = await request(`/api/tickets${q}`)
  } catch (e) {
    error.value = e.message
  }
}
onMounted(load)
</script>
