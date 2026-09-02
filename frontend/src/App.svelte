<script lang="ts">
  import { onMount } from 'svelte'
  import { CreateTask, DeleteTask, ExportBackup, GetState, GetTaskTimeSummary, ImportBackup, PauseTimer, ResumeTimer, SetTaskCompleted, StartTimer, StopTimer } from '../wailsjs/go/main/App'
  import { Quit, WindowMinimise, WindowToggleMaximise } from '../wailsjs/runtime/runtime'

  type Task = { id: string; title: string; completed: boolean }
  type Entry = { taskId: string; durationSeconds: number }
  type Timer = { taskId: string; taskTitle: string; startedAt: string; paused: boolean; sessionSeconds: number }
  type State = { tasks: Task[]; entries: Entry[]; activeTimer: Timer | null }
  type TimeSummary = { taskId: string; lastDaySeconds: number; lastWeekSeconds: number; lastMonthSeconds: number; allTimeSeconds: number }

  let state: State = { tasks: [], entries: [], activeTimer: null }
  let selectedTaskID = ''
  let newTitle = ''
  let addingTask = false
  let busy = true
  let error = ''
  let now = Date.now()
  let statsTask: Task | null = null
  let deleteTarget: Task | null = null
  let summary: TimeSummary | null = null
  let statsLoading = false
  let menuX = 0
  let menuY = 0

  $: openTasks = state.tasks.filter(task => !task.completed)
  $: selectedTask = state.tasks.find(task => task.id === selectedTaskID)
  $: activeSegment = state.activeTimer && !state.activeTimer.paused
    ? Math.max(0, Math.floor((now - new Date(state.activeTimer.startedAt).getTime()) / 1000))
    : 0
  $: elapsed = state.activeTimer ? state.activeTimer.sessionSeconds + activeSegment : 0
  $: totals = state.entries.reduce((sum, entry) => {
    sum[entry.taskId] = (sum[entry.taskId] || 0) + entry.durationSeconds
    return sum
  }, {} as Record<string, number>)
  onMount(() => {
    void refresh()
    const ticker = window.setInterval(() => now = Date.now(), 1000)
    return () => window.clearInterval(ticker)
  })

  async function refresh() {
    try {
      busy = true
      state = await GetState() as State
      if (state.activeTimer) selectedTaskID = state.activeTimer.taskId
      else if (!state.tasks.some(task => task.id === selectedTaskID && !task.completed)) selectedTaskID = state.tasks.find(task => !task.completed)?.id || ''
      error = ''
    } catch (reason) {
      error = message(reason)
    } finally {
      busy = false
    }
  }

  async function toggleTimer() {
    if (!selectedTaskID && !state.activeTimer) return
    try {
      busy = true
      if (state.activeTimer) await StopTimer('')
      else await StartTimer(selectedTaskID)
      await refresh()
    } catch (reason) {
      error = message(reason)
      busy = false
    }
  }

  async function togglePause() {
    if (!state.activeTimer) return
    try {
      busy = true
      if (state.activeTimer.paused) await ResumeTimer()
      else await PauseTimer()
      await refresh()
    } catch (reason) {
      error = message(reason)
      busy = false
    }
  }

  async function addTask() {
    if (!newTitle.trim()) return
    try {
      busy = true
      const task = await CreateTask({ title: newTitle.trim() }) as Task
      selectedTaskID = task.id
      newTitle = ''
      addingTask = false
      await refresh()
    } catch (reason) {
      error = message(reason)
      busy = false
    }
  }

  async function complete(task: Task) {
    try {
      await SetTaskCompleted(task.id, !task.completed)
      await refresh()
    } catch (reason) {
      error = message(reason)
    }
  }

  async function backup(kind: 'export' | 'import') {
    try {
      const path = kind === 'export' ? await ExportBackup() : await ImportBackup()
      if (path && kind === 'import') await refresh()
    } catch (reason) {
      error = message(reason)
    }
  }

  async function showTaskMenu(event: MouseEvent, task: Task) {
    menuX = Math.min(event.clientX, window.innerWidth - 205)
    menuY = Math.min(event.clientY, window.innerHeight - (task.completed ? 215 : 174))
    statsTask = task
    summary = null
    statsLoading = true
    try {
      const result = await GetTaskTimeSummary(task.id) as TimeSummary
      if (statsTask?.id === task.id) summary = result
    } catch (reason) {
      error = message(reason)
      statsTask = null
    } finally {
      statsLoading = false
    }
  }

  function requestDelete() {
    const task = statsTask
    if (!task) return
    deleteTarget = task
    statsTask = null
  }

  async function confirmDelete() {
    const task = deleteTarget
    if (!task) return
    try {
      busy = true
      await DeleteTask(task.id)
      deleteTarget = null
      await refresh()
    } catch (reason) {
      error = message(reason)
      busy = false
    }
  }

  function formatTime(seconds: number) {
    const safe = Math.max(0, Math.floor(seconds))
    return [Math.floor(safe / 3600), Math.floor((safe % 3600) / 60), safe % 60]
      .map(value => String(value).padStart(2, '0')).join(':')
  }

  function compactTime(seconds: number) {
    const hours = Math.floor(seconds / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)
    return hours ? `${hours}h ${minutes}m` : `${minutes}m`
  }

  function message(reason: unknown) {
    return reason instanceof Error ? reason.message : String(reason || 'Something went wrong')
  }
</script>

<svelte:window on:click={() => statsTask = null} on:keydown={(event) => {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'n') addingTask = true
  if (event.code === 'Space' && event.target === document.body) { event.preventDefault(); void toggleTimer() }
  if (event.key === 'Escape') { statsTask = null; deleteTarget = null }
}} />

