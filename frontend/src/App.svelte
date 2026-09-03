<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { ConnectGoogleDrive, CreateTask, DeleteTask, DisconnectGoogleDrive, GetDriveSyncStatus, GetState, GetTaskTimeSummary, LogTime, PauseTimer, ResumeTimer, SetTaskCompleted, StartTimer, StopTimer, SyncNow } from '../wailsjs/go/main/App'
  import { EventsOn, Quit, WindowMinimise, WindowToggleMaximise } from '../wailsjs/runtime/runtime'

  type Task = { id: string; title: string; completed: boolean }
  type Entry = { taskId: string; durationSeconds: number }
  type Timer = { taskId: string; taskTitle: string; startedAt: string; paused: boolean; sessionSeconds: number }
  type State = { tasks: Task[]; entries: Entry[]; activeTimer: Timer | null }
  type TimeSummary = { taskId: string; lastDaySeconds: number; lastWeekSeconds: number; lastMonthSeconds: number; allTimeSeconds: number }
  type DriveStatus = { connected: boolean; configured: boolean; state: string; message: string; lastSyncedAt: string }

  let state: State = { tasks: [], entries: [], activeTimer: null }
  let selectedTaskID = ''
  let newTitle = ''
  let addingTask = false
  let newTaskInput: HTMLInputElement
  let busy = true
  let error = ''
  let now = Date.now()
  let statsTask: Task | null = null
  let deleteTarget: Task | null = null
  let summary: TimeSummary | null = null
  let statsLoading = false
  let driveStatus: DriveStatus = { connected: false, configured: false, state: 'disconnected', message: 'Saved locally', lastSyncedAt: '' }
  let syncMenuOpen = false
  let syncBusy = false
  let menuX = 0
  let menuY = 0
  const initialStart = clockParts(new Date(Date.now() - 60 * 60 * 1000))
  const initialEnd = clockParts(new Date())
  let startHour = initialStart.hour
  let startMinute = initialStart.minute
  let startPeriod = initialStart.period
  let endHour = initialEnd.hour
  let endMinute = initialEnd.minute
  let endPeriod = initialEnd.period
  let openTimePicker: 'start' | 'end' | null = null
  let startHourInput: HTMLInputElement
  let startMinuteInput: HTMLInputElement
  let startPeriodInput: HTMLInputElement
  let endHourInput: HTMLInputElement
  let endMinuteInput: HTMLInputElement
  let endPeriodInput: HTMLInputElement
  const timeOptions = Array.from({ length: 48 }, (_, index) => formatClock(index * 30))

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
  $: driveLabel = driveStatus.state === 'syncing' ? '↻ SYNCING'
    : driveStatus.state === 'pending' ? '● SYNC PENDING'
    : driveStatus.state === 'error' ? '! SYNC ERROR'
    : driveStatus.connected ? '● DRIVE SYNCED' : '● SAVED LOCALLY'
  onMount(() => {
    void refresh()
    void refreshDriveStatus()
    const stopStatus = EventsOn('drive:status', (status: DriveStatus) => driveStatus = status)
    const stopData = EventsOn('drive:data-changed', () => void refresh())
    const ticker = window.setInterval(() => now = Date.now(), 1000)
    return () => { window.clearInterval(ticker); stopStatus(); stopData() }
  })

  async function refreshDriveStatus() {
    try {
      driveStatus = await GetDriveSyncStatus() as DriveStatus
    } catch {
      // Local timing remains available if sync status cannot be loaded.
    }
  }

  async function driveAction(action: 'connect' | 'sync' | 'disconnect') {
    try {
      syncBusy = true
      if (action === 'connect') driveStatus = await ConnectGoogleDrive() as DriveStatus
      else if (action === 'sync') driveStatus = await SyncNow() as DriveStatus
      else driveStatus = await DisconnectGoogleDrive() as DriveStatus
      if (action !== 'disconnect') await refresh()
      error = ''
    } catch (reason) {
      error = message(reason)
      await refreshDriveStatus()
    } finally {
      syncBusy = false
    }
  }

  function lastSyncText() {
    if (!driveStatus.lastSyncedAt) return 'NOT SYNCED YET'
    const date = new Date(driveStatus.lastSyncedAt)
    return Number.isNaN(date.getTime()) ? 'NOT SYNCED YET' : `LAST: ${date.toLocaleString([], { dateStyle: 'short', timeStyle: 'short' })}`
  }

  function dismissMenus(event: MouseEvent) {
    statsTask = null
    if (!(event.target instanceof Element) || !event.target.closest('.sync-area')) syncMenuOpen = false
  }

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

  async function beginAddingTask() {
    addingTask = true
    await tick()
    newTaskInput.focus()
    newTaskInput.select()
  }

  async function complete(task: Task) {
    try {
      await SetTaskCompleted(task.id, !task.completed)
      await refresh()
    } catch (reason) {
      error = message(reason)
    }
  }

  async function addLoggedTime() {
    if (!selectedTaskID) {
      error = 'Select an open task first'
      return
    }
    const startMinutes = parseParts(startHour, startMinute, startPeriod)
    const endMinutes = parseParts(endHour, endMinute, endPeriod)
    if (startMinutes === null || endMinutes === null || startMinutes === endMinutes) {
      error = 'Choose two different start and end times'
      return
    }
    try {
      busy = true
      setTimeParts('start', startMinutes)
      setTimeParts('end', endMinutes)
      const { start, end } = recentRange(startMinutes, endMinutes)
      await LogTime(selectedTaskID, start.toISOString(), end.toISOString())
      await refresh()
    } catch (reason) {
      error = message(reason)
      busy = false
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

  function clockParts(date: Date) {
    const hour = date.getHours()
    return {
      hour: String(hour % 12 || 12),
      minute: String(date.getMinutes()).padStart(2, '0'),
      period: hour >= 12 ? 'PM' : 'AM'
    }
  }

  function formatClock(totalMinutes: number) {
    const hours = Math.floor(totalMinutes / 60) % 24
    const minutes = totalMinutes % 60
    const suffix = hours >= 12 ? 'PM' : 'AM'
    return `${hours % 12 || 12}:${String(minutes).padStart(2, '0')} ${suffix}`
  }

  function parseParts(hourText: string, minuteText: string, periodText: string): number | null {
    const hour = Number(hourText)
    const minute = Number(minuteText)
    const period = periodText.trim().toUpperCase()
    if (!Number.isInteger(hour) || hour < 1 || hour > 12 || !Number.isInteger(minute) || minute < 0 || minute > 59 || !['AM', 'PM'].includes(period)) return null
    return (hour % 12 + (period === 'PM' ? 12 : 0)) * 60 + minute
  }

  function fieldMinutes(field: 'start' | 'end') {
    return field === 'start'
      ? parseParts(startHour, startMinute, startPeriod)
      : parseParts(endHour, endMinute, endPeriod)
  }

  function setTimeParts(field: 'start' | 'end', totalMinutes: number) {
    const hours = Math.floor(totalMinutes / 60) % 24
    const hour = String(hours % 12 || 12)
    const minute = String(totalMinutes % 60).padStart(2, '0')
    const period = hours >= 12 ? 'PM' : 'AM'
    if (field === 'start') {
      startHour = hour
      startMinute = minute
      startPeriod = period
    } else {
      endHour = hour
      endMinute = minute
      endPeriod = period
    }
  }

  function commitStart() {
    const minutes = fieldMinutes('start')
    if (minutes === null) {
      error = 'Enter a valid start time'
      return
    }
    setTimeParts('start', minutes)
    openTimePicker = 'end'
    window.setTimeout(() => { endHourInput.focus(); endHourInput.select() })
  }

  function typeNumber(field: 'start' | 'end', part: 'hour' | 'minute', event: Event) {
    const input = event.currentTarget as HTMLInputElement
    const value = input.value.replace(/\D/g, '').slice(0, 2)
    if (field === 'start') {
      if (part === 'hour') startHour = value
      else startMinute = value
    } else {
      if (part === 'hour') endHour = value
      else endMinute = value
    }
    input.value = value
    if (part === 'hour' && (value.length === 2 || /^[2-9]$/.test(value))) focusPart(field, 'minute')
    if (part === 'minute' && value.length === 2) focusPart(field, 'period')
  }

  function typePeriod(field: 'start' | 'end', event: Event) {
    const input = event.currentTarget as HTMLInputElement
    const value = input.value.replace(/[^apm]/gi, '').toUpperCase().slice(0, 2)
    if (field === 'start') startPeriod = value
    else endPeriod = value
    input.value = value
    if (field === 'start' && /^(AM|PM)$/.test(value) && fieldMinutes('start') !== null) commitStart()
  }

  function focusPart(field: 'start' | 'end', part: 'hour' | 'minute' | 'period') {
    const input = field === 'start'
      ? part === 'hour' ? startHourInput : part === 'minute' ? startMinuteInput : startPeriodInput
      : part === 'hour' ? endHourInput : part === 'minute' ? endMinuteInput : endPeriodInput
    input.focus()
    input.select()
  }

  function activatePart(field: 'start' | 'end', event: Event) {
    openTimePicker = field
    const input = event.currentTarget as HTMLInputElement
    window.setTimeout(() => input.select())
  }

  function timeKeydown(field: 'start' | 'end', part: 'hour' | 'minute' | 'period', event: KeyboardEvent) {
    if (event.key === 'Enter') {
      event.preventDefault()
      if (field === 'start') commitStart()
      else commitEnd(true)
    } else if (part === 'hour' && [':', '.', ' '].includes(event.key)) {
      event.preventDefault()
      focusPart(field, 'minute')
    } else if (part === 'minute' && ['a', 'A', 'p', 'P'].includes(event.key)) {
      event.preventDefault()
      if (field === 'start') startPeriod = `${event.key.toUpperCase()}M`
      else endPeriod = `${event.key.toUpperCase()}M`
      if (field === 'start' && fieldMinutes('start') !== null) commitStart()
      else focusPart(field, 'period')
    }
  }

  function commitEnd(submit = false) {
    const minutes = fieldMinutes('end')
    if (minutes === null) {
      error = 'Enter a valid end time'
      return
    }
    setTimeParts('end', minutes)
    openTimePicker = null
    if (submit) void addLoggedTime()
  }

  function chooseTime(field: 'start' | 'end', value: string) {
    const [clock, period] = value.split(' ')
    const [hour, minute] = clock.split(':').map(Number)
    const minutes = (hour % 12 + (period === 'PM' ? 12 : 0)) * 60 + minute
    setTimeParts(field, minutes)
    if (field === 'start') {
      commitStart()
    } else {
      openTimePicker = null
      endHourInput.focus()
    }
  }

  function normalizeTime(field: 'start' | 'end') {
    window.setTimeout(() => {
      const focused = document.activeElement
      const inputs = field === 'start'
        ? [startHourInput, startMinuteInput, startPeriodInput]
        : [endHourInput, endMinuteInput, endPeriodInput]
      if (inputs.includes(focused as HTMLInputElement)) return
      const minutes = fieldMinutes(field)
      if (minutes !== null) setTimeParts(field, minutes)
      if (openTimePicker === field) openTimePicker = null
    }, 120)
  }

  function recentRange(startMinutes: number, endMinutes: number) {
    const now = new Date()
    const end = new Date(now)
    end.setHours(Math.floor(endMinutes / 60), endMinutes % 60, 0, 0)
    if (end.getTime() > now.getTime()) end.setDate(end.getDate() - 1)

    const start = new Date(end)
    start.setHours(Math.floor(startMinutes / 60), startMinutes % 60, 0, 0)
    if (start.getTime() >= end.getTime()) start.setDate(start.getDate() - 1)
    return { start, end }
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

<svelte:window on:click={dismissMenus} on:keydown={(event) => {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'n') { event.preventDefault(); void beginAddingTask() }
  if (event.code === 'Space' && event.target === document.body) { event.preventDefault(); void toggleTimer() }
  if (event.key === 'Escape') { statsTask = null; deleteTarget = null; syncMenuOpen = false }
}} />

<main class="app-window">
  <header class="titlebar drag-region">
    <span class="app-mark">⌛</span><strong>PUNCHCARD</strong>
    <div class="sync-area no-drag">
      <button class:error-state={driveStatus.state === 'error'} class:syncing={driveStatus.state === 'syncing'} class="saved" title={driveStatus.message} on:click|stopPropagation={() => syncMenuOpen = !syncMenuOpen}>{driveLabel}</button>
      {#if syncMenuOpen}
        <div class="sync-menu">
          <div class="sync-menu-title"><strong>GOOGLE DRIVE</strong><button aria-label="Close sync menu" on:click={() => syncMenuOpen = false}>×</button></div>
          <div class="sync-menu-body">
            <span class:status-error={driveStatus.state === 'error'} class="sync-message">{driveStatus.message}</span>
            <small>{lastSyncText()}</small>
            {#if driveStatus.connected}
              <button disabled={syncBusy || driveStatus.state === 'syncing'} on:click={() => driveAction('sync')}>↻ SYNC NOW</button>
              <button class="disconnect" disabled={syncBusy} on:click={() => driveAction('disconnect')}>DISCONNECT</button>
            {:else}
              <button disabled={syncBusy} on:click={() => driveAction('connect')}>CONNECT DRIVE</button>
              <small>FIRST USE ASKS FOR GOOGLE DESKTOP OAUTH JSON.</small>
            {/if}
          </div>
        </div>
      {/if}
    </div>
    <div class="window-controls no-drag">
      <button aria-label="Minimize" on:click={WindowMinimise}>—</button>
      <button aria-label="Maximize" on:click={WindowToggleMaximise}>□</button>
      <button aria-label="Quit" on:click={Quit}>×</button>
    </div>
  </header>

  <div class="workspace">
    <section class="timer-pane">
      <div class:live={state.activeTimer && !state.activeTimer.paused} class:paused={state.activeTimer?.paused} class="timer-card">
        <div class="panel-titlebar" aria-hidden="true"><strong>TIMER</strong><span class="panel-window-controls"><b>—</b><b>□</b><b>×</b></span></div>
        <div class="timer-content">
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
      </div>

      <section class="log-window">
        <div class="panel-titlebar" aria-hidden="true"><strong>LOG TIME</strong><span class="panel-window-controls"><b>—</b><b>□</b><b>×</b></span></div>
        <div class="options">
          <div class="time-field">
          <label for="log-start-hour">START</label>
          <div class="time-input" role="group" aria-label="Start time">
            <input class="time-hour" id="log-start-hour" bind:this={startHourInput} value={startHour} inputmode="numeric" autocomplete="off" maxlength="2" aria-label="Start hour" on:input={(event) => typeNumber('start', 'hour', event)} on:focus={(event) => activatePart('start', event)} on:click={(event) => activatePart('start', event)} on:blur={() => normalizeTime('start')} on:keydown={(event) => timeKeydown('start', 'hour', event)} />
            <span class="time-separator" aria-hidden="true">:</span>
            <input class="time-minute" bind:this={startMinuteInput} value={startMinute} inputmode="numeric" autocomplete="off" maxlength="2" aria-label="Start minute" on:input={(event) => typeNumber('start', 'minute', event)} on:focus={(event) => activatePart('start', event)} on:click={(event) => activatePart('start', event)} on:blur={() => normalizeTime('start')} on:keydown={(event) => timeKeydown('start', 'minute', event)} />
            <input class="time-period" bind:this={startPeriodInput} value={startPeriod} inputmode="text" autocomplete="off" maxlength="2" aria-label="Start AM or PM" on:input={(event) => typePeriod('start', event)} on:focus={(event) => activatePart('start', event)} on:click={(event) => activatePart('start', event)} on:blur={() => normalizeTime('start')} on:keydown={(event) => timeKeydown('start', 'period', event)} />
            <button class="time-arrow" type="button" tabindex="-1" aria-label="Show start time choices" aria-expanded={openTimePicker === 'start'} on:mousedown|preventDefault on:click={() => openTimePicker = openTimePicker === 'start' ? null : 'start'}>▾</button>
          </div>
          {#if openTimePicker === 'start'}
            <div class="time-dropdown" role="listbox" aria-label="Start time">
              {#each timeOptions as option}<button type="button" class:selected-time={option === formatClock(fieldMinutes('start') ?? -1)} on:mousedown|preventDefault on:click={() => chooseTime('start', option)}>{option}</button>{/each}
            </div>
          {/if}
          </div>
          <div class="time-field">
          <label for="log-end-hour">END</label>
          <div class="time-input" role="group" aria-label="End time">
            <input class="time-hour" id="log-end-hour" bind:this={endHourInput} value={endHour} inputmode="numeric" autocomplete="off" maxlength="2" aria-label="End hour" on:input={(event) => typeNumber('end', 'hour', event)} on:focus={(event) => activatePart('end', event)} on:click={(event) => activatePart('end', event)} on:blur={() => normalizeTime('end')} on:keydown={(event) => timeKeydown('end', 'hour', event)} />
            <span class="time-separator" aria-hidden="true">:</span>
            <input class="time-minute" bind:this={endMinuteInput} value={endMinute} inputmode="numeric" autocomplete="off" maxlength="2" aria-label="End minute" on:input={(event) => typeNumber('end', 'minute', event)} on:focus={(event) => activatePart('end', event)} on:click={(event) => activatePart('end', event)} on:blur={() => normalizeTime('end')} on:keydown={(event) => timeKeydown('end', 'minute', event)} />
            <input class="time-period" bind:this={endPeriodInput} value={endPeriod} inputmode="text" autocomplete="off" maxlength="2" aria-label="End AM or PM" on:input={(event) => typePeriod('end', event)} on:focus={(event) => activatePart('end', event)} on:click={(event) => activatePart('end', event)} on:blur={() => normalizeTime('end')} on:keydown={(event) => timeKeydown('end', 'period', event)} />
            <button class="time-arrow" type="button" tabindex="-1" aria-label="Show end time choices" aria-expanded={openTimePicker === 'end'} on:mousedown|preventDefault on:click={() => openTimePicker = openTimePicker === 'end' ? null : 'end'}>▾</button>
          </div>
          {#if openTimePicker === 'end'}
            <div class="time-dropdown" role="listbox" aria-label="End time">
              {#each timeOptions as option}<button type="button" class:selected-time={option === formatClock(fieldMinutes('end') ?? -1)} on:mousedown|preventDefault on:click={() => chooseTime('end', option)}>{option}</button>{/each}
            </div>
          {/if}
          </div>
          <button class="log-add" disabled={busy || !selectedTaskID} on:click={addLoggedTime}>＋ ADD</button>
        </div>
      </section>

      {#if error}<div class="error" role="alert">{error}<button on:click={() => error = ''}>×</button></div>{/if}
    </section>

    <aside class="task-sidebar">
      <div class="sidebar-title panel-titlebar"><strong>ALL TASKS</strong><span class="task-count">{openTasks.length} OPEN</span><span class="panel-window-controls" aria-hidden="true"><b>—</b><b>□</b><b>×</b></span></div>

      {#if addingTask}
        <form class="new-task" on:submit|preventDefault={addTask}>
          <input bind:this={newTaskInput} bind:value={newTitle} aria-label="New task title" placeholder="New task…" maxlength="120" />
          <button type="submit" disabled={!newTitle.trim() || busy}>ADD</button>
          <button type="button" aria-label="Cancel" on:click={() => addingTask = false}>×</button>
        </form>
      {:else}
        <button class="add-task" on:click={beginAddingTask}>＋ ADD A TASK</button>
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
      <div class="confirm-title panel-titlebar"><strong id="delete-title">DELETE TASK?</strong><span class="panel-window-controls" aria-hidden="true"><b>—</b><b>□</b><b>×</b></span></div>
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
