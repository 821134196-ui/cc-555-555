<template>
  <div>
    <div class="panel toolbar">
      <div class="filters">
        <button
          v-for="f in filters"
          :key="f.value"
          class="btn btn-sm"
          :class="{ 'filter-active': status === f.value }"
          @click="status = f.value; load()"
        >{{ f.label }}</button>
      </div>
      <button class="btn btn-sm" @click="load">刷新</button>
    </div>

    <div class="panel">
      <table v-if="orders.length" class="order-table">
        <thead>
          <tr>
            <th>#</th><th>员工</th><th>原座位</th><th>目标座位</th>
            <th>搬迁日期</th><th>状态</th><th>IT 核对</th><th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="o in orders" :key="o.id">
            <td>{{ o.id }}</td>
            <td>{{ o.employee.name }}</td>
            <td>{{ o.from_seat?.code || '—' }}</td>
            <td>{{ o.to_seat.code }}
              <i v-if="!o.to_seat.net_enabled" class="fault">网口故障</i>
            </td>
            <td>{{ fmtDate(o.move_date) }}</td>
            <td><span class="tag" :class="'tag-' + o.status">{{ statusText(o.status) }}</span></td>
            <td>
              <span v-if="o.checked_at" class="check-line">
                <b :class="o.net_tested ? 'ok' : 'no'">网络 {{ o.net_tested ? '✓' : '✗' }}</b>
                <b :class="o.asset_match ? 'ok' : 'no'">标签 {{ o.asset_match ? '✓' : '✗' }}</b>
              </span>
              <span v-else class="muted">待核对</span>
            </td>
            <td>
              <button class="btn btn-sm" @click="$emit('open', o.id)">查看/处理</button>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else class="muted empty">暂无搬迁单</p>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, onMounted } from 'vue'
import { api } from '../api.js'

const props = defineProps({
  user: Object,
  refreshKey: Number
})
defineEmits(['toast', 'changed', 'open', 'create'])

const filters = [
  { value: '', label: '全部' },
  { value: 'reserved', label: '待 IT 核对' },
  { value: 'it_checked', label: '待员工确认' },
  { value: 'confirmed', label: '已完成' },
  { value: 'cancelled', label: '已取消' }
]
const status = ref('')
const orders = ref([])

onMounted(load)
watch(() => props.refreshKey, load)

async function load() {
  try {
    const { orders: os } = await api.orders(status.value)
    orders.value = os
  } catch (e) {
    /* 列表静默，全局无处理 */
  }
}
function fmtDate(d) {
  return d ? d.slice(0, 10) : '—'
}
function statusText(s) {
  return {
    reserved: '预占待核对', it_checked: 'IT 已核对，待员工确认',
    confirmed: '已完成', cancelled: '已取消'
  }[s] || s
}
</script>

<style scoped>
.toolbar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; }
.filters { display: flex; gap: 8px; }
.filter-active { background: var(--brand); color: #fff; border-color: var(--brand); }
.order-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.order-table th, .order-table td { text-align: left; padding: 10px 8px; border-bottom: 1px solid var(--line); }
.order-table th { color: var(--muted); font-weight: 600; font-size: 12px; }
.check-line { display: flex; gap: 8px; }
.ok { color: #178a5b; }
.no { color: var(--danger); }
.fault {
  font-style: normal; font-size: 11px; color: var(--danger);
  background: var(--danger-bg); border-radius: 4px; padding: 1px 6px; margin-left: 6px;
}
.empty { text-align: center; padding: 40px 0; margin: 0; }
</style>
