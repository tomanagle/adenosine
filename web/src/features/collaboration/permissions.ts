export function canTriageIssue(input: { local: boolean; ownerDid: string; viewerDid?: string }) {
  return input.local && input.viewerDid === input.ownerDid
}
