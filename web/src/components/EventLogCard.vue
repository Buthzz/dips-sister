<script setup>
import { computed, ref, watch, nextTick } from 'vue'

const props = defineProps({
  events: { type: Array, default: () => [] },
})

const emit = defineEmits(['clear'])

const activeFilter = ref('ALL')
const autoScroll = ref(true)
const logContainer = ref(null)

const filteredEvents = computed(() => {
  if (activeFilter.value === 'ALL') return props.events
  return props.events.filter((e) => e.level === activeFilter.value)
})

watch(
  () => props.events.length,
  async () => {
    if (autoScroll.value && logContainer.value) {
      await nextTick()
      logContainer.value.scrollTop = logContainer.value.scrollHeight
    }
  }
)
</script>

<template>
  <section class="card event-card">
    <div class="card-title">
      <div class="title-left">
        <span class="terminal-dot"></span>
        <span>Aktivitas Kluster (Live Event Stream)</span>
        <span class="summary">{{ events.length }} event tercatat</span>
      </div>

      <div class="toolbar">
        <div class="filters">
          <button
            class="filter-btn"
            :class="{ active: activeFilter === 'ALL' }"
            @click="activeFilter = 'ALL'"
          >
            Semua
          </button>
          <button
            class="filter-btn filter-btn--info"
            :class="{ active: activeFilter === 'INFO' }"
            @click="activeFilter = 'INFO'"
          >
            INFO
          </button>
          <button
            class="filter-btn filter-btn--warn"
            :class="{ active: activeFilter === 'WARN' }"
            @click="activeFilter = 'WARN'"
          >
            WARN
          </button>
          <button
            class="filter-btn filter-btn--error"
            :class="{ active: activeFilter === 'ERROR' }"
            @click="activeFilter = 'ERROR'"
          >
            ERROR
          </button>
        </div>

        <label class="autoscroll-toggle">
          <input type="checkbox" v-model="autoScroll" />
          <span>Auto-scroll</span>
        </label>
      </div>
    </div>

    <div ref="logContainer" class="terminal-box">
      <div v-if="filteredEvents.length === 0" class="terminal-empty">
        Belum ada aktivitas kluster yang sesuai filter.
      </div>
      <div
        v-for="(ev, idx) in filteredEvents"
        :key="idx"
        class="log-line"
        :class="'log--' + ev.level.toLowerCase()"
      >
        <span class="log-time">[{{ ev.time }}]</span>
        <span class="log-level">{{ ev.level }}</span>
        <span class="log-msg">{{ ev.message }}</span>
      </div>
    </div>
  </section>
</template>

<style scoped>
.event-card {
  margin-top: 1.2rem;
}

.title-left {
  display: flex;
  align-items: center;
  gap: 0.55rem;
}

.terminal-dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: #10b981;
  box-shadow: 0 0 6px rgba(16, 185, 129, 0.6);
}

.summary {
  font-size: 0.78rem;
  font-weight: 500;
  color: var(--muted);
}

.toolbar {
  display: flex;
  align-items: center;
  gap: 0.8rem;
  flex-wrap: wrap;
}

.filters {
  display: flex;
  gap: 0.25rem;
  background: var(--slate-bg);
  padding: 2px;
  border-radius: 6px;
}

.filter-btn {
  border: none;
  background: transparent;
  padding: 0.18rem 0.55rem;
  border-radius: 4px;
  font-size: 0.74rem;
  font-weight: 600;
  color: var(--muted);
  cursor: pointer;
  transition: all 0.15s ease;
}

.filter-btn.active {
  background: var(--surface);
  color: var(--text);
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.08);
}

.filter-btn--info.active {
  color: var(--primary);
}

.filter-btn--warn.active {
  color: var(--amber);
}

.filter-btn--error.active {
  color: var(--red);
}

.autoscroll-toggle {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  font-size: 0.76rem;
  color: var(--muted);
  cursor: pointer;
}

.terminal-box {
  background: #0f172a;
  border-radius: 8px;
  padding: 0.75rem 0.9rem;
  height: 200px;
  overflow-y: auto;
  font-family: Cascadia Code, Consolas, "Courier New", monospace;
  font-size: 0.78rem;
  line-height: 1.55;
  color: #e2e8f0;
  box-shadow: inset 0 2px 4px rgba(0, 0, 0, 0.3);
}

.terminal-empty {
  color: #64748b;
  font-style: italic;
  padding: 1.5rem 0;
  text-align: center;
}

.log-line {
  display: flex;
  align-items: flex-start;
  gap: 0.6rem;
  word-break: break-all;
}

.log-time {
  color: #64748b;
  flex: 0 0 auto;
}

.log-level {
  font-weight: 700;
  flex: 0 0 46px;
}

.log--info .log-level {
  color: #38bdf8;
}

.log--warn .log-level {
  color: #fbbf24;
}

.log--error .log-level {
  color: #f87171;
}

.log--info .log-msg {
  color: #e2e8f0;
}

.log--warn .log-msg {
  color: #fde68a;
}

.log--error .log-msg {
  color: #fca5a5;
}
</style>
