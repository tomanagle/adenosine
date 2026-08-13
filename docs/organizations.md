# Organizations

Organizations are shared owners for repositories and policy. A person can belong to many
organizations and an organization can contain many people. The first release deliberately
uses GitHub's familiar access vocabulary:

- organization roles are `owner` and `member`;
- invitations expire after seven days and must be accepted by the invitee;
- membership visibility is private by default and only the member can make it public;
- teams contain organization members and have `member` and `maintainer` roles;
- repository roles are `read`, `triage`, `write`, `maintain`, and `admin`;
- a base permission applies to every member, while stronger direct or team access wins;
- organization owners are administrators of every organization repository;
- people with an effective repository `admin` role can manage that repository's direct and
  outside collaborators;
- repository collaborators can remain outside collaborators without organization membership;
- an organization must always retain at least one owner.

The founding owner is also the author of the organization's AT Protocol root. They must remain
an owner until Adenosine supports a rotatable organization-controlled DID; allowing a local
demotion before then would make the database disagree with the portable authority root.

Features without an Adenosine equivalent, such as billing managers, GitHub App managers,
enterprise-managed users, and SAML/SCIM, are not represented as inert roles. They should be
added when the corresponding capability exists so a role never promises permissions the
server cannot enforce.

## AT Protocol authority

An AT repository has one DID author, so Adenosine does not model an organization as though it
could directly write to several people's repositories. The public contract is consent based:

1. The creator publishes `dev.adenosine.organization`, the stable organization root record.
2. Invitations and private membership are kept in the authoritative server database. They are
   intentionally not written to an AT repository: AT repository records are public, so a
   so-called private grant would disclose the relationship it was meant to hide.
3. When a member explicitly makes membership public, the root controller (or an owner with a
   valid public owner grant) publishes `dev.adenosine.organizationGrant`, and the member publishes
   `dev.adenosine.organizationMembership` in their own AT repository. Its deterministic key is
   derived from the organization URI.
4. Hiding a formerly public membership publishes `dev.adenosine.organizationRevocation` against
   the exact grant and deletes the current membership record. AT repository history and external
   indexers may retain earlier public evidence, so the UI does not promise historical erasure.

The federation ingestion path validates canonical record authorship shape, deterministic keys,
strong-reference syntax, roles, visibility, and timestamps before storing untrusted network
evidence. It does not use network organization records for local authorization; local membership,
owner, team, and repository policy is enforced synchronously from `core.*`. A future public
authority resolver can expose verified network membership after resolving full grant and
revocation chains without weakening local access control.

Organization profile changes republish the stable creator-authored root. Team membership,
repository permissions, and private organization policy remain local security state. A repository
AT record can declare an organization reference for discovery, but that declaration is not an
authorization grant. A future organization-controlled DID can remove the founding-owner
constraint and make shared root control portable.

## Permission resolution

For an organization repository, effective permission is the strongest of:

1. `admin` for organization owners;
2. the organization base permission for members;
3. a direct repository assignment;
4. every team repository assignment.

Public repositories remain readable by everyone. Base permissions do not apply to outside
collaborators. Only `write`, `maintain`, and `admin` permit Git pushes; `triage`, `write`,
`maintain`, and `admin` permit issue and pull-request state management without weakening AT
Protocol authorship. The acting user is authorized locally, while the repository controller
publishes the canonical status record. Destructive repository settings and direct collaborator
management require `admin`.

Visible teams can be nested and child teams inherit repository access from their parents. Secret
teams are top-level only. Organization owners and team maintainers can manage their team; moving a
team beneath another team requires maintainer authority on both sides. Deleting a parent deletes
its child-team hierarchy, matching the destructive behavior called out in the UI confirmation.

Every membership, role, team, repository-access, and policy mutation writes an organization
audit event with actor DID, action, target, request ID, and timestamp. Audit events are local
security records and are not placed in a public AT repository.
