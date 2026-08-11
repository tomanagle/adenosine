import { describe, expect, it } from 'vitest'

import {
  commitsSearchSchema,
  compareSearchSchema,
  parseRepositoryPath,
  repositoryParamsSchema,
  repositoryRevisionParamsSchema,
  treeSearchSchema,
} from './validation'

describe('repository browser route validation', () => {
  it('accepts bounded repository and revision parameters', () => {
    expect(repositoryParamsSchema.parse({ owner: 'alice.example', repo: 'project.git' })).toEqual({
      owner: 'alice.example',
      repo: 'project.git',
    })
    expect(
      repositoryRevisionParamsSchema.parse({
        owner: 'did:plc:alice',
        repo: 'project',
        revision: 'feature/browser',
      }).revision,
    ).toBe('feature/browser')
  })

  it('rejects option-like revisions, traversal paths, and invalid slugs', () => {
    expect(() =>
      repositoryRevisionParamsSchema.parse({ owner: 'alice', repo: 'Project', revision: '-n1' }),
    ).toThrow()
    expect(() => parseRepositoryPath('docs/../secret')).toThrow()
    expect(() => parseRepositoryPath('/absolute')).toThrow()
  })

  it('applies defaults without silently accepting invalid search values', () => {
    expect(() => treeSearchSchema.parse({ ref: '-bad' })).toThrow()
    expect(() => commitsSearchSchema.parse({ limit: '500' })).toThrow()
    expect(commitsSearchSchema.parse({})).toEqual({ limit: 30 })
    expect(compareSearchSchema.parse({ base: 'main', head: 'feature' })).toEqual({
      base: 'main',
      head: 'feature',
    })
  })
})
