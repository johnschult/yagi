import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { makeState } from '@/test/fixtures'
import { renderWithApp, stubFetch } from '@/test/render'
import { ReportCard } from './report-card'

afterEach(() => vi.unstubAllGlobals())

const text = 'Yagi report\nMade: now\n\n== Tuner antenna ==\n'

describe('Report a problem', () => {
  it('makes the report, shows it to read, and copies it', async () => {
    const stub = stubFetch({ 'POST /report': () => ({ body: { at: new Date().toISOString(), text } }) })
    vi.stubGlobal('fetch', stub.fn)
    const user = userEvent.setup() // gives the page a clipboard to copy to
    const writeText = vi.spyOn(navigator.clipboard, 'writeText')
    renderWithApp(<ReportCard />, makeState())

    expect(stub.calls).toHaveLength(0) // nothing is made until it is asked for
    await user.click(screen.getByRole('button', { name: /make a report/i }))
    const pre = await screen.findByLabelText('Report')
    expect(pre.textContent).toBe(text)
    expect(screen.getByText(/Read it before you share it/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /^copy$/i }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(text))
  })

  it('says why when the report could not be made', async () => {
    const stub = stubFetch({ 'POST /report': () => ({ status: 500, body: { error: 'boom' } }) })
    vi.stubGlobal('fetch', stub.fn)
    const user = userEvent.setup()
    renderWithApp(<ReportCard />, makeState())
    await user.click(screen.getByRole('button', { name: /make a report/i }))
    await waitFor(() => expect(stub.calls).toHaveLength(1))
    expect(screen.queryByLabelText('Report')).not.toBeInTheDocument()
  })
})
