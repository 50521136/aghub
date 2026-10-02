/**
 * Request to enable or disable a set of users
 */
export interface UsersToggleRequest {
    uids: string[];
    enabled: boolean;
}
