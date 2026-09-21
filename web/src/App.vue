<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { fetchHealth, fetchNodes, fetchJobs } from './api.js'
import UploadCard from './components/UploadCard.vue'
import NodesCard from './components/NodesCard.vue'
import JobsCard from './components/JobsCard.vue'

const nodes = ref([])
const jobs = ref([])
const masterOnline = ref(false)
const origin = window.location.origin

let timer = null

async function pullHealth() {
  try {
    masterOnline.value = await fetchHealth()
  } catch {
    masterOnline.value = false
  }
}

async function pullNodes() {
  try {
    nodes.value = await fetchNodes()
  } catch {
    nodes.value = []
  }
}

async function pullJobs() {
  try {
    jobs.value = await fetchJobs()
  } catch {
    /* daftar job lama dipertahankan ketika polling gagal sesaat */
  }
}

async function refreshAll() {
  await Promise.allSettled([pullHealth(), pullNodes(), pullJobs()])
}

async function handleJobsChanged() {
  await Promise.allSettled([pullJobs(), pullNodes()])
}

onMounted(() => {
  refreshAll()
  timer = setInterval(refreshAll, 2000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="page">
    <header class="topbar">
      <div class="brand">
        <h1>Distributed Image Processing</h1>
      </div>

      <div class="cluster" :class="{ off: !masterOnline }">
        <span class="dot"></span>
        <div class="cluster-text">
          <strong>{{ masterOnline ? 'Master aktif' : 'Master tidak terjangkau' }}</strong>
          <small>{{ nodes.filter((n) => n.status === 'alive').length }} dari {{ nodes.length }} node worker aktif</small>
        </div>
      </div>
    </header>

    <div v-if="!masterOnline" class="banner banner--error">
      Tidak dapat menghubungi master di {{ origin }}. Pastikan binary master berjalan pada port 8080.
    </div>

    <main class="layout">
      <aside class="col-left">
        <UploadCard @created="handleJobsChanged" />
      </aside>

      <section class="col-right">
        <NodesCard :nodes="nodes" @refresh="pullNodes" />
        <JobsCard :jobs="jobs" @delete="handleJobsChanged" />
      </section>
    </main>
  </div>
</template>

<style scoped>
.page {
  max-width: 1180px;
  margin: 0 auto;
  padding: 1.2rem 1rem 2.5rem;
}

.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  flex-wrap: wrap;
  margin-bottom: 1.1rem;
}

.brand h1 {
  font-size: 1.55rem;
  letter-spacing: -0.01em;
}

.cluster {
  display: inline-flex;
  align-items: center;
  gap: 0.6rem;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 999px;
  padding: 0.4rem 0.9rem;
  box-shadow: var(--shadow);
}

.cluster .dot {
  width: 11px;
  height: 11px;
  border-radius: 50%;
  background: var(--green);
  box-shadow: 0 0 0 3px var(--green-bg);
}

.cluster.off .dot {
  background: var(--red);
  box-shadow: 0 0 0 3px var(--red-bg);
}

.cluster-text {
  display: flex;
  flex-direction: column;
  line-height: 1.25;
}

.cluster-text strong {
  font-size: 0.85rem;
}

.cluster-text small {
  color: var(--muted);
  font-size: 0.75rem;
}

.banner--error {
  background: var(--red-bg);
  color: var(--red);
  border: 1px solid #fecaca;
  border-radius: 8px;
  padding: 0.6rem 0.9rem;
  font-size: 0.85rem;
  margin-bottom: 1rem;
}

.layout {
  display: grid;
  grid-template-columns: 380px 1fr;
  gap: 1rem;
  align-items: start;
}

@media (max-width: 900px) {
  .layout {
    grid-template-columns: 1fr;
  }
}

.col-left {
  position: sticky;
  top: 1rem;
}

.col-right {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
</style>