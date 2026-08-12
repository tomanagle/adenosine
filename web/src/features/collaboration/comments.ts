import type { Comment } from '@adenosine/api-client'

export function boundedCommentDepth(comment: Comment) {
  return comment.parent_uri ? 1 : 0
}
