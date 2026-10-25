import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { lineText } from '@/lib/logs'
import type { LogEntry, LogsDoc } from '@/lib/types'
import { makeState } from '@/test/fixtures'
import { explain, renderWithApp, stubFetch } from '@/test/render'
import { LogsPanel } from './logs'

afterEach(() => vi.unstubAllGlobals())

const at = (s: number) => new Date(Date.UTC(2026, 9, 19, 18, 30, s, 123)).toISOString()
const line = (seq: number, level: LogEntry['level'], message: string, attrs?: LogEntry['attrs']): LogEntry => ({ seq, time: at(seq), level, message, attrs })
const lines = [
  line(1, 'info', 'starting', [{ key: 'version', value: '2026.10.8' }]),
  line(2, 'info', 'tuner online', [{ key: 'device', value: 'antenna' }]),
  line(3, 'warn', 'no viewers configured'),
  line(4, 'error', "couldn't read the guide", [{ key: 'err', value: 'Get "https://x": boom' }]),
]
const doc = (entries = lines, over: Partial<LogsDoc> = {}): LogsDoc => ({ entries, latest: entries.length, kept: entries.length, capacity: 2000, level: 'info', ...over })

function show(d: LogsDoc | { status: number; error: string } = doc(), state = makeState(), extra: Record<string, (b: unknown) => { status?: number; body?: unknown }> = {}) {
  // The log is asked for with a query (?limit=500&level=warn&q=…): any of them gets the same answer.
  const read = () => ('status' in d ? { status: d.status, body: { error: d.error } } : { body: d })
  const routes = new Proxy(extra, { get: (t, k) => (typeof k === 'string' && k.startsWith('GET /logs') ? read : t[k as string]) })
  const { fn, calls } = stubFetch(routes)
  vi.stubGlobal('fetch', fn)
  renderWithApp(<LogsPanel />, state)
  return calls
}
const rows = () => screen.getAllByTestId('log-line')

