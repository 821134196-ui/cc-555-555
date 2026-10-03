<template>
  <div class="page">
    <div class="card">
      <h2>新建搬迁单</h2>
      <div v-if="error" class="alert error">{{ error }}</div>
      <div class="row">
        <div class="field">
          <label>搬迁员工</label>
          <select v-model="form.employee_id">
            <option :value="0" disabled>请选择员工</option>
            <option v-for="u in employees" :key="u.id" :value="u.id">
              {{ u.name }}（当前座位 {{ u.current_seat_code || '无' }}，分机 {{ u.extension }}）
            </option>
          </select>
        </div>
        <div class="field">
          <label>搬迁日期</label>
          <input type="date" v-model="form.move_date" />
        </div>
      </div>
      <div class="field">
        <label>备注</label>
        <textarea v-model="form.note" rows="2" placeholder="选填"></textarea>
      </div>
    </div>

    <div class="card">
      <h3>选择目标座位（当前：{{ selectedSeat ? selectedSeat.code : '未选择' }}）</h3>
      <div class="floor-tabs">
        <button
          v-for="f in floors"
          :key="f.floor"
          :class="{ active: f.floor === currentFloor }"
          @click="currentFloor = f.floor; loadSeats()"
        >{{ f.floor }}F</button>
      </div>
      <SeatMap
        :seats="seats"
        selectable
        :selected-id="selectedSeat?.id"
        @select="selectedSeat = $event"
      />
    </div>

    <div>
      <button class="btn primary" :disabled="submitting" @click="submit">提交搬迁单</button>
      <router-link to="/" class="btn" style="margin-left:10px">返回</router-link>
    </div>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { request } from '../api'
import SeatMap from '../components/SeatMap.vue'

const router = useRouter()
const employees = ref([])
const floors = ref([])
const currentFloor = ref(0)
const seats = ref([])
const selectedSeat = ref(null)
const error = ref('')
const submitting = ref(false)

const form = reactive({ employee_id: 0, move_date: '', note: '' })

async function loadSeats() {
  seats.value = await request(`/api/floors/${currentFloor.value}/seats`)
}

onMounted(async () => {
  try {
    employees.value = (await request('/api/users?role=employee')).filter(u => u.current_seat_id)
    floors.value = await request('/api/floors')
    if (floors.value.length) {
      currentFloor.value = floors.value[0].floor
      await loadSeats()
    }
  } catch (e) {
    error.value = e.message
  }
})

async function submit() {
  error.value = ''
  if (!form.employee_id) { error.value = '请选择搬迁员工'; return }
  if (!selectedSeat.value) { error.value = '请选择目标座位'; return }
  if (!form.move_date) { error.value = '请选择搬迁日期'; return }
  submitting.value = true
  try {
    const t = await request('/api/tickets', {
      method: 'POST',
      body: {
        employee_id: form.employee_id,
        to_seat_id: selectedSeat.value.id,
        move_date: form.move_date,
        note: form.note,
      },
    })
    router.push(`/tickets/${t.id}`)
  } catch (e) {
    error.value = e.message
    loadSeats() // 冲突时刷新座位状态
  } finally {
    submitting.value = false
  }
}
</script>
