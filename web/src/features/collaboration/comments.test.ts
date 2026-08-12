import type { Comment } from '@adenosine/api-client'
import { describe, expect, it } from 'vitest'

import { boundedCommentDepth } from './comments'

describe('boundedCommentDepth', () => {
  const base = {
    uri: 'at://comment',
    cid: 'cid',
    author_did: 'did:plc:a',
    issue_uri: 'at://issue',
    issue_cid: 'issue-cid',
    parent_uri: null,
    parent_cid: null,
    body: '',
    created_at: '',
    updated_at: '',
    indexed_at: '',
  } satisfies Comment
  const testCases = [
    { name: 'root comment', comment: base, want: 0 },
    {
      name: 'reply is bounded to one visual level',
      comment: { ...base, uri: 'at://reply', parent_uri: base.uri, parent_cid: base.cid },
      want: 1,
    },
  ]
  for (const testCase of testCases) {
    it(testCase.name, () => expect(boundedCommentDepth(testCase.comment)).toBe(testCase.want))
  }
})
