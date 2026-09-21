<script setup>
import { ref } from 'vue'
import { fetchJobDetail, deleteJob, resultUrl, statusBadgeClass } from '../api.js'
import { formatDateTime } from '../format.js'

const props = defineProps({
  jobs: { type: Array, default: () => [] },
})

const emit = defineEmits(['delete', 'changed'])

const expandedId = ref(null)
const detail = ref(null)
const loadingDetail = ref(false)

function persen(job) {
  return job.total > 0 ? Math.round((job.done / job.total) * 100) : 0
}

async function toggle(id) {
  if (expandedId.value === id) {
    expandedId.value = null
    detail.value = null
    return
  }
  expandedId.value = id
  detail.value = null
  loadingDetail.value = true
  try {
    detail.value = await fetchJobDetail(id)
  } finally {
    loadingDetail.value = false
  }
}

async function hapus(id) {
  if (!window.confirm(`Hapus job ${id} beserta seluruh berkasnya?`)) return
  try {
    await deleteJob(id)
    if (expandedId.value === id) {
      expandedId.value = null
      detail.value = null
    }
    emit('delete')
  } catch {
    /* error dibiarkan, daftar akan disegarkan saat polling berikutnya */
  }
}
</script>

<template>
  <section class="card">
    <h2 class="card-title">
      <span class="title-left">Daftar Job</span>
      <span v-if="jobs.length" class="summary">{{ jobs.length }} job</span>
    </h2>

    <div v-if="jobs.length === 0" class="empty-note">
      Belum ada job. Upload gambar untuk memulai pemrosesan terdistribusi.
    </div>

    <div v-else class="job-list">
      <div v-for="job in jobs" :key="job.id" class="job" :class="{ 'job--open': expandedId === job.id }">
        <div class="job-head" @click="toggle(job.id)">
          <code class="job-id">{{ job.id }}</code>
          <span class="badge" :class="statusBadgeClass(job.status)">{{ job.status }}</span>
          <div class="job-progress">
            <div class="bar">
              <div class="bar-fill" :class="'bar-fill--' + (job.status === 'FAILED' ? 'red' : 'green')" :style="{ width: persen(job) + '%' }"></div>
            </div>
            <span>{{ job.done }}/{{ job.total }} selesai</span>
          </div>
          <span v-if="job.failed" class="job-failed">{{ job.failed }} gagal</span>
          <span class="job-date">{{ formatDateTime(job.created_at) }}</span>
          <button class="btn btn--danger" title="Hapus job" @click.stop="hapus(job.id)">Hapus</button>
        </div>

        <div v-if="expandedId === job.id" class="job-detail">
          <p v-if="loadingDetail" class="empty-note">Memuat rincian task...</p>
          <template v-else-if="detail">
            <table class="table" v-if="detail.tasks && detail.tasks.length">
              <thead>
                <tr>
                  <th>Task</th>
                  <th>Berkas</th>
                  <th>Status</th>
                  <th>Node</th>
                  <th>Retry</th>
                  <th>Durasi</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="t in detail.tasks" :key="t.id">
                  <td><code>{{ t.id.slice(-3) }}</code></td>
                  <td>
                    <span class="task-name">{{ t.filename }}</span>
                    <span v-if="t.error" class="task-error" :title="t.error">(!)</span>
                  </td>
                  <td><span class="badge" :class="statusBadgeClass(t.status)">{{ t.status }}</span></td>
                  <td><code>{{ t.assigned_to || '-' }}</code></td>
                  <td>{{ t.retries }}</td>
                  <td>{{ t.duration_ms ? t.duration_ms + ' ms' : '-' }}</td>
                  <td>
                    <a
                      v-if="t.status === 'DONE'"
                      class="btn btn--ghost"
                      :href="resultUrl(detail.id, t.filename)"
                      :download="t.filename"
                      @click.stop
                    >
                      Unduh
                    </a>
                    <span v-else>-</span>
                  </td>
                </tr>
              </tbody>
            </table>
            <p v-else class="empty-note">Tidak ada task.</p>
          </template>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.summary {
  font-size: 0.78rem;
  font-weight: 500;
  color: var(--muted);
}

.job-list {
  display: flex;
  flex-direction: column;
  gap: 0.55rem;
}

.job {
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
}

.job--open {
  border-color: var(--primary);
}

.job-head {
  display: flex;
  align-items: center;
  gap: 0.65rem;
  padding: 0.5rem 0.7rem;
  cursor: pointer;
  flex-wrap: wrap;
  background: var(--surface);
}

.job-head:hover {
  background: var(--primary-weak);
}

.job-id {
  flex: 0 0 auto;
  max-width: 230px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.job-progress {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex: 1 1 180px;
  font-size: 0.78rem;
  color: var(--muted);
  min-width: 150px;
}

.bar {
  flex: 1;
  height: 6px;
  background: var(--slate-bg);
  border-radius: 999px;
  overflow: hidden;
}

.bar-fill {
  height: 100%;
  border-radius: 999px;
  transition: width 0.4s ease;
}

.bar-fill--green {
  background: var(--green);
}

.bar-fill--red {
  background: var(--red);
}

.job-failed {
  font-size: 0.78rem;
  color: var(--red);
}

.job-date {
  font-size: 0.75rem;
  color: var(--muted);
}

.job-detail {
  border-top: 1px solid var(--border);
  padding: 0.6rem 0.7rem 0.3rem;
}

.task-name {
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: inline-block;
  vertical-align: bottom;
}

.task-error {
  color: var(--red);
  font-weight: 700;
  cursor: help;
}
</style>