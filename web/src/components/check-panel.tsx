import { useMutation } from '@tanstack/react-query'
import { AlertTriangle, CheckCircle2, ChevronDown, Info, Loader2, MinusCircle, Stethoscope } from 'lucide-react'
import { Row, Section } from '@/components/settings/section'
import { Button } from '@/components/ui/button'
import { checkTuner } from '@/lib/api'
import { timeAgo } from '@/lib/format'
import type { CheckStep, DeviceState } from '@/lib/types'

const marks: Record<CheckStep['status'], { icon: typeof Info; label: string; className: string }> = {
  ok: { icon: CheckCircle2, label: 'Fine', className: 'text-emerald-600 dark:text-emerald-400' },
  problem: { icon: AlertTriangle, label: 'Problem', className: 'text-red-700 dark:text-red-300' },
  note: { icon: Info, label: 'Worth knowing', className: 'text-amber-700 dark:text-amber-300' },
  skipped: { icon: MinusCircle, label: 'Not checked', className: 'text-muted-foreground' },
}

/**
 * Asks "is the trouble in the tuner?": it walks the tuner's side in order, and says what it finds. It only reads,
 * and it runs when asked, not on a timer.
 */
export function CheckPanel({ device }: { device: DeviceState }) {
  const check = useMutation({ mutationFn: () => checkTuner(device.name) })
  const report = check.data
  const problems = report?.steps.filter((s) => s.status === 'problem').length ?? 0
  const notes = report?.steps.filter((s) => s.status === 'note').length ?? 0

  return (
    <Section bare icon={Stethoscope} title="Check this tuner" description="A media server says a channel isn't available, or its guide is empty? This looks at the tuner's side, step by step.">
      <Row title="Check now" description="Asks the tuner a few questions. Read-only: it never tunes a channel or changes anything.">
        <Button size="sm" variant="outline" disabled={check.isPending} onClick={() => check.mutate()}>
          {check.isPending ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Stethoscope data-icon="inline-start" />}
          {check.isPending ? 'Checking…' : 'Check now'}
        </Button>
      </Row>
      {check.isError && (
        <p role="alert" className="px-4 py-3 text-sm text-red-700 dark:text-red-300">
          {check.error instanceof Error ? check.error.message : "Couldn't check the tuner."}
        </p>
      )}
      {report && (
        <div className="px-4 py-3.5">
          <p role="status" className="mb-3 text-sm font-medium">
            {problems === 0 ? (notes === 0 ? 'Nothing wrong was found on the tuner’s side.' : 'Nothing is broken on the tuner’s side, but one thing is worth a look.') : problems === 1 ? 'One thing is wrong.' : `${problems} things are wrong.`}
            <span className="ml-2 text-xs font-normal text-muted-foreground">Checked {timeAgo(report.at)}.</span>
          </p>
          <ul className="space-y-2.5">
            {report.steps.map((s) => {
              const m = marks[s.status]
              return (
                <li key={s.id} className="flex gap-2.5 text-sm">
                  <m.icon aria-label={m.label} className={`mt-0.5 size-4 shrink-0 ${m.className}`} />
                  <div className="min-w-0">
                    <div className="font-medium">{s.title}</div>
                    <div className="text-xs text-muted-foreground">{s.detail}</div>
                  </div>
                </li>
              )
            })}
          </ul>
          {problems === 0 && (
            <p className="mt-3 text-xs text-muted-foreground">If a media server still can't play, the cause is likely between it and Yagi, or in the media server. The Logs page shows what Yagi saw.</p>
          )}
          {report.debug && report.debug.length > 0 && (
            <details className="group mt-3 rounded-lg border border-border">
              <summary className="flex cursor-pointer list-none items-center justify-between px-3 py-2 text-xs font-medium">
                What the tuner prints about itself
                <ChevronDown aria-hidden className="size-4 transition-transform group-open:rotate-180" />
              </summary>
              <div className="space-y-3 border-t border-border px-3 py-2.5">
                {report.debug.map((d) => (
                  <div key={d.name}>
                    <div className="text-xs text-muted-foreground">{d.name}</div>
                    <pre className="overflow-x-auto font-mono text-[12px] leading-relaxed">{d.lines.join('\n')}</pre>
                  </div>
                ))}
              </div>
            </details>
          )}
        </div>
      )}
    </Section>
  )
}
