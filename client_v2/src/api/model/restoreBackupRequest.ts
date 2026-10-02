/**
 * The request of the POST /control/users/restore API
 */
export interface RestoreBackupRequest {
    /** The name of the backup to restore */
    name: string;
}
