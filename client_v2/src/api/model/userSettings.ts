/**
 * The user management settings
 */
export interface UserSettings {
    /** Refuse clients that belong to no user instead of letting them through unaccounted. */
    deny_unmatched?: boolean;
}