describe('the Logs tab', () => {
  it('lists the lines oldest first, with the time, a named level with a picture, the message and its attributes', async () => {
    show()
    await screen.findAllByTestId('log-line')
    expect(rows().map((r) => r.getAttribute('data-level'))).toEqual(['info', 'info', 'warn', 'error'])
    const r = within(rows()[1])
    expect(r.getByText('tuner online')).toBeInTheDocument()
    expect(r.getByText('device=')).toBeInTheDocument()
    expect(r.getByText('antenna')).toBeInTheDocument()
    expect(r.getByText('Info').parentElement!.querySelector('svg')).not.toBeNull()
    expect(within(rows()[3]).getByText('Error')).toBeInTheDocument()
    expect(within(rows()[2]).getByText('Warning')).toBeInTheDocument()
    expect(rows()[0].querySelector('time')).toHaveAttribute('datetime', at(1))
  })

  it('says how many are shown, how many are kept and how much is being logged', async () => {
    show(doc(lines, { kept: 1500, capacity: 2000, level: 'debug' }))
    await screen.findAllByTestId('log-line')
    expect(screen.getByText(/Showing 4 of 1500 kept \(it keeps the last 2000\)\. Logging everything, including debug/)).toBeInTheDocument()
  })

  it('asks for a level from the server, and for what is searched for, a moment after typing stops', async () => {
    const calls = show()
    await screen.findAllByTestId('log-line')
    const user = userEvent.setup()
    await user.selectOptions(screen.getByRole('combobox', { name: 'Show' }), 'warn')
    await waitFor(() => expect(calls.some((c) => c.url.includes('level=warn'))).toBe(true))
    await user.type(screen.getByRole('textbox', { name: 'Search the log' }), 'guide')
    await waitFor(() => expect(calls.some((c) => c.url.includes('q=guide'))).toBe(true))
    expect(calls.filter((c) => /[?&]q=(g|gu|gui|guid)$/.test(c.url))).toHaveLength(0) // not a request for each key
  })

  it('has a clear button in the search box only while there is something in it', async () => {
    show()
    await screen.findAllByTestId('log-line')
    const user = userEvent.setup()
    expect(screen.queryByRole('button', { name: 'Clear search' })).not.toBeInTheDocument()
    await user.type(screen.getByRole('textbox', { name: 'Search the log' }), 'abc')
    await user.click(screen.getByRole('button', { name: 'Clear search' }))
    expect(screen.getByRole('textbox', { name: 'Search the log' })).toHaveValue('')
    expect(screen.queryByRole('button', { name: 'Clear search' })).not.toBeInTheDocument()
  })

  it('says when nothing has been logged, and when nothing matches', async () => {
    show(doc([], { kept: 0 }))
    expect(await screen.findByText('Nothing has been logged yet.')).toBeInTheDocument()
  })

  it('says when no line matches what was asked for', async () => {
    show(doc([], { kept: 12 }))
    expect(await screen.findByText('No line matches.')).toBeInTheDocument()
  })

  it('says why when it cannot read the log', async () => {
    show({ status: 404, error: 'there is no log to show' })
    expect(await screen.findByRole('alert')).toHaveTextContent('there is no log to show')
  })

  it('clears the log on the server, and can only when there is something to clear', async () => {
    const calls = show(doc(), makeState(), { 'DELETE /logs': () => ({ status: 204 }) })
    await screen.findAllByTestId('log-line')
    await userEvent.setup().click(screen.getByRole('button', { name: 'Clear' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url.endsWith('/logs'))).toBe(true))
  })

  it('changes how much is logged, as a setting', async () => {
    const calls = show(doc(), makeState(), { 'PATCH /settings': () => ({ body: {} }) })
    await screen.findAllByTestId('log-line')
    const level = screen.getByRole('combobox', { name: 'Log level' })
    expect(level).toHaveValue('info')
    expect(within(level).getByRole('option', { name: 'Info (default)' })).toBeInTheDocument()
    await userEvent.setup().selectOptions(level, 'debug')
    await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ logLevel: 'debug' }))
  })

  it('cannot change the level from a read-only view', async () => {
    const s = makeState()
    s.editable = false
    show(doc(), s)
    await screen.findAllByTestId('log-line')
    expect(screen.getByRole('combobox', { name: 'Log level' })).toBeDisabled()
  })

  it('polls while it is live and stops when that is switched off', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      const calls = show()
      await screen.findAllByTestId('log-line')
      const n = () => calls.filter((c) => c.method === 'GET' && c.url.includes('/logs')).length
      const before = n()
      await vi.advanceTimersByTimeAsync(3100)
      await waitFor(() => expect(n()).toBeGreaterThan(before))
      await userEvent.setup({ advanceTimers: vi.advanceTimersByTime }).click(screen.getByRole('switch', { name: 'Live' }))
      const paused = n()
      await vi.advanceTimersByTimeAsync(10_000)
      expect(n()).toBe(paused)
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('opened from a link', () => {
  it('starts with what the link searched for, and asks the server for it', async () => {
    const read = () => ({ body: doc() })
    const routes = new Proxy({} as Record<string, () => { body: LogsDoc }>, { get: (_, k) => (typeof k === 'string' && k.startsWith('GET /logs') ? read : undefined) })
    const { fn, calls } = stubFetch(routes)
    vi.stubGlobal('fetch', fn)
    renderWithApp(<LogsPanel />, makeState(), { route: '/settings/logs?q=antenna' })
    expect(screen.getByRole('textbox', { name: 'Search the log' })).toHaveValue('antenna')
    await screen.findAllByTestId('log-line')
    expect(calls.some((c) => c.url.includes('q=antenna'))).toBe(true)
  })
})

describe('lineText', () => {
  it('is a line as a file would have it: time, level, message, then key=value, quoting what has spaces', () => {
    expect(lineText(line(1, 'warn', 'hello', [{ key: 'a', value: 'b' }, { key: 'err', value: 'a "b" c' }]))).toBe(`${at(1)} WARN  hello a=b err="a \\"b\\" c"`)
  })
})

describe('diagnostics mode', () => {
  it('is off, says what it does, and turns on for the time picked', async () => {
    const calls = show(doc(), makeState(), { 'PUT /diagnostics': () => ({ body: { on: true, until: at(0) } }) })
    const card = screen.getByRole('region', { name: 'Diagnostics mode' })
    expect(within(card).getByText('Diagnostics mode is off')).toBeInTheDocument()
    expect((await explain(card, 'Diagnostics mode')).getByText(/only happens now and then/)).toBeInTheDocument()
    await userEvent.keyboard('{Escape}')
    await userEvent.selectOptions(within(card).getByLabelText('Keep it on for'), '240')
    await userEvent.click(within(card).getByRole('button', { name: 'Turn on' }))
    await waitFor(() => expect(calls.find((c) => c.method === 'PUT')?.body).toEqual({ on: true, minutes: 240 }))
  })

  it('says when it turns itself off, and turns off when asked', async () => {
    const calls = show(doc(), makeState({ diagnostics: { on: true, until: at(0) } }), { 'PUT /diagnostics': () => ({ body: { on: false } }) })
    const card = screen.getByRole('region', { name: 'Diagnostics mode' })
    expect(within(card).getByText('Diagnostics mode is on')).toBeInTheDocument()
    expect(within(card).getByText(/turns itself off at/)).toBeInTheDocument()
    expect(within(card).queryByLabelText('Keep it on for')).not.toBeInTheDocument()
    await userEvent.click(within(card).getByRole('button', { name: 'Turn off' }))
    await waitFor(() => expect(calls.find((c) => c.method === 'PUT')?.body).toEqual({ on: false }))
  })

  it('has no switch when the settings can not be changed', () => {
    const s = makeState()
    s.editable = false
    show(doc(), s)
    expect(within(screen.getByRole('region', { name: 'Diagnostics mode' })).queryByRole('button', { name: /^Turn o/ })).not.toBeInTheDocument()
  })
})
