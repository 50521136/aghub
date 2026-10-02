import type { UpdateRelease } from './updateRelease';

/**
 * The result of a version check.
 */
export interface UpdateCheckResponse {
    checked_at?: string;
    current_version?: string;
    repo?: string;
    error?: string;
    has_update?: boolean;
    can_update?: boolean;
    latest?: UpdateRelease;
}
