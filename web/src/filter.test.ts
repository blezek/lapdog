import { describe, expect, it } from 'vitest'
import {
  canonicalFilterQuery,
  clearedFilterParams,
  filterParams,
  readSavedFilterSets,
  removeSavedFilterSet,
  savedFilterSummary,
  upsertSavedFilterSet,
  writeSavedFilterSets,
} from './useFilter'

class MemoryStorage {
  value: string | null = null
  getItem() { return this.value }
  setItem(_key: string, value: string) { this.value = value }
}

describe('shared filter parameters', () => {
  it('carries every filter between routes but drops page-local state', () => {
    const got = filterParams(new URLSearchParams(
      'range=30&type=Race,Practice&car=1,2&track=18,341&sel=99&page=4',
    ))
    expect(got.toString()).toBe('range=30&type=Race%2CPractice&car=1%2C2&track=18%2C341')
  })

  it('clears every filter into the all-time history view', () => {
    expect(clearedFilterParams().toString()).toBe('range=all')
  })
})

describe('saved filter sets', () => {
  it('matches equivalent filter queries regardless of key or list order', () => {
    expect(canonicalFilterQuery('track=18%2C341&type=Race&sel=4')).toBe(
      canonicalFilterQuery('type=Race&track=341%2C18'),
    )
    expect(canonicalFilterQuery('range=90')).toBe(canonicalFilterQuery(''))
  })

  it('summarizes the criteria needed to distinguish a saved view', () => {
    expect(savedFilterSummary('range=30&type=Race&track=18%2C341&ai=exclude')).toBe(
      'Last 30 days · Race · 2 tracks · No AI',
    )
    expect(savedFilterSummary('')).toBe('Last 90 days')
  })

  it('persists and reloads named filter queries', () => {
    const storage = new MemoryStorage()
    const saved = [{ id: 'weekend', name: 'Weekend races', query: 'type=Race&dow=0%2C6' }]
    writeSavedFilterSets(storage, saved)
    expect(readSavedFilterSets(storage)).toEqual(saved)
  })

  it('updates a same-name set instead of creating a duplicate', () => {
    const old = [{ id: 'one', name: 'Road', query: 'track=18' }]
    const result = upsertSavedFilterSet(old, 'road', 'track=18%2C341', 'two')
    expect(result.id).toBe('one')
    expect(result.sets).toEqual([{ id: 'one', name: 'road', query: 'track=18%2C341' }])
  })

  it('deletes only the selected set', () => {
    const sets = [
      { id: 'one', name: 'Road', query: 'track=18' },
      { id: 'two', name: 'Oval', query: 'track=2' },
    ]
    expect(removeSavedFilterSet(sets, 'one')).toEqual([sets[1]])
  })

  it('treats malformed storage as no saved filters', () => {
    const storage = new MemoryStorage()
    storage.value = '{broken'
    expect(readSavedFilterSets(storage)).toEqual([])
  })
})
