<template>
  <div>
    <div class="legend">
      <span><i class="dot available"></i>可用</span>
      <span><i class="dot occupied"></i>已占用</span>
      <span><i class="dot reserved"></i>已预占</span>
      <span v-if="selectable" class="muted">点击绿色座位进行选择</span>
    </div>
    <div class="seat-grid">
      <div v-for="row in rows" :key="row.label" class="seat-row">
        <span class="row-label">{{ row.label }}</span>
        <div
          v-for="seat in row.seats"
          :key="seat.id"
          class="seat"
          :class="[seat.status, { selectable: canPick(seat), selected: seat.id === selectedId }]"
          :title="tooltip(seat)"
          @click="pick(seat)"
        >
          <span v-if="!seat.port_ok" class="bad-port" title="网口故障">⚠️</span>
          <span class="code">{{ seat.code }}</span>
          <span class="sub">{{ subtitle(seat) }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  seats: { type: Array, default: () => [] },
  selectable: Boolean,       // 选座模式：仅可用座位可点
  selectedId: Number,
})
const emit = defineEmits(['select'])

// 按排分组，行号 → A/B/C...
const rows = computed(() => {
  const map = new Map()
  for (const s of props.seats) {
    if (!map.has(s.row)) map.set(s.row, [])
    map.get(s.row).push(s)
  }
  return [...map.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([row, seats]) => ({
      label: String.fromCharCode(65 + row),
      seats: seats.sort((a, b) => a.col - b.col),
    }))
})

function canPick(seat) {
  return props.selectable && seat.status === 'available'
}
function pick(seat) {
  if (canPick(seat)) emit('select', seat)
}
function subtitle(seat) {
  if (seat.status === 'occupied') return seat.occupant_name || '已占用'
  if (seat.status === 'reserved') return seat.active_ticket_no || '已预占'
  return '可用'
}
function tooltip(seat) {
  const lines = [`座位 ${seat.code}`, `网口 ${seat.port_code}${seat.port_ok ? '' : '（故障）'}`]
  lines.push(seat.has_phone ? '有电话端口' : '无电话端口')
  lines.push(seat.has_monitor ? `显示器 ${seat.asset_tag}` : '无显示器')
  if (seat.occupant_name) lines.push(`使用人：${seat.occupant_name}`)
  if (seat.active_ticket_no) lines.push(`预占单：${seat.active_ticket_no}`)
  return lines.join('\n')
}
</script>
