<template>
  <div class="page">
    <div class="card">
      <h2>楼层图</h2>
      <div class="floor-tabs">
        <button
          v-for="f in floors"
          :key="f.floor"
          :class="{ active: f.floor === current }"
          @click="current = f.floor; loadSeats()"
        >
          {{ f.floor }}F（可用 {{ f.available }} / 预占 {{ f.reserved }} / 占用 {{ f.occupied }}）
        </button>
      </div>
      <div v-if="error" class="alert error">{{ error }}</div>
      <SeatMap :seats="seats" @select="showSeat" />
    </div>

    <div v-if="selected" class="card">
      <h3>座位 {{ selected.code }}</h3>
      <div class="info-grid mt">
        <div><div class="k">状态</div><div class="v">{{ statusLabel(selected.status) }}</div></div>
        <div><div class="k">使用人</div><div class="v">{{ selected.occupant_name || '—' }}</div></div>
        <div><div class="k">预占单</div><div class="v">{{ selected.active_ticket_no || '—' }}</div></div>
        <div>
          <div class="k">网口</div>
          <div class="v">{{ selected.port_code }}
            <span :style="{ color: selected.port_ok ? '#16a34a' : '#dc2626' }">
              {{ selected.port_ok ? '正常' : '故障' }}
            </span>
          </div>
        </div>
        <div><div class="k">电话端口</div><div class="v">{{ selected.has_phone ? '有' : '无' }}</div></div>
        <div><div class="k">显示器</div><div class="v">{{ selected.has_monitor ? selected.asset_tag : '无' }}</div></div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { request } from '../api'
import SeatMap from '../components/SeatMap.vue'

const floors = ref([])
const current = ref(0)
const seats = ref([])
const selected = ref(null)
const error = ref('')

function statusLabel(s) {
  return { available: '可用', occupied: '已占用', reserved: '已预占' }[s] || s
}

async function loadFloors() {
  floors.value = await request('/api/floors')
  if (floors.value.length && !current.value) current.value = floors.value[0].floor
}
async function loadSeats() {
  error.value = ''
  selected.value = null
  try {
    seats.value = await request(`/api/floors/${current.value}/seats`)
  } catch (e) {
    error.value = e.message
  }
}
function showSeat(seat) {
  selected.value = seat
}
onMounted(async () => {
  try {
    await loadFloors()
    await loadSeats()
  } catch (e) {
    error.value = e.message
  }
})
</script>
