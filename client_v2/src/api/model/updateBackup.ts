/**
 * The previous version of the application kept next to the executable.
 */
export interface UpdateBackup {
    /** True when a backup binary is present. */
    available?: boolean;

    /** The version of the backup.  Empty when it could not be determined. */
    version?: string;

    /** The Unix timestamp in seconds at which the backup was made. */
    saved_at?: number;

    /** The size of the backup binary in bytes. */
    size?: number;
}
