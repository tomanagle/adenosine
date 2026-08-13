import { describe, expect, it } from 'vitest'

import {
  branchHeadSha,
  emptyProposalForm,
  emptyRepositoryForm,
  proposalFormSchema,
  proposalRequest,
  repositoryFormSchema,
  repositoryRequest,
} from './requests'

const headSha = 'a'.repeat(40)

describe('repositoryFormSchema', () => {
  const testCases = [
    { name: 'accepts a lowercase slug', slug: 'ledger-service', wantValid: true },
    { name: 'rejects an empty slug', slug: '', wantValid: false },
    { name: 'rejects uppercase characters', slug: 'Ledger', wantValid: false },
    { name: 'rejects a leading separator', slug: '-ledger', wantValid: false },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      const result = repositoryFormSchema.safeParse({ ...emptyRepositoryForm, slug: testCase.slug })
      expect(result.success).toBe(testCase.wantValid)
    })
  }
})

describe('repositoryRequest', () => {
  const testCases = [
    {
      name: 'trims values and omits blank optional fields',
      values: { ...emptyRepositoryForm, slug: '  ledger  ', display_name: '  ', description: '' },
      want: { slug: 'ledger', visibility: 'public', default_branch: 'main' },
    },
    {
      name: 'keeps provided metadata, visibility, and default branch',
      values: {
        slug: 'ledger',
        display_name: ' Ledger ',
        description: ' Accounts ',
        visibility: 'private' as const,
        default_branch: ' trunk ',
      },
      want: {
        slug: 'ledger',
        display_name: 'Ledger',
        description: 'Accounts',
        visibility: 'private',
        default_branch: 'trunk',
      },
    },
    {
      name: 'falls back to main when the default branch is cleared',
      values: { ...emptyRepositoryForm, slug: 'ledger', default_branch: '' },
      want: { slug: 'ledger', visibility: 'public', default_branch: 'main' },
    },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      expect(repositoryRequest(testCase.values)).toEqual(testCase.want)
    })
  }

  it('refuses a slug the API would reject', () => {
    expect(() => repositoryRequest({ ...emptyRepositoryForm, slug: 'Not Valid' })).toThrow()
  })
})

describe('proposalFormSchema', () => {
  const base = {
    ...emptyProposalForm('at://did:plc:viewer/sh.adenosine.repository/alpha', 'main'),
    source_branch: 'feature',
    title: 'Add the ledger reconciler',
  }
  const testCases = [
    { name: 'accepts distinct branches', values: base, wantValid: true, wantPath: undefined },
    {
      name: 'rejects proposing a branch onto itself',
      values: { ...base, source_branch: 'main' },
      wantValid: false,
      wantPath: 'source_branch',
    },
    {
      name: 'accepts the same branch across a fork boundary',
      values: {
        ...base,
        source_branch: 'main',
        target_repository_uri: 'at://did:plc:upstream/sh.adenosine.repository/alpha',
      },
      wantValid: true,
      wantPath: undefined,
    },
    {
      name: 'rejects an empty title',
      values: { ...base, title: '' },
      wantValid: false,
      wantPath: 'title',
    },
    {
      name: 'rejects a missing repository',
      values: { ...base, repository_uri: '' },
      wantValid: false,
      wantPath: 'repository_uri',
    },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      const result = proposalFormSchema.safeParse(testCase.values)
      expect(result.success).toBe(testCase.wantValid)
      if (!result.success) {
        expect(result.error.issues[0]?.path[0]).toBe(testCase.wantPath)
      }
    })
  }
})

describe('branchHeadSha', () => {
  const branches = [
    { name: 'main', sha: 'b'.repeat(40), default: true },
    { name: 'feature', sha: headSha, default: false },
  ]
  const testCases = [
    { name: 'reads the selected branch head', branch: 'feature', want: headSha },
    { name: 'unknown branch has no head', branch: 'missing', want: undefined },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      expect(branchHeadSha(branches, testCase.branch)).toBe(testCase.want)
    })
  }
})

describe('proposalRequest', () => {
  const values = {
    repository_uri: 'at://did:plc:viewer/sh.adenosine.repository/alpha',
    target_repository_uri: 'at://did:plc:viewer/sh.adenosine.repository/alpha',
    source_branch: 'feature',
    target_branch: 'main',
    title: '  Add the ledger reconciler  ',
    body: 'Details',
  }

  it('proposes to the selected target repository at the branch head', () => {
    const targetRepositoryURI = 'at://did:plc:upstream/sh.adenosine.repository/alpha'
    expect(proposalRequest(values, headSha)).toEqual({
      source_repository_uri: values.repository_uri,
      target_repository_uri: values.target_repository_uri,
      source_branch: 'feature',
      target_branch: 'main',
      head_sha: headSha,
      title: 'Add the ledger reconciler',
      body: 'Details',
    })

    expect(
      proposalRequest({ ...values, target_repository_uri: targetRepositoryURI }, headSha)
        .target_repository_uri,
    ).toBe(targetRepositoryURI)
  })

  it('refuses a head that is not a commit id', () => {
    expect(() => proposalRequest(values, 'not-a-sha')).toThrow()
  })
})
