CREATE TABLE core.organizations (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    website TEXT,
    location TEXT,
    creator_did TEXT NOT NULL REFERENCES core.accounts(did),
    base_permission TEXT NOT NULL DEFAULT 'read',
    members_can_create_repositories BOOLEAN NOT NULL DEFAULT true,
    state TEXT NOT NULL DEFAULT 'creating',
    at_uri TEXT UNIQUE,
    at_cid TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ,
    CONSTRAINT organizations_slug_format CHECK (slug ~ '^[a-z0-9][a-z0-9-]*$'),
    CONSTRAINT organizations_name_nonempty CHECK (length(name) > 0),
    CONSTRAINT organizations_base_permission CHECK (base_permission IN ('none', 'read', 'write')),
    CONSTRAINT organizations_state CHECK (state IN ('creating', 'active', 'failed', 'deleting', 'deleted'))
);

CREATE UNIQUE INDEX organizations_slug_active_uidx
    ON core.organizations (lower(slug))
    WHERE deleted_at IS NULL;

CREATE INDEX organizations_creator_idx
    ON core.organizations (creator_did)
    WHERE deleted_at IS NULL;

CREATE TABLE core.organization_members (
    organization_id UUID NOT NULL REFERENCES core.organizations(id) ON DELETE CASCADE,
    account_did TEXT NOT NULL REFERENCES core.accounts(did) ON DELETE CASCADE,
    role TEXT NOT NULL,
    visibility TEXT NOT NULL DEFAULT 'private',
    invited_by_did TEXT NOT NULL REFERENCES core.accounts(did),
    grant_uri TEXT,
    grant_cid TEXT,
    membership_uri TEXT,
    membership_cid TEXT,
    joined_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, account_did),
    CONSTRAINT organization_member_role CHECK (role IN ('owner', 'member')),
    CONSTRAINT organization_member_visibility CHECK (visibility IN ('private', 'public')),
    CONSTRAINT organization_member_grant_pair CHECK ((grant_uri IS NULL) = (grant_cid IS NULL)),
    CONSTRAINT organization_member_record_pair CHECK ((membership_uri IS NULL) = (membership_cid IS NULL))
);

CREATE INDEX organization_members_account_idx ON core.organization_members (account_did, organization_id DESC);
CREATE INDEX organization_members_visibility_idx ON core.organization_members (organization_id, visibility, account_did);
CREATE INDEX organization_members_public_idx
    ON core.organization_members (organization_id, joined_at)
    WHERE visibility = 'public';

CREATE TABLE core.organization_invitations (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES core.organizations(id) ON DELETE CASCADE,
    invitee_did TEXT NOT NULL,
    role TEXT NOT NULL,
    invited_by_did TEXT NOT NULL REFERENCES core.accounts(did),
    grant_uri TEXT,
    grant_cid TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT organization_invitation_role CHECK (role IN ('owner', 'member')),
    CONSTRAINT organization_invitation_expiry CHECK (expires_at > created_at),
    CONSTRAINT organization_invitation_grant_pair CHECK ((grant_uri IS NULL) = (grant_cid IS NULL)),
    CONSTRAINT organization_invitation_terminal_state CHECK (accepted_at IS NULL OR revoked_at IS NULL)
);

CREATE UNIQUE INDEX organization_invitations_pending_uidx
    ON core.organization_invitations (organization_id, invitee_did)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

CREATE INDEX organization_invitations_invitee_idx
    ON core.organization_invitations (invitee_did, created_at DESC, id DESC)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;
CREATE INDEX organization_invitations_org_time_idx
    ON core.organization_invitations (organization_id, created_at DESC, id DESC)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

CREATE TABLE core.organization_teams (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES core.organizations(id) ON DELETE CASCADE,
    parent_team_id UUID REFERENCES core.organization_teams(id) ON DELETE SET NULL,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    visibility TEXT NOT NULL DEFAULT 'visible',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ,
    CONSTRAINT organization_teams_slug_format CHECK (slug ~ '^[a-z0-9][a-z0-9-]*$'),
    CONSTRAINT organization_teams_name_nonempty CHECK (length(name) > 0),
    CONSTRAINT organization_teams_visibility CHECK (visibility IN ('visible', 'secret'))
);

CREATE UNIQUE INDEX organization_teams_slug_active_uidx
    ON core.organization_teams (organization_id, lower(slug))
    WHERE deleted_at IS NULL;
CREATE INDEX organization_teams_name_idx
    ON core.organization_teams (organization_id, lower(name), id)
    WHERE deleted_at IS NULL;

CREATE TABLE core.organization_team_members (
    team_id UUID NOT NULL REFERENCES core.organization_teams(id) ON DELETE CASCADE,
    account_did TEXT NOT NULL REFERENCES core.accounts(did) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (team_id, account_did),
    CONSTRAINT organization_team_member_role CHECK (role IN ('member', 'maintainer'))
);

CREATE INDEX organization_team_members_account_idx ON core.organization_team_members (account_did);

