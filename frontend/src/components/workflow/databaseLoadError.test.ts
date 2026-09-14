import { describe, expect, it } from 'vitest'
import { describeDatabaseLoadFailure } from './databaseLoadError'

describe('describeDatabaseLoadFailure', () => {
  it('treats a not-yet-created managed database as an empty state', () => {
    expect(describeDatabaseLoadFailure({
      message: 'Request failed with status code 400',
      response: {
        data: {
          message: 'Invalid db_path',
          error: 'database file not found: Chats/Work/projects/demo/db/db.sqlite',
        },
      },
    })).toEqual({
      missing: true,
      message: 'database file not found: Chats/Work/projects/demo/db/db.sqlite',
    })
  })

  it('keeps the server detail for a real database failure', () => {
    expect(describeDatabaseLoadFailure({
      message: 'Request failed with status code 500',
      response: { data: { error: 'database is locked' } },
    })).toEqual({ missing: false, message: 'database is locked' })
  })
})
