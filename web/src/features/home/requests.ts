import type {
  Branch,
  CreatePullRequestRequest,
  CreateRepositoryRequest,
} from '@adenosine/api-client'
import {
  zCreatePullRequestRequest,
  zCreateRepositoryRequest,
  zRepositorySlug,
  zRepositoryUri,
} from '@adenosine/api-client/schemas'
import type { z } from 'zod'

/**
 * Forms validate with the generated request schemas so the browser rejects the
 * same values the API would, and submit bodies are parsed by those schemas
 * before they leave the page.
 */

export const repositoryFormSchema = zCreateRepositoryRequest

export type RepositoryFormValues = z.input<typeof repositoryFormSchema>

export const emptyRepositoryForm: RepositoryFormValues = {
  slug: '',
  display_name: '',
  description: '',
  visibility: 'public',
  default_branch: 'main',
}

/**
 * Field-level messages keep the generated constraints as the source of truth
 * while saying, in words, what the reader has to change.
 */
export function slugMessage(value: string | undefined): string | undefined {
  const slug = (value ?? '').trim()
  if (!slug) return 'Enter a repository name.'
  if (!zRepositorySlug.safeParse(slug).success) {
    return 'Use lowercase letters, numbers, dots, and dashes, starting with a letter or number.'
  }
  return undefined
}

export function requiredMessage(value: string | undefined, message: string): string | undefined {
  return (value ?? '').trim() ? undefined : message
}

export function repositoryRequest(values: RepositoryFormValues): CreateRepositoryRequest {
  return zCreateRepositoryRequest.parse({
    slug: values.slug.trim(),
    display_name: values.display_name?.trim() || undefined,
    description: values.description?.trim() || undefined,
    visibility: values.visibility,
    default_branch: values.default_branch?.trim() || 'main',
    organization: values.organization?.trim() || undefined,
  })
}

export const proposalFormSchema = zCreatePullRequestRequest
  .pick({ source_branch: true, target_branch: true, title: true, body: true })
  .extend({ repository_uri: zRepositoryUri })
  .refine((values) => values.source_branch !== values.target_branch, {
    error: 'Pick a source branch that differs from the target branch.',
    path: ['source_branch'],
  })

export type ProposalFormValues = z.input<typeof proposalFormSchema>

export function emptyProposalForm(repositoryUri = '', targetBranch = ''): ProposalFormValues {
  return {
    repository_uri: repositoryUri,
    source_branch: '',
    target_branch: targetBranch,
    title: '',
    body: '',
  }
}

/** A proposal records the exact commit being proposed, read from the branch head. */
export function branchHeadSha(branches: Branch[], name: string): string | undefined {
  return branches.find((branch) => branch.name === name)?.sha
}

export function proposalRequest(
  values: ProposalFormValues,
  headSha: string,
): CreatePullRequestRequest {
  return zCreatePullRequestRequest.parse({
    source_repository_uri: values.repository_uri,
    target_repository_uri: values.repository_uri,
    source_branch: values.source_branch,
    target_branch: values.target_branch,
    head_sha: headSha,
    title: values.title.trim(),
    body: values.body,
  })
}
