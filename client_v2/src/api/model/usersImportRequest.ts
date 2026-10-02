import type { User } from './user';

/**
 * User import request
 */
export interface UsersImportRequest {
    users: User[];
    /** Must be true; the existing users are removed. */
    replace?: boolean;
}
