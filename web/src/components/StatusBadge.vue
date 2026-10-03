<template>
  <span class="badge" :class="status">{{ label }}</span>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({ status: String })
const labels = {
  pending: '待 IT 核对',
  ready: '待员工确认',
  blocked: '受阻',
  completed: '已完成',
  cancelled: '已取消',
}
const label = computed(() => labels[props.status] || props.status)
</script>
