import { useMutation } from '@tanstack/react-query'
import { Check, Copy, Download, FileText, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { makeReport } from '@/lib/api'

/**
 * "Report a problem": gathers the settings, each tuner's state and events, a fresh check of every tuner and the newest
 * log lines into one piece of text, to copy or save and send to whoever is helping. Nothing leaves Yagi by itself.
 */
export function ReportCard() {
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const report = useMutation({
    mutationFn: makeReport,
    onSuccess: () => setOpen(true),
    onError: (e) => toast.error(e instanceof Error ? e.message : "Couldn't make the report"),
  })
  const text = report.data?.text ?? ''

  async function copy() {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      toast.error("Couldn't copy: select the text and copy it instead")
    }
  }

  function save() {
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }))
    const a = document.createElement('a')
    a.href = url
    a.download = `yagi-report-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}.txt`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <section aria-label="Report a problem" className="flex flex-wrap items-center gap-x-4 gap-y-3 rounded-xl border border-border bg-card/70 px-4 py-3.5">
      <FileText aria-hidden className="size-5 shrink-0 text-muted-foreground" />
      <div className="min-w-0 flex-1 basis-72">
        <h2 className="text-sm font-semibold">Report a problem</h2>
        <p className="text-xs text-muted-foreground">
          Puts everything in one place for whoever is helping: your settings, each tuner's state and recent events, a fresh check of every tuner, and the newest log lines. Nothing is sent anywhere. You read it, then copy or save it.
        </p>
      </div>
      <Button size="sm" disabled={report.isPending} onClick={() => report.mutate()}>
        {report.isPending ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <FileText data-icon="inline-start" />}
        {report.isPending ? 'Checking the tuners…' : 'Make a report'}
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>Your report</DialogTitle>
            <DialogDescription>
              It has no passwords or keys, but it does have the addresses of your tuner and of whoever watched. Read it before you share it.
            </DialogDescription>
          </DialogHeader>
          <pre
            aria-label="Report"
            tabIndex={0}
            className="min-h-0 overflow-auto rounded-lg border border-border bg-muted p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-words outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            {text}
          </pre>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={save}>
              <Download data-icon="inline-start" /> Save as a file
            </Button>
            <Button type="button" onClick={copy}>
              {copied ? <Check data-icon="inline-start" /> : <Copy data-icon="inline-start" />}
              {copied ? 'Copied' : 'Copy'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}
