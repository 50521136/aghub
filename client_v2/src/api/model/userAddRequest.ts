import type { UserAddRequestPeriod } from './userAddRequestPeriod';

/**
 * User creation request
 */
export interface UserAddRequest {
    name: string;
    remark?: string;
    ids: string[];
    /** Request quota per period.  Omitted or negative means unlimited. */
    request_limit?: number;
    period?: UserAddRequestPeriod;
    /** Days until the subscription expires.  0 means never. */
    expire_days?: number;
    enabled?: boolean;
}
