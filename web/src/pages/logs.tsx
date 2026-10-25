import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, Loader2, Stethoscope, Trash2, X } from 'lucide-react'
import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router'
import { toast } from 'sonner'
import { InfoTip } from '@/components/info-tip'
import { ReportCard } from '@/components/report-card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { useApp } from '@/hooks/app-context'
import { useEdits } from '@/hooks/mutations'
import { STATE_KEY } from '@/hooks/use-live-state'
import { clearLogs, getLogs, setDiagnostics } from '@/lib/api'
import { clock, LEVEL_LOOK, LEVELS, lineText } from '@/lib/logs'
import type { LogEntry, LogLevel } from '@/lib/types'
import { cn } from '@/lib/utils'

/** Show lines at this level and above. */
const SHOW: { id: LogLevel | ''; label: string }[] = [
  { id: '', label: 'Everything kept' },
  { id: 'info', label: 'Info and above' },
  { id: 'warn', label: 'Warnings and errors' },
  { id: 'error', label: 'Errors only' },
]

function Line({ e }: { e: LogEntry }) {
  const look = LEVEL_LOOK[e.level]
  return (
    <li data-testid="log-line" data-level={e.level} className="flex flex-col gap-x-3 gap-y-0.5 px-3 py-1.5 font-mono text-xs sm:flex-row">
      <span className="flex shrink-0 items-center gap-2">
        <time dateTime={e.time} title={e.time} className="tabular-nums text-muted-foreground">
          {clock(e.time)}
        </time>
        <span className={cn('inline-flex w-[4.5rem] items-center gap-1 font-sans font-medium', look.tone)}>
          <look.icon aria-hidden className="size-3.5" />
          {look.label}
        </span>
      </span>
      <span className="min-w-0 flex-1 break-words">
        <span>{e.message}</span>
        {(e.attrs ?? []).map((a) => (
          // a real space before each, so the words of the message and of the details can break between them
          <Fragment key={a.key}>
            {' '}
            <span className="whitespace-pre-wrap text-muted-foreground">
              <span className="opacity-80">{a.key}=</span>
              <span className="text-foreground/80">{a.value}</span>
            </span>
          </Fragment>
        ))}
      </span>
    </li>
  )
}

/** How long diagnostics mode stays on, in minutes. */
const DIAG_FOR = [
  { minutes: 15, label: '15 minutes' },
  { minutes: 60, label: '1 hour' },
  { minutes: 240, label: '4 hours' },
  { minutes: 1440, label: '24 hours' },
]

/**
 * Diagnostics mode: off until asked for, and it turns itself off. While it is on Yagi writes down how every request
 * went and checks the tuner the moment it fails one, which is how to catch "channel not available" when it only
 * happens now and then.
 */
