/**
 * A new password for the user portal
 */
export interface UsersPasswordRequest {
    /** Unique identifier of the user. */
    uid: string;
    /** The new password.  An empty value revokes the portal access. */
    password: string;
}
