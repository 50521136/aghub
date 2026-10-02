import type { UserUpdateRequestPeriod } from './userUpdateRequestPeriod';

/**
 * User update request
 */
export interface UserUpdateRequest {
    uid: string;
    name?: string;
    remark?: string;
    ids?: string[];
    request_limit?: number;
    period?: UserUpdateRequestPeriod;
    expire_days?: number;
    extend_days?: number;
    enabled?: boolean;
}
