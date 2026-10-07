import { describe, expect, it } from 'vitest'

import { matchPlatformCombination } from './Garage61'
import type { GarageCatalog } from './api'

describe('Garage61 recorded combination selection', () => {
  const catalog: GarageCatalog = {
    configured: true,
    cars: [
      { id: 11, name: 'Mazda', platform: 'iracing', platform_id: '67' },
      { id: 12, name: 'Another car', platform: 'iracing', platform_id: '68' },
    ],
    tracks: [
      { id: 21, name: 'Spa', variant: 'Grand Prix Pits', platform: 'iracing', platform_id: '523' },
      { id: 22, name: 'Spa', variant: 'Bike', platform: 'iracing', platform_id: '524' },
    ],
  }

  it('selects the exact iRacing car and layout IDs', () => {
    expect(matchPlatformCombination(catalog, '67', '523')).toEqual({ car: catalog.cars[0], track: catalog.tracks[0] })
    expect(matchPlatformCombination(catalog, '67', '524')?.track.id).toBe(22)
  })

  it('refuses missing or ambiguous matches', () => {
    expect(matchPlatformCombination(catalog, '67', '999')).toBeNull()
    expect(matchPlatformCombination({ ...catalog, cars: [...catalog.cars, { ...catalog.cars[0]!, id: 13 }] }, '67', '523')).toBeNull()
  })
})
