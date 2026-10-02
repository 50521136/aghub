import type { User } from './user';
import type { UserBulkError } from './userBulkError';

/**
 * The result of a bulk user creation
 */
export interface UserBulkAddResponse {
    users?: User[];
    errors?: UserBulkError[];
    skipped?: number;
}
