/**
 * The user management settings
 */
export interface UserSettings {
    /** Refuse clients that belong to no user instead of letting them through unaccounted. */
    deny_unmatched?: boolean;
    /** The origins of the portal front-ends that may call the portal API. */
    portal_origins?: string[];
    /** The address of the portal API as the browser reaches it. */
    portal_api_base?: string;
}
