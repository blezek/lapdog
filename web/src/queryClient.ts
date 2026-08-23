import { QueryClient } from '@tanstack/react-query'

/** Historical views refresh while mounted so an active session appears without a reload. */
export const pageRefreshMs = 5_000

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        // Every mounted page reads a loopback API, so periodic refresh is cheap.
        // Live telemetry and connection status specify their own faster intervals.
        refetchInterval: pageRefreshMs,
        // Keep historical views current while iRacing owns focus. Otherwise the
        // browser pauses the interval for the exact period the user is racing.
        refetchIntervalInBackground: true,
        // The interval above is the refresh clock. A window-focus request on top of
        // it makes switching between LapDog and iRacing issue redundant requests.
        refetchOnWindowFocus: false,
        staleTime: 15_000,
        retry: 1,
      },
    },
  })
}