function DiagnosticsCard({ editable }: { editable: boolean }) {
  const { state } = useApp()
  const qc = useQueryClient()
  const [minutes, setMinutes] = useState(60)
  const diag = state.diagnostics
  const on = diag?.on === true
  const change = useMutation({
    mutationFn: (p: { on: boolean }) => setDiagnostics(p.on, p.on ? minutes : undefined),
    onSuccess: (d) => {
      toast.success(d.on ? 'Diagnostics mode is on' : 'Diagnostics mode is off')
      void qc.invalidateQueries({ queryKey: STATE_KEY })
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : "Couldn't change diagnostics mode"),
  })
  if (!diag) return null // an older server has none
  return (
    <section aria-label="Diagnostics mode" className="flex flex-wrap items-center gap-x-4 gap-y-3 rounded-xl border border-border bg-card/70 px-4 py-3.5">
      <Stethoscope aria-hidden className={cn('size-5 shrink-0', on ? 'text-sky-600 dark:text-sky-400' : 'text-muted-foreground')} />
      <div className="min-w-0 flex-1 basis-72">
        <div className="flex items-center gap-1.5">
          <h2 className="text-sm font-semibold">Diagnostics mode {on ? 'is on' : 'is off'}</h2>
          <InfoTip about="Diagnostics mode">
            For trouble that only happens now and then. While it is on, Yagi writes down how every request went (how long, what the tuner answered, how the stream ended) and checks the tuner the moment it fails one. It turns itself off.
          </InfoTip>
        </div>
        {on && diag.until && (
          <p className="text-xs text-muted-foreground">It turns itself off at {clock(diag.until)}. Turn it on, wait for the trouble, then look here.</p>
        )}
      </div>
      {editable && (
        <div className="flex items-center gap-2">
          {!on && (
            <Select aria-label="Keep it on for" value={minutes} onChange={(e) => setMinutes(Number(e.target.value))} className="h-8 w-32">
              {DIAG_FOR.map((d) => (
                <option key={d.minutes} value={d.minutes}>
                  {d.label}
                </option>
              ))}
            </Select>
          )}
          <Button size="sm" variant={on ? 'outline' : 'default'} disabled={change.isPending} onClick={() => change.mutate({ on: !on })}>
            {change.isPending && <Loader2 data-icon="inline-start" className="animate-spin" />}
            {on ? 'Turn off' : 'Turn on'}
          </Button>
        </div>
      )}
    </section>
  )
}

