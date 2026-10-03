import type { UserDisabledReason } from './userDisabledReason';
import type { UserPeriod } from './userPeriod';
import type { UserStatus } from './userStatus';

/**
 * A managed user with a query quota
 */
export interface User {
    /** Unique identifier of the user. */
    uid?: string;
    /** Human-readable name of the user. */
    name?: string;
    /** Optional free-form note. */
    remark?: string;
    /** IP addresses, CIDR networks, or ClientIDs. */
    ids?: string[];
    /** Request quota per period.  -1 means unlimited. */
    request_limit?: number;
    /** Quota accounting period. */
    period?: UserPeriod;
    /** Unix timestamp of the expiration date.  0 means never. */
    expires_at?: number;
    /** Unix timestamp of the creation. */
    created_at?: number;
    /** Administrator-controlled switch. */
    enabled?: boolean;
    /** Requests in the current period. */
    requests?: number;
    /** Requests since creation. */
    total_requests?: number;
    /** Unix timestamp of the start of the current period. */
    period_start?: number;
    /** Unix timestamp of the last allowed request. */
    last_seen?: number;
    /** Computed status of the user. */
    status?: UserStatus;
    /** Reason the user is not allowed to query. */
    disabled_reason?: UserDisabledReason;
    /** Seconds until the expiration date.  -1 means never. */
    remaining_seconds?: number;
    /** Requests left in the current period.  -1 means unlimited. */
    remaining_requests?: number;
    /** Unix timestamp at which the current period ends and the counter resets.  0 means never. */
    next_reset?: number;
    /** Whether the user has a password for the user portal. */
    has_portal_password?: boolean;
}
