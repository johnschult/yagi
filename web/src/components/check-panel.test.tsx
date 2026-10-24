import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { CheckReport } from '@/lib/types'
import { makeDevice, makeState } from '@/test/fixtures'
import { renderWithApp, stubFetch } from '@/test/render'
import { CheckPanel } from './check-panel'

afterEach(() => vi.unstubAllGlobals())

const path = 'POST /devices/antenna/check'
const at = new Date().toISOString()
const good: CheckReport = {
  at,
  steps: [
    { id: 'reach', title: 'The tuner answers', status: 'ok', detail: 'It answered at 192.168.1.50.' },
    { id: 'channels', title: 'It has channels', status: 'ok', detail: '12 channels.' },
  ],
  debug: [{ name: 'Tuner 1', lines: ['net: bps=1 err=0 stop=0'] }],
}
const show = () => {
  const device = makeDevice()
  return renderWithApp(<CheckPanel device={device} />, makeState({ devices: [device] }))
}
const panel = () => screen.getByRole('region', { name: 'Check this tuner' })

describe('Check this tuner', () => {
  it('asks nothing until the button is pressed', () => {
    const stub = stubFetch({ [path]: () => ({ body: good }) })
    vi.stubGlobal('fetch', stub.fn)
    show()
    expect(stub.calls).toHaveLength(0)
    expect(within(panel()).getByRole('button', { name: 'Check now' })).toBeInTheDocument()
  })

  it('says all is well, and shows what the tuner printed', async () => {
    vi.stubGlobal('fetch', stubFetch({ [path]: () => ({ body: good }) }).fn)
    show()
    await userEvent.click(within(panel()).getByRole('button', { name: 'Check now' }))
    expect(await within(panel()).findByText('Nothing wrong was found on the tuner’s side.')).toBeInTheDocument()
    expect(within(panel()).getByText('12 channels.')).toBeInTheDocument()
    expect(within(panel()).getByText(/net: bps=1 err=0 stop=0/)).toBeInTheDocument()
  })

  it('counts and explains what is wrong', async () => {
    const bad: CheckReport = { at, steps: [{ id: 'channels', title: 'It has channels', status: 'problem', detail: 'The channel list is empty.' }, ...good.steps.slice(0, 1)] }
    vi.stubGlobal('fetch', stubFetch({ [path]: () => ({ body: bad }) }).fn)
    show()
    await userEvent.click(within(panel()).getByRole('button', { name: 'Check now' }))
    expect(await within(panel()).findByText('One thing is wrong.')).toBeInTheDocument()
    expect(within(panel()).getByLabelText('Problem')).toBeInTheDocument()
    expect(within(panel()).getByText('The channel list is empty.')).toBeInTheDocument()
  })

  it('does not say all is well when the tuner counted errors', async () => {
    const noisy: CheckReport = { at, steps: [{ id: 'errors', title: "The tuner's own error counters", status: 'note', detail: 'Tuner 2: 31 transport errors.' }] }
    vi.stubGlobal('fetch', stubFetch({ [path]: () => ({ body: noisy }) }).fn)
    show()
    await userEvent.click(within(panel()).getByRole('button', { name: 'Check now' }))
    expect(await within(panel()).findByText(/worth a look/)).toBeInTheDocument()
    expect(within(panel()).queryByText(/Nothing wrong was found/)).not.toBeInTheDocument()
  })

  it('says so when the check itself fails', async () => {
    vi.stubGlobal('fetch', stubFetch({ [path]: () => ({ status: 409, body: { error: 'checking the tuner is not available' } }) }).fn)
    show()
    await userEvent.click(within(panel()).getByRole('button', { name: 'Check now' }))
    expect(await within(panel()).findByRole('alert')).toHaveTextContent('checking the tuner is not available')
  })
})