/** Yagi's own log: what it has been doing, with the problems easy to find. */
export function LogsPanel() {
  const { state } = useApp()
  const qc = useQueryClient()
  const { settings: update } = useEdits()
  const [level, setLevel] = useState<LogLevel | ''>('')
  // A link can open the log already searched, such as for one tuner (?q=antenna).
  const [params] = useSearchParams()
  const [text, setText] = useState(() => (params.get('q') ?? '').slice(0, 200))
  const [term, setTerm] = useState(text)
  const [live, setLive] = useState(true)

  // What is typed is searched for a moment after it stops, so each key is not a request.
  useEffect(() => {
    const t = setTimeout(() => setTerm(text), 250)
    return () => clearTimeout(t)
  }, [text])

  const q = useQuery({
    queryKey: ['logs', level, term],
    queryFn: () => getLogs(level, term),
    refetchInterval: live ? 3000 : false,
    retry: false,
    refetchOnWindowFocus: false,
    placeholderData: (prev) => prev,
  })
  const entries = useMemo(() => q.data?.entries ?? [], [q.data])

  // Stay at the newest line while it is live and you have not scrolled up to read something.
  const end = useRef<HTMLDivElement>(null)
  const pinned = useRef(true)
  useEffect(() => {
    const onScroll = () => {
      pinned.current = window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 80
    }
    onScroll()
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [])
  const landed = useRef(false)
  useEffect(() => {
    if (entries.length === 0) return
    if (!landed.current) {
      landed.current = true // the page opens at its top, with the controls, not scrolled to the end
      return
    }
    if (live && pinned.current) end.current?.scrollIntoView?.({ block: 'end' })
  }, [entries, live])

  const clear = useMutation({
    mutationFn: clearLogs,
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['logs'] }),
    onError: (err) => toast.error(err instanceof Error ? err.message : 'Could not clear the log'),
  })

  const download = () => {
    const body = entries.map(lineText).join('\n') + '\n'
    const url = URL.createObjectURL(new Blob([body], { type: 'text/plain' }))
    const a = document.createElement('a')
    a.href = url
    a.download = `yagi-log-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}.txt`
    a.click()
    URL.revokeObjectURL(url)
  }

  const editable = state.editable
  const logLevel = state.settings.logLevel

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
        <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
          What Yagi has been doing.
          <InfoTip about="the log">
            <p>The newest lines are kept in memory, not in a file, and start over when it restarts.</p>
            <p>
              <strong className="font-medium text-foreground">Show</strong> picks which of the kept lines are listed; <strong className="font-medium text-foreground">Log level</strong> sets how much it writes down from now on.
            </p>
            <p>
              The requests a media server makes over and over (GET discover.json, lineup.json and the like) are only recorded at the Debug level, because there are so many; that fills the log quickly. Streams, refusals and warnings are always shown.
            </p>
          </InfoTip>
        </p>
        <div className="flex items-center gap-2 text-sm" title="How much Yagi writes down from now on. Lines already kept don't change.">
          <label htmlFor="log-level" className="text-muted-foreground">
            Log level
          </label>
          <Select
            id="log-level"
            value={logLevel}
            disabled={!editable || update.isPending}
            onChange={(e) => update.mutate({ logLevel: e.target.value as LogLevel })}
            className="h-8 w-36"
          >
            {LEVELS.map((l) => (
              <option key={l} value={l}>
                {l === 'info' ? 'Info (default)' : LEVEL_LOOK[l].label === 'Warning' ? 'Warnings and up' : LEVEL_LOOK[l].label}
              </option>
            ))}
          </Select>
        </div>
      </div>

      <DiagnosticsCard editable={editable} />

      <ReportCard />

      <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
        <div className="flex items-center gap-2 text-sm">
          <label htmlFor="log-show" className="text-muted-foreground">
            Show
          </label>
          <Select id="log-show" value={level} onChange={(e) => setLevel(e.target.value as LogLevel | '')} className="h-8 min-w-0 flex-1 sm:w-52 sm:flex-none">
            {SHOW.map((s) => (
              <option key={s.id} value={s.id}>
                {s.label}
              </option>
            ))}
          </Select>
        </div>
        <div className="relative min-w-0 flex-1 sm:max-w-sm">
          <Input aria-label="Search the log" placeholder="Search" value={text} onChange={(e) => setText(e.target.value)} className="h-8 w-full pr-8" />
          {text !== '' && (
            <button
              type="button"
              aria-label="Clear search"
              title="Clear search"
              onClick={() => {
                setText('')
                setTerm('')
              }}
              className="absolute top-1/2 right-1.5 inline-flex size-5 -translate-y-1/2 items-center justify-center rounded-full text-muted-foreground outline-none hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
            >
              <X aria-hidden className="size-3.5" />
            </button>
          )}
        </div>
        <div className="flex items-center gap-3 sm:ml-auto">
          <label className="flex items-center gap-2 text-sm">
            <Switch checked={live} onCheckedChange={setLive} aria-label="Live" /> Live
          </label>
          <Button size="sm" variant="outline" disabled={entries.length === 0} onClick={download}>
            <Download data-icon="inline-start" /> Download
          </Button>
          <Button size="sm" variant="outline" disabled={clear.isPending || (q.data?.kept ?? 0) === 0} onClick={() => clear.mutate()}>
            <Trash2 data-icon="inline-start" /> Clear
          </Button>
        </div>
      </div>

      {q.isPending ? (
        <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 aria-hidden className="size-4 animate-spin" /> Loading…
        </p>
      ) : q.isError ? (
        <p role="alert" className="text-sm text-red-700 dark:text-red-300">
          {q.error instanceof Error ? q.error.message : "Couldn't read the log."}
        </p>
      ) : entries.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-10 text-center text-sm text-muted-foreground">
          {(q.data?.kept ?? 0) === 0 ? 'Nothing has been logged yet.' : 'No line matches.'}
        </p>
      ) : (
        <ul aria-label="Log lines" className="divide-y divide-border overflow-hidden rounded-xl border border-border bg-card/70">
          {entries.map((e) => (
            <Line key={e.seq} e={e} />
          ))}
        </ul>
      )}
      <div ref={end} />

      {q.data && (
        <p className="text-xs text-muted-foreground">
          Showing {entries.length} of {q.data.kept} kept (it keeps the last {q.data.capacity}). Logging {q.data.level === 'info' ? 'info and above' : q.data.level === 'warn' ? 'warnings and errors' : q.data.level === 'error' ? 'errors only' : 'everything, including debug'}.
        </p>
      )}
    </div>
  )
}
