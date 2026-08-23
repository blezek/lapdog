import { describe, expect, it } from 'vitest'

import { createQueryClient, pageRefreshMs } from './queryClient'

describe('query client refresh', () => {
  it('polls mounted page queries every five seconds', () => {
    const queries = createQueryClient().getDefaultOptions().queries

    expect(pageRefreshMs).toBe(5_000)
    expect(queries?.refetchInterval).toBe(pageRefreshMs)
    expect(queries?.refetchIntervalInBackground).toBe(true)
  })

  it('does not add a redundant request when the window regains focus', () => {
    const queries = createQueryClient().getDefaultOptions().queries

    expect(queries?.refetchOnWindowFocus).toBe(false)
  })
})
