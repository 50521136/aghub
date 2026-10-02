import type { UpdateStatus } from './updateStatus';

/**
 * The current update state.
 */
export interface UpdateStatusResponse {
    current_version?: string;
    repository?: string;
    enabled?: boolean;
    status?: UpdateStatus;
}
