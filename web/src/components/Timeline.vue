<template>
  <ul class="timeline">
    <li v-for="e in events" :key="e.id">
      <div class="t-head">{{ actionLabel(e.action) }} · {{ e.actor?.name }}</div>
      <div class="t-detail">{{ e.detail }}</div>
      <div class="t-time">{{ formatTime(e.created_at) }}</div>
    </li>
  </ul>
</template>

<script setup>
defineProps({ events: { type: Array, default: () => [] } })

const labels = {
  created: '创建搬迁单',
  rescheduled: '改期',
  seat_changed: '更换目标座位',
  cancelled: '取消搬迁',
  network_test_passed: '网口测试通过',
  network_test_failed: '网口测试失败',
  phone_migrated: '分机迁移成功',
  phone_failed: '分机迁移失败',
  monitor_checked: '显示器核对',
  asset_mismatch: '资产标签不符',
  asset_resolved: '资产异常解除',
  it_ready: 'IT 核对完成',
  employee_confirmed: '员工确认接收',
  employee_issue: '员工反馈问题',
  completed: '搬迁完成',
}
function actionLabel(a) {
  return labels[a] || a
}
function formatTime(t) {
  if (!t) return ''
  return new Date(t).toLocaleString('zh-CN', { hour12: false })
}
</script>
