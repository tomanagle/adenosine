# AWS deployment

The maintained AWS architecture is a single-region ECS cluster on EC2, not Fargate. ECS on
EC2 supports the canonical TCP SSH listener and EFS-backed POSIX storage. An ALB terminates
HTTPS; a separate NLB carries SSH on port 2222. RDS PostgreSQL stores relational state, EFS
stores bare Git repositories and SSH identity, Secrets Manager stores application keys, and
CloudWatch receives container and Collector output. A web sidecar and Caddy gateway preserve
Compose public routing while keeping `/internal/*` private. S3 is never mounted as a Git filesystem.

## Bootstrap and update

Create a Route53 public hosted zone, authenticate `aws` and `pulumi`, install dependencies in
`infra/pulumi/aws`, and export:

```sh
export ADENOSINE_DOMAIN='forge.example.com'
export ADENOSINE_ROUTE53_ZONE_ID='Z...'
export ADENOSINE_IMAGE='ghcr.io/example/adenosine@sha256:...'
export ADENOSINE_OAUTH_STATE_KEY='base64-encoded-32-byte-key'
export ADENOSINE_OAUTH_CREDENTIAL_KEY='base64-encoded-32-byte-key'
scripts/deploy-aws.sh --stack production --yes --skip-conformance
```

The script checks AWS identity and applies the same stack. Each replacement task gates serving
on its migration container, then the script waits for readiness and optionally runs common
conformance. Restrict SSH before deployment, for example:

```sh
pulumi -C infra/pulumi/aws config set --path 'sshAllowedCidrs[0]' '203.0.113.10/32'
```

The default `0.0.0.0/0` keeps Git SSH reachable but is not recommended. The task security
group accepts HTTP only from the ALB, RDS only from the task, and NFS only between task and
EFS. Storage, RDS, EFS transit, and Secrets Manager are encrypted; EC2 requires IMDSv2.

## Availability, recovery, and scaling

The initial stack intentionally runs one stateful Adenosine task and one EC2 host. EFS and
RDS span two availability zones, but this is not a zero-downtime application architecture.
Set RDS Multi-AZ and increase host capacity only after testing task replacement and Git lock
behavior. Do not run multiple writers merely by increasing desired count.

The maintained single-task stack may keep release assets beneath instance state. Before increasing
the application task count, configure every task to use one private S3 bucket through the selectable
release-asset backend; never mount S3 as the Git repository filesystem. Grant only the bucket and
object operations documented in [Repository Releases](repository-releases.md#storage-backends-and-capacity),
keep public access blocked, and validate cross-task upload/download behavior before adding writers.
Treat RDS and the release bucket as one recovery set: use a write barrier, capture both at a matched
recovery point, and drill a paired restore before relying on the replicated topology.

RDS uses a PostgreSQL 17 parameter group with logical replication settings when Electric is
enabled. A migration container must complete successfully before the application starts; it
also idempotently creates the Electric replication role, grants, replica identities, and
publication. Secret version ARNs are embedded in the task definition so rotation produces a
new ECS revision.

RDS retains automated snapshots for `backupRetentionDays` and takes a final snapshot on
destroy. The same retention config drives a daily AWS Backup plan covering RDS and EFS. Both
RDS and EFS are Pulumi-protected against accidental destroy. Those native
snapshots complement, but do not replace, a portable backup of PostgreSQL, the EFS access
point, SSH host key, required secrets, and release manifest. If S3 release storage is selected,
the portable backup deliberately fails closed; provider recovery must also preserve and restore the
bucket with PostgreSQL. Restore into a clean stack,
preserve the SSH key, run migrations, then run doctor/conformance and clone a repository.

Rollback requires the prior immutable image and a compatible database. Forward-only
migrations require restoring the pre-update database and EFS backup as one recovery point.

Baseline cost includes a continuously running EC2 instance, RDS instance and storage, EFS,
one NAT gateway, ALB, NLB, public IPv4, CloudWatch, and data transfer. This opinionated full-forge target
trades that cost for real SSH and POSIX Git semantics.
