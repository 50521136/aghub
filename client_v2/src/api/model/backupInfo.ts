/**
 * One dated backup of the user state
 */
export interface BackupInfo {
    /** The file name of the backup, used to restore it */
    name: string;
    /** The modification time of the backup in Unix seconds */
    time: number;
    /** The size of the backup in bytes */
    size: number;
    /** The number of users in the backup, negative when it cannot be read */
    users: number;
}
