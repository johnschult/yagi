import type { ComponentType, ReactNode } from 'react'
import { InfoTip } from '@/components/info-tip'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

/** A group of related settings: a heading, then one bordered list of rows. */
export function Section({ title, description, note, icon: Icon, bare = false, children }: { title: string; description?: ReactNode; /** What is true of it now, which stays on the page. */ note?: ReactNode; icon?: ComponentType<{ className?: string; 'aria-hidden'?: boolean }>; /** On a page that already says what it is, such as a tab: no heading, only the description. */ bare?: boolean; children: ReactNode }) {
  return (
    <section aria-label={title} className="space-y-2">
      {(!bare || description || note) && (
        <div className="px-1">
          {!bare && (
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              {Icon && <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />}
              {title}
            </h2>
          )}
          {(description || note) && (
            <p className={`text-xs text-muted-foreground${bare ? '' : ` mt-0.5${Icon ? ' pl-6' : ''}`}`}>
              {description}
              {description && note ? ' ' : ''}
              {note}
            </p>
          )}
        </div>
      )}
      <div className="divide-y divide-border overflow-hidden rounded-xl border border-border bg-card/70">{children}</div>
    </section>
  )
}

/** One setting: what it is on the left, how to change it on the right. */
export function Row({
  title,
  description,
  note,
  icon,
  htmlFor,
  error,
  inline = false,
  children,
}: {
  title: string
  /** What the setting does. It is behind the (i) beside the title, to keep the page short. */
  description?: ReactNode
  /** What is true of it right now (a status, a result), which stays on the row. */
  note?: ReactNode
  /** A decorative mark before the title, to set the row off. */
  icon?: ReactNode
  /** The id of the control, so the title labels it. */
  htmlFor?: string
  error?: string | null
  /** The control stays on the title's line, at the right, on a phone too (a switch), instead of wrapping below it. */
  inline?: boolean
  children: ReactNode
}) {
  return (
    <div className={cn('flex gap-3 px-4 py-3.5 sm:gap-8', inline ? 'items-start justify-between sm:items-center' : 'flex-col sm:flex-row sm:items-center sm:justify-between')}>
      <div className={cn('flex min-w-0 gap-3 sm:max-w-md', note ? 'items-start' : 'items-center', inline && 'flex-1')}>
        {icon && <span className={cn('shrink-0', note && 'mt-0.5')}>{icon}</span>}
        <div className="min-w-0">
          <div className="flex items-center gap-1.5">
            <Label htmlFor={htmlFor} className="font-medium">
              {title}
            </Label>
            {description && <InfoTip about={title}>{description}</InfoTip>}
          </div>
          {note && <p className="mt-1 text-xs text-muted-foreground">{note}</p>}
          {error && (
            <p role="alert" className="mt-1 text-xs text-red-600 dark:text-red-400">
              {error}
            </p>
          )}
        </div>
      </div>
      <div className={cn('flex shrink-0 items-center gap-2', inline && 'pt-0.5 sm:pt-0')}>{children}</div>
    </div>
  )
}