ALTER TABLE core.repositories
    ADD COLUMN organization_id UUID REFERENCES core.organizations(id);

DROP INDEX core.repositories_owner_slug_active_uidx;

CREATE UNIQUE INDEX repositories_account_slug_active_uidx
    ON core.repositories (owner_did, lower(slug))
    WHERE organization_id IS NULL AND deleted_at IS NULL;

CREATE UNIQUE INDEX repositories_organization_slug_active_uidx
    ON core.repositories (organization_id, lower(slug))
    WHERE organization_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX repositories_organization_time_idx
    ON core.repositories (organization_id, created_at DESC, id DESC)
    WHERE organization_id IS NOT NULL AND deleted_at IS NULL;

CREATE TABLE core.organization_team_repositories (
    team_id UUID NOT NULL REFERENCES core.organization_teams(id) ON DELETE CASCADE,
    repository_id UUID NOT NULL REFERENCES core.repositories(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (team_id, repository_id),
    CONSTRAINT organization_team_repository_role CHECK (role IN ('read', 'triage', 'write', 'maintain', 'admin'))
);

CREATE INDEX organization_team_repositories_repository_idx
    ON core.organization_team_repositories (repository_id);

ALTER TABLE core.repository_collaborators DROP CONSTRAINT repository_collaborator_role;
ALTER TABLE core.repository_collaborators
    ADD CONSTRAINT repository_collaborator_role CHECK (role IN ('read', 'triage', 'write', 'maintain', 'admin'));

CREATE TABLE ops.organization_audit_events (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES core.organizations(id) ON DELETE CASCADE,
    actor_did TEXT NOT NULL,
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    request_id TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT organization_audit_action_nonempty CHECK (length(action) > 0),
    CONSTRAINT organization_audit_target_nonempty CHECK (length(target_type) > 0 AND length(target_id) > 0)
);

CREATE INDEX organization_audit_events_org_time_idx
    ON ops.organization_audit_events (organization_id, created_at DESC, id DESC);

CREATE TABLE network.organizations (
    uri TEXT PRIMARY KEY, cid TEXT, creator_did TEXT NOT NULL, rkey TEXT NOT NULL,
    slug TEXT, name TEXT, description TEXT, website TEXT, location TEXT,
    record_created_at TIMESTAMPTZ, record_updated_at TIMESTAMPTZ,
    indexed_at TIMESTAMPTZ NOT NULL, source_event_id BIGINT NOT NULL, deleted_at TIMESTAMPTZ
);
CREATE INDEX network_organizations_slug_idx ON network.organizations (lower(slug)) WHERE deleted_at IS NULL;

CREATE TABLE network.organization_grants (
    uri TEXT PRIMARY KEY, cid TEXT, author_did TEXT NOT NULL, rkey TEXT NOT NULL,
    organization_uri TEXT NOT NULL, organization_cid TEXT NOT NULL, subject_did TEXT,
    role TEXT, authority_uri TEXT, authority_cid TEXT, record_created_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ, indexed_at TIMESTAMPTZ NOT NULL, source_event_id BIGINT NOT NULL, deleted_at TIMESTAMPTZ
);
CREATE INDEX network_organization_grants_org_idx ON network.organization_grants (organization_uri, subject_did) WHERE deleted_at IS NULL;

CREATE TABLE network.organization_memberships (
    uri TEXT PRIMARY KEY, cid TEXT, author_did TEXT NOT NULL, rkey TEXT NOT NULL,
    organization_uri TEXT NOT NULL, organization_cid TEXT NOT NULL, grant_uri TEXT NOT NULL, grant_cid TEXT NOT NULL,
    visibility TEXT, record_created_at TIMESTAMPTZ, record_updated_at TIMESTAMPTZ,
    indexed_at TIMESTAMPTZ NOT NULL, source_event_id BIGINT NOT NULL, deleted_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX network_organization_membership_uidx ON network.organization_memberships (organization_uri, author_did) WHERE deleted_at IS NULL;

CREATE TABLE network.organization_revocations (
    uri TEXT PRIMARY KEY, cid TEXT, author_did TEXT NOT NULL, rkey TEXT NOT NULL,
    organization_uri TEXT NOT NULL, organization_cid TEXT NOT NULL, grant_uri TEXT NOT NULL, grant_cid TEXT NOT NULL,
    subject_did TEXT, authority_uri TEXT, authority_cid TEXT, record_created_at TIMESTAMPTZ,
    indexed_at TIMESTAMPTZ NOT NULL, source_event_id BIGINT NOT NULL, deleted_at TIMESTAMPTZ
);
CREATE INDEX network_organization_revocations_grant_idx ON network.organization_revocations (grant_uri) WHERE deleted_at IS NULL;

ALTER TABLE network.repositories
    ADD COLUMN organization_uri TEXT,
    ADD COLUMN organization_cid TEXT;
CREATE INDEX network_repositories_organization_idx ON network.repositories (organization_uri) WHERE deleted_at IS NULL;
