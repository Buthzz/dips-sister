<script setup>
import { computed } from 'vue'
import { statusBadgeClass } from '../api.js'
import { formatTime } from '../format.js'

const props = defineProps({
  nodes: { type: Array, default: () => [] },
})

const emit = defineEmits(['refresh'])

const alive = computed(() => props.nodes.filter((n) => n.status === 'alive').length)
</script>

<template>
  <section class="card">
    <h2 class="card-title">
      <span class="title-left">Status Node</span>
      <span class="summary">{{ alive }} aktif dari {{ nodes.length }} terdaftar</span>
      <button class="btn" @click="emit('refresh')">Refresh</button>
    </h2>

    <div v-if="nodes.length === 0" class="empty-note">
      Belum ada node yang mendaftar. Nyalakan laptop worker (mode node) untuk bergabung ke kluster.
    </div>

    <div v-else-if="nodes.length > 0 && alive === 0" class="alert alert--warn">
      Tidak ada node node worker aktif — semua task akan dieksekusi oleh master (graceful degradation).
    </div>

    <div v-else>
      <table class="table">
        <thead>
          <tr>
            <th>Node ID</th>
            <th>Alamat</th>
            <th>Status</th>
            <th>Task Aktif</th>
            <th>Kapasitas</th>
            <th>Heartbeat Terakhir</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="n in nodes" :key="n.node_id">
            <td><code>{{ n.node_id }}</code></td>
            <td><code>{{ n.addr }}</code></td>
            <td><span class="badge" :class="statusBadgeClass(n.status)">{{ n.status }}</span></td>
            <td>{{ n.active_tasks }}</td>
            <td>{{ n.capacity }}</td>
            <td>{{ formatTime(n.last_heartbeat) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.summary {
  font-size: 0.78rem;
  font-weight: 500;
  color: var(--muted);
}
</style>