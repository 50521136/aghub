import type { UpdateBackup } from './updateBackup';

/**
 * The backup that a rollback would restore.
 */
export interface UpdateBackupResponse {
    backup?: UpdateBackup;
    current_version?: string;
}
