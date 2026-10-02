import type { BackupInfo } from './backupInfo';

/**
 * The response of the GET /control/users/backups API
 */
export interface BackupsResponse {
    /** The dated backups, newest first */
    backups: BackupInfo[];
}