<main class="app-window">
  <header class="titlebar drag-region">
    <span class="app-mark">⌛</span><strong>PUNCHCARD</strong>
    <span class="saved">● SAVED LOCALLY</span>
    <div class="window-controls no-drag">
      <button aria-label="Minimize" on:click={WindowMinimise}>—</button>
      <button aria-label="Maximize" on:click={WindowToggleMaximise}>□</button>
      <button aria-label="Quit" on:click={Quit}>×</button>
    </div>
  </header>

  <div class="workspace">
    <section class="timer-pane">
      <div class:live={state.activeTimer && !state.activeTimer.paused} class:paused={state.activeTimer?.paused} class="timer-card">
        <span class="kicker">{state.activeTimer ? state.activeTimer.paused ? 'Ⅱ PAUSED' : '● ON THE CLOCK' : 'READY TO WORK'}</span>
        <h1>{state.activeTimer?.taskTitle || selectedTask?.title || 'Select a task'}</h1>
        <div class="timer">{formatTime(elapsed)}</div>
        {#if state.activeTimer}
          <div class="timer-actions">
            <button class="pause-button" disabled={busy} on:click={togglePause}>{state.activeTimer.paused ? '▶ RESUME' : 'Ⅱ PAUSE'}</button>
            <button class="punch-button stop" disabled={busy} on:click={toggleTimer}>■ PUNCH OUT</button>
          </div>
        {:else}
          <button class="punch-button" disabled={busy || !selectedTaskID} on:click={toggleTimer}>▶ PUNCH IN</button>
        {/if}
      </div>

      <div class="options">
        <label>
          <span>TASK</span>
          <select bind:value={selectedTaskID} disabled={openTasks.length === 0}>
            {#if openTasks.length === 0}<option value="">No open tasks</option>{/if}
            {#each openTasks as task}<option value={task.id}>{task.title}</option>{/each}
          </select>
        </label>
        <button on:click={() => addingTask = true}>＋ NEW</button>
        <button on:click={() => backup('export')}>EXPORT</button>
        <button on:click={() => backup('import')}>IMPORT</button>
      </div>

      {#if error}<div class="error" role="alert">{error}<button on:click={() => error = ''}>×</button></div>{/if}
    </section>

    <aside class="task-sidebar">
      <div class="sidebar-title"><strong>ALL TASKS</strong><span>{openTasks.length} OPEN</span></div>

      {#if addingTask}
        <form class="new-task" on:submit|preventDefault={addTask}>
          <input bind:value={newTitle} aria-label="New task title" placeholder="New task…" maxlength="120" />
          <button type="submit" disabled={!newTitle.trim() || busy}>ADD</button>
          <button type="button" aria-label="Cancel" on:click={() => addingTask = false}>×</button>
        </form>
      {:else}
        <button class="add-task" on:click={() => addingTask = true}>＋ ADD A TASK</button>
      {/if}

      <div class="task-list">
        {#if state.tasks.length === 0}
          <p class="empty">No tasks yet.<br />Add one to begin.</p>
        {/if}
        {#each state.tasks as task}
          <div class:selected={selectedTaskID === task.id} class:completed={task.completed} class:active={state.activeTimer?.taskId === task.id} class="task-row">
            <button class="check" aria-label={task.completed ? `Reopen ${task.title}` : `Complete ${task.title}`} on:click={() => complete(task)}>{task.completed ? '✓' : ''}</button>
            <button class="task-name" title="Right-click for time summary" on:click={() => { if (!task.completed) selectedTaskID = task.id }} on:contextmenu|preventDefault={(event) => showTaskMenu(event, task)}>
              <strong>{task.title}</strong><span>{compactTime((totals[task.id] || 0) + (state.activeTimer?.taskId === task.id ? activeSegment : 0))}</span>
            </button>
          </div>
        {/each}
      </div>
    </aside>
  </div>
</main>

{#if statsTask}
  <div class="task-menu" style={`left:${menuX}px; top:${menuY}px`}>
    <div class="task-menu-title"><strong>{statsTask.title}</strong><span>TIME LOG</span></div>
    <div><span>LAST 24 HOURS</span><b>{statsLoading ? '...' : compactTime(summary?.lastDaySeconds || 0)}</b></div>
    <div><span>LAST 7 DAYS</span><b>{statsLoading ? '...' : compactTime(summary?.lastWeekSeconds || 0)}</b></div>
    <div><span>LAST 30 DAYS</span><b>{statsLoading ? '...' : compactTime(summary?.lastMonthSeconds || 0)}</b></div>
    <div class="all-time"><span>ALL TIME</span><b>{statsLoading ? '...' : compactTime(summary?.allTimeSeconds || 0)}</b></div>
    {#if statsTask.completed}<button class="delete-task" on:click|stopPropagation={requestDelete}>DELETE TASK</button>{/if}
  </div>
{/if}

{#if deleteTarget}
  <div class="modal-layer">
    <div class="confirm-dialog" role="alertdialog" aria-modal="true" aria-labelledby="delete-title" aria-describedby="delete-message">
      <div class="confirm-title"><span aria-hidden="true">!</span><strong id="delete-title">DELETE TASK?</strong><i></i></div>
      <div class="confirm-body">
        <div class="trash-icon" aria-hidden="true">×</div>
        <div><strong>{deleteTarget.title}</strong><p id="delete-message">This permanently removes the task and all of its recorded time.</p></div>
      </div>
      <div class="confirm-actions">
        <button on:click={() => deleteTarget = null}>CANCEL</button>
        <button class="confirm-delete" disabled={busy} on:click={confirmDelete}>DELETE</button>
      </div>
    </div>
  </div>
{/if}
