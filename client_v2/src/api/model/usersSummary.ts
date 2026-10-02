/**
 * Aggregate state of the managed users
 */
export interface UsersSummary {
    total?: number;
    active?: number;
    disabled?: number;
    expired?: number;
    over_quota?: number;
    expiring_soon?: number;
    requests?: number;
    total_requests?: number;
}
