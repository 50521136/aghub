/**
 * The state of an update in progress.
 */
export interface UpdateStatus {
    started_at?: string;
    version?: string;
    message?: string;
    error?: string;
    progress?: number;
    running?: boolean;
    done?: boolean;
}
