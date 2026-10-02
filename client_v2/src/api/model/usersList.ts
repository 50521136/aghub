import type { User } from './user';
import type { UserSettings } from './userSettings';
import type { UsersSummary } from './usersSummary';

/**
 * The managed users and the aggregate state
 */
export interface UsersList {
    users?: User[];
    summary?: UsersSummary;
    settings?: UserSettings;
    /** Domain of the DoT/DoH endpoint, empty when none is configured.  The host name of a client is its identifier joined to this domain with a dot. */
    domain?: string;
}
