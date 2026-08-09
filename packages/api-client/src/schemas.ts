export * from './generated/zod.gen'

import type { z } from 'zod'
import { zSyncRepository } from './generated/zod.gen'

// Electric parses PostgreSQL int8 counters to bigint; this is the validated
// collection-row type rather than the JSON wire type exported by the SDK.
export type SyncRepositoryRow = z.output<typeof zSyncRepository>
