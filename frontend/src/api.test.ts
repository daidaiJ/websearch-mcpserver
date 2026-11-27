import { expect, it, vi } from 'vitest'
import { api } from './api'

it('shares an in-flight overview, lets one view leave, and fetches again on refresh', async () => {
  let complete!: (value: Response) => void
  const fetcher = vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(resolve => { complete = resolve }))
  const leaving = new AbortController()
  const first = api('overview', { signal: leaving.signal })
  const second = api<{ total: number }>('overview')
  expect(fetcher).toHaveBeenCalledTimes(1)
  leaving.abort()
  await expect(first).rejects.toMatchObject({ name: 'AbortError' })
  complete(Response.json({ total: 2 }))
  await expect(second).resolves.toEqual({ total: 2 })
  const refresh = api('overview')
  expect(fetcher).toHaveBeenCalledTimes(2)
  complete(Response.json({ total: 3 }))
  await expect(refresh).resolves.toEqual({ total: 3 })
})
