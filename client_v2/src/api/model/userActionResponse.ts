import type { User } from './user';

/**
 * The result of a user management operation
 */
export interface UserActionResponse {
    ok?: boolean;
    message?: string;
    updated?: number;
    user?: User;
}
