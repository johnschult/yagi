import { Check, RotateCcw } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Row } from './section'

interface Props {
  id: string
  title: string
  description?: string
  note?: string
  /** What is saved now. The row keeps its own text until saved or reset. */
  initial: string
  /** Returns a message if the text can't be saved. */
  validate?: (text: string) => string | null
  /** Saves the text; a thrown error's message is shown on the row. */
  onSave: (text: string) => Promise<unknown>
  disabled?: boolean
  placeholder?: string
}

type Kind = { kind: 'text'; maxLength?: number } | { kind: 'number'; min?: number; max?: number } | { kind: 'lines' }

/**
 * A setting that is typed in. Save and Reset appear only once it differs from
 * what is saved; Enter saves (not in a multi-line box, where it makes a line).
 */
export function EditRow(props: Props & Kind) {
  const { id, title, description, note, initial, validate, onSave, disabled, placeholder } = props
  const [text, setText] = useState(initial)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const dirty = text !== initial

  async function save(e: FormEvent) {
    e.preventDefault()
    setError(null)
    const problem = validate?.(text) ?? null
    if (problem) return setError(problem)
    setBusy(true)
    try {
      await onSave(text)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong.')
    } finally {
      setBusy(false)
    }
  }

  const common = { id, value: text, placeholder, disabled, onChange: (e: { target: { value: string } }) => setText(e.target.value) }

  return (
    <form onSubmit={save} noValidate>
      <Row title={title} description={description} note={note} htmlFor={id} error={error}>
        {props.kind === 'lines' ? (
          <Textarea {...common} rows={3} className="w-full font-mono sm:w-72" />
        ) : props.kind === 'number' ? (
          <Input {...common} type="number" min={props.min} max={props.max} className="w-24" />
        ) : (
          <Input {...common} maxLength={props.maxLength} className="w-full sm:w-64" />
        )}
        {dirty && (
          <>
            <Button type="submit" size="sm" disabled={busy || disabled}>
              <Check data-icon="inline-start" /> Save
            </Button>
            <Button type="button" size="sm" variant="ghost" onClick={() => (setText(initial), setError(null))}>
              <RotateCcw data-icon="inline-start" /> Reset
            </Button>
          </>
        )}
      </Row>
    </form>
  )
}
