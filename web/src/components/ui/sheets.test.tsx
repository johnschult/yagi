import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { mockPhone } from '@/test/match-media'

afterEach(() => vi.unstubAllGlobals())

function Demo({ alert = false }: { alert?: boolean }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button onClick={() => setOpen(true)}>Open</button>
      {alert ? (
        <AlertDialog open={open} onOpenChange={setOpen}>
          <AlertDialogContent>
            <AlertDialogTitle>Remove it?</AlertDialogTitle>
            <AlertDialogDescription>This cannot be undone.</AlertDialogDescription>
            <AlertDialogCancel>Keep</AlertDialogCancel>
          </AlertDialogContent>
        </AlertDialog>
      ) : (
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogContent>
            <DialogTitle>Edit it</DialogTitle>
            <DialogDescription>Change things.</DialogDescription>
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}

describe.each([true, false])('dialogs when phone is %s', (phone) => {
  it('opens a dialog, as a bottom sheet only on a phone, and closes with Escape', async () => {
    mockPhone(phone)
    render(<Demo />)
    await userEvent.setup().click(screen.getByRole('button', { name: 'Open' }))
    const d = await screen.findByRole('dialog', { name: 'Edit it' })
    expect(d).toHaveAccessibleDescription('Change things.')
    expect(d.className.includes('bottom-0')).toBe(phone)
    if (phone) {
      expect(d.className).toMatch(/\bfixed\b/) // not overridden by a relative, which would drop it into the page
      expect(d.className).not.toMatch(/\brelative\b/)
      expect(d.firstElementChild).toHaveClass('absolute') // the drag handle is out of the flow, so it takes no row of the body's grid
      // The body is what scrolls, and Vaul is told to leave a swipe in it alone: when the sheet itself scrolled, a tall dialog couldn't be scrolled by touch.
      expect(d.querySelector('[data-vaul-no-drag]')).toContainElement(screen.getByText('Edit it'))
    }
    await userEvent.setup().keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('opens an alert dialog and its Cancel button closes it', async () => {
    mockPhone(phone)
    render(<Demo alert />)
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Open' }))
    const d = await screen.findByRole(phone ? 'dialog' : 'alertdialog', { name: 'Remove it?' })
    expect(d).toHaveAccessibleDescription('This cannot be undone.')
    await user.click(screen.getByRole('button', { name: 'Keep' }))
    await waitFor(() => expect(screen.queryByRole(phone ? 'dialog' : 'alertdialog')).not.toBeInTheDocument())
  })
})

describe('a dialog open while the screen crosses the phone breakpoint', () => {
  function WithInput() {
    const [open, setOpen] = useState(true)
    return (
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogTitle>Add</DialogTitle>
          <DialogDescription>Fill in.</DialogDescription>
          <input aria-label="Name" />
        </DialogContent>
      </Dialog>
    )
  }

  it('stays what it was and keeps what was typed, then follows the screen once closed and reopened', async () => {
    const m = mockPhone(true)
    render(<WithInput />)
    const user = userEvent.setup()
    await user.type(await screen.findByRole('textbox', { name: 'Name' }), 'Sam')
    act(() => m.set(false)) // turned sideways
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue('Sam')
    expect(screen.getByRole('dialog').className).toContain('bottom-0')
  })
})
