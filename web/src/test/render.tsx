import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within, type BoundFunctions, type queries } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement } from 'react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { Toaster } from '@/components/ui/sonner'
import { AppContext } from '@/hooks/app-context'
import { TunerPage } from '@/pages/tuner'
import { SettingsPage } from '@/pages/settings'
import { TunersPage } from '@/pages/tuners'
import type { AppState } from '@/lib/types'

/** Render a page with a fixed app state, no network, and a router. */
export function renderWithApp(ui: ReactElement, state: AppState, opts: { connected?: boolean; route?: string } = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return {
    client,
    ...render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[opts.route ?? '/']}>
          <AppContext.Provider value={{ state, connected: opts.connected ?? true }}>{ui}</AppContext.Provider>
        </MemoryRouter>
        <Toaster />
      </QueryClientProvider>,
    ),
  }
}

/** Replace fetch with a stub that answers from `routes` (`"METHOD /path"` -> handler). */
export function stubFetch(routes: Record<string, (body: unknown) => { status?: number; body?: unknown }>) {
  const calls: { method: string; url: string; body: unknown }[] = []
  const fn = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ method, url, body })
    // Whatever the base path is: API routes are keyed without it ("GET /state"),
    // sign-in routes keep their /auth prefix ("POST /auth/plex/start").
    let path = url.replace(/^.*?\/api(?=\/)/, '')
    if (path === url) path = url.replace(/^.*?(?=\/auth\/)/, '')
    const key = `${method} ${path}`
    const handler = routes[key]
    if (!handler) return new Response(JSON.stringify({ error: `unexpected ${key}` }), { status: 500 })
    const r = handler(body)
    const status = r.status ?? 200
    return new Response(status === 204 ? null : JSON.stringify(r.body ?? {}), { status })
  }
  return { fn, calls }
}

/** The tuner pages with their routes, opened at `path` (a list at /tuner, a tuner at /tuners/antenna, a tab at /tuners/antenna/device). */
export function renderTunerRoutes(state: AppState, path: string, opts: { connected?: boolean } = {}) {
  return renderWithApp(
    <Routes>
      <Route path="/tuners" element={<TunersPage />} />
      <Route path="/tuners/:name" element={<TunerPage />} />
      <Route path="/tuners/:name/:tab" element={<TunerPage />} />
    </Routes>,
    state,
    { ...opts, route: path },
  )
}

/** The settings, opened on one of its tabs. */
export function renderSettings(state: AppState, tab: 'access' | 'notifications' | 'privacy' | 'logs' = 'access', opts: { connected?: boolean } = {}) {
  return renderWithApp(
    <Routes>
      <Route path="/settings" element={<SettingsPage />} />
      <Route path="/settings/:tab" element={<SettingsPage />} />
    </Routes>,
    state,
    { ...opts, route: tab === 'access' ? '/settings' : `/settings/${tab}` },
  )
}

/** Open the (i) beside something (the "About <name>" button, found in `scope`) and return what it says. */
export async function explain(scope: HTMLElement | BoundFunctions<typeof queries>, name: string | RegExp) {
  const button = 'getByRole' in scope ? scope.getByRole('button', { name: typeof name === 'string' ? `About ${name}` : name }) : within(scope).getByRole('button', { name: typeof name === 'string' ? `About ${name}` : name })
  await userEvent.setup().click(button)
  return within(await screen.findByRole('dialog'))
}
